package delivery

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/logref"
	"github.com/pokemon/poracleng/processor/internal/metrics"
)

const defaultDiscordBaseURL = "https://discord.com/api/v10"

// DiscordSender delivers messages via the Discord REST API.
type DiscordSender struct {
	baseURL      string
	token        string
	client       *http.Client
	rateLimiter  *DiscordRateLimiter
	uploadImages bool
	deleteDelay  time.Duration
	dmChannels   sync.Map // userID → DM channelID

	// Per-subtype wire-call concurrency semaphores (nil = unlimited). A slot is
	// held only across a single HTTP round-trip (see roundTrip), never during
	// WaitForRateLimit or 429/5xx backoff, so a rate-limited route can't pin the
	// pool. discordSem gates bot calls (channel/DM/thread); webhookSem gates
	// webhook calls. Selected by the `auth` flag (auth => bot => discordSem).
	discordSem   chan struct{}
	webhookSem   chan struct{}
	discordInFly atomic.Int64
	webhookInFly atomic.Int64

	// Tile URL rewrite: when re-downloading a tile for multipart upload,
	// substitute the public tileserver base with the internal one so our
	// download bypasses any CDN/proxy in front of the public URL.
	tilePublicBase   string
	tileInternalBase string
}

// SetTileURLRewrite configures the tile URL rewrite applied before the
// processor downloads an embed image for re-upload. publicBase is the URL
// that appears in embed.image.url (what Discord clients resolve);
// internalBase is the private URL the processor should hit instead.
// Empty internalBase disables the rewrite.
func (ds *DiscordSender) SetTileURLRewrite(publicBase, internalBase string) {
	ds.tilePublicBase = strings.TrimRight(publicBase, "/")
	ds.tileInternalBase = strings.TrimRight(internalBase, "/")
}

// rewriteTileURL returns url with the public tile base swapped for the
// internal base. Returns url unchanged if rewrite isn't configured or the
// URL doesn't start with the public base.
func (ds *DiscordSender) rewriteTileURL(url string) string {
	if ds.tileInternalBase == "" || ds.tilePublicBase == "" {
		return url
	}
	if ds.tileInternalBase == ds.tilePublicBase {
		return url
	}
	if !strings.HasPrefix(url, ds.tilePublicBase) {
		return url
	}
	return ds.tileInternalBase + strings.TrimPrefix(url, ds.tilePublicBase)
}

// NewDiscordSender creates a new Discord sender.
func NewDiscordSender(token string, uploadImages bool, deleteDelayMs int) *DiscordSender {
	return &DiscordSender{
		baseURL:      defaultDiscordBaseURL,
		token:        token,
		client:       &http.Client{Timeout: 30 * time.Second},
		rateLimiter:  NewDiscordRateLimiter(),
		uploadImages: uploadImages,
		deleteDelay:  time.Duration(deleteDelayMs) * time.Millisecond,
	}
}

// makeSem returns a semaphore of capacity n, or nil for n<=0 (unlimited).
func makeSem(n int) chan struct{} {
	if n <= 0 {
		return nil
	}
	return make(chan struct{}, n)
}

// SetConcurrency sizes the per-subtype wire-call semaphores. n<=0 is clamped
// to 1 (never unlimited) — an unset sender that never had SetConcurrency
// called keeps nil sems, but a configured sender always caps at ≥1. Call once
// at construction, before any Send/Edit/Delete.
func (ds *DiscordSender) SetConcurrency(discord, webhook int) {
	if discord <= 0 {
		discord = 1
	}
	if webhook <= 0 {
		webhook = 1
	}
	ds.discordSem = makeSem(discord)
	ds.webhookSem = makeSem(webhook)
}

// DiscordInFlight / WebhookInFlight report current concurrent wire calls per
// subtype (for the [Status] log and per-platform depth gauges).
func (ds *DiscordSender) DiscordInFlight() int { return int(ds.discordInFly.Load()) }
func (ds *DiscordSender) WebhookInFlight() int { return int(ds.webhookInFly.Load()) }

// Platform returns the platform identifier.
func (ds *DiscordSender) Platform() string { return "discord" }

// Send delivers a message to Discord. Routes by job.Type.
func (ds *DiscordSender) Send(ctx context.Context, job *Job) (*SentMessage, error) {
	switch job.Type {
	case "discord:user":
		channelID, err := ds.ensureDMChannel(ctx, job.Target, job.LogReference)
		if err != nil {
			return nil, err
		}
		return ds.postMessage(ctx, channelID, job.Message, job.StaticMapData, job.ReplyToID, job.LogReference)
	case "discord:channel", "discord:thread":
		return ds.postMessage(ctx, job.Target, job.Message, job.StaticMapData, job.ReplyToID, job.LogReference)
	case "webhook":
		// Discord webhooks cannot post replies — message_reference is
		// rejected by the webhook endpoint. Drop ReplyToID silently for
		// webhook deliveries; a missing reply chain is strictly
		// preferable to a refused alert.
		return ds.postWebhook(ctx, job.Target, job.Message, job.StaticMapData, job.LogReference)
	default:
		return nil, fmt.Errorf("unsupported discord job type: %s", job.Type)
	}
}

// Delete deletes a previously sent message.
func (ds *DiscordSender) Delete(ctx context.Context, sentID string) error {
	url, auth, rlKey, err := ds.resolveMessageURL(sentID)
	if err != nil {
		return err
	}
	// Clean-deletion is routed through the FairQueue, whose lane drainer
	// serialises this delete with sends to the same target; WaitForRateLimit
	// already ran before calling Delete (so the proactive gate + serialisation
	// happen there, same as sends). Here we only need the reactive 429
	// Retry-After backoff + header-driven limiter updates.
	respBody, status, err := ds.doWithRetry(ctx, http.MethodDelete, url, nil, "", auth, rlKey, "clean-delete")
	if err != nil {
		return err
	}
	switch status {
	case http.StatusNoContent, http.StatusOK, http.StatusNotFound:
		return nil // deleted, or already gone
	case http.StatusForbidden, http.StatusUnauthorized:
		return nil // no permission, don't retry
	default:
		return fmt.Errorf("discord delete returned status %d: %s", status, truncate(string(respBody), 200))
	}
}

// Edit edits a previously sent message. Supports multipart image upload
// (same as Send) so inline-mode tiles are preserved on edits rather than
// falling back to the placeholder URL.
func (ds *DiscordSender) Edit(ctx context.Context, sentID string, message json.RawMessage, staticMapData []byte) error {
	normalized, imageURL, err := NormalizeAndExtractImage(message, ds.uploadImages)
	if err == nil && normalized != nil {
		message = normalized
	}

	// Webhook-sent messages use "file" as the multipart field name;
	// bot-sent messages use "files[0]". Match the send path.
	fileField := "files[0]"
	if strings.HasPrefix(sentID, "http") {
		fileField = "file"
	}

	var reqBody io.Reader
	var contentType string

	if len(staticMapData) > 0 && imageURL != "" {
		normalized = ReplaceEmbedImageURL(normalized)
		buf, ct, err := BuildMultipartMessage(normalized, staticMapData, fileField)
		if err == nil {
			reqBody = buf
			contentType = ct
		}
	} else if imageURL != "" {
		if imageData, err := DownloadImage(ds.client, ds.rewriteTileURL(imageURL)); err == nil {
			normalized = ReplaceEmbedImageURL(normalized)
			buf, ct, err := BuildMultipartMessage(normalized, imageData, fileField)
			if err == nil {
				reqBody = buf
				contentType = ct
			}
		} else {
			log.Warnf("discord: edit image download failed (%s), editing without image: %v", imageURL, err)
		}
	}

	if reqBody == nil {
		reqBody = bytes.NewReader(message)
		contentType = "application/json"
	}

	url, auth, rlKey, err := ds.resolveMessageURL(sentID)
	if err != nil {
		return err
	}
	bodyBytes, err := io.ReadAll(reqBody)
	if err != nil {
		return fmt.Errorf("reading edit body: %w", err)
	}
	// Edit is only called from the FairQueue, which already Waited on the rate
	// limiter — so here we just need the reactive 429/Retry-After + 5xx retry
	// (and header-driven limiter updates) the send path gets but doRequest
	// didn't.
	respBody, status, err := ds.doWithRetry(ctx, http.MethodPatch, url, bodyBytes, contentType, auth, rlKey, "edit")
	if err != nil {
		return err
	}
	if status >= 200 && status < 300 {
		return nil
	}
	return fmt.Errorf("discord edit returned status %d: %s", status, truncate(string(respBody), 200))
}

// resolveMessageURL parses a sentID into a DELETE/PATCH URL, auth flag, and the
// rate-limit key (the destination — channel ID or webhook URL). The key matches
// what the send path uses for posts to that destination, so edits and deletes
// share the same limiter bucket.
func (ds *DiscordSender) resolveMessageURL(sentID string) (url string, auth bool, rlKey string, err error) {
	idx := strings.LastIndex(sentID, ":")
	if idx < 0 {
		return "", false, "", fmt.Errorf("invalid sentID format: %s", sentID)
	}
	target := sentID[:idx]
	messageID := sentID[idx+1:]

	if strings.HasPrefix(target, "http") {
		// Webhook: target is the webhook URL
		return target + "/messages/" + messageID, false, target, nil
	}
	// Bot: target is channelID
	return ds.baseURL + "/channels/" + target + "/messages/" + messageID, true, target, nil
}

// ensureDMChannel gets or creates a DM channel for the given user.
func (ds *DiscordSender) ensureDMChannel(ctx context.Context, userID, logRef string) (string, error) {
	if cached, ok := ds.dmChannels.Load(userID); ok {
		return cached.(string), nil
	}

	body := fmt.Sprintf(`{"recipient_id":"%s"}`, userID)
	respBody, status, _, err := ds.roundTrip(ctx, http.MethodPost, ds.baseURL+"/users/@me/channels",
		[]byte(body), "application/json", true)
	if err != nil {
		return "", fmt.Errorf("creating DM channel: %w", err)
	}
	if status != http.StatusOK {
		code := extractErrorCode(respBody)
		if code == 50007 || code == 10003 || code == 10013 {
			return "", &PermanentError{
				Err:    fmt.Errorf("discord error %d creating DM channel for %s", code, userID),
				Reason: fmt.Sprintf("discord error code %d", code),
			}
		}
		return "", fmt.Errorf("creating DM channel returned status %d: %s", status, respBody)
	}
	var result struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("decoding DM channel response: %w", err)
	}
	ds.dmChannels.Store(userID, result.ID)
	logref.Infof(logRef, "discord: created DM channel %s for user %s", result.ID, userID)
	return result.ID, nil
}

// WaitForRateLimit blocks until the target is not rate-limited.
func (ds *DiscordSender) WaitForRateLimit(target string) {
	ds.rateLimiter.Wait(target)
}

// postMessage sends a message to a Discord channel via the bot API.
func (ds *DiscordSender) postMessage(ctx context.Context, channelID string, message json.RawMessage, staticMapData []byte, replyToID, logRef string) (*SentMessage, error) {
	normalized, imageURL, err := NormalizeAndExtractImage(message, ds.uploadImages)
	if err != nil {
		return nil, fmt.Errorf("normalizing message: %w", err)
	}

	// Inject message_reference for reply chaining BEFORE the multipart
	// builder consumes the body, so the reference rides through both the
	// JSON-body and multipart-payload paths below.
	if replyToID != "" {
		if msgID := splitSentID(replyToID); msgID != "" {
			normalized = injectMessageReference(normalized, msgID)
		}
	}

	var reqBody io.Reader
	var contentType string

	if len(staticMapData) > 0 && imageURL != "" {
		// Inline tile: bytes already available, skip download
		logref.Debugf(logRef, "discord: using inline tile for bot/%s (%d bytes)", channelID, len(staticMapData))
		normalized = ReplaceEmbedImageURL(normalized)
		buf, ct, err := BuildMultipartMessage(normalized, staticMapData, "files[0]")
		if err == nil {
			reqBody = buf
			contentType = ct
		}
	} else if imageURL != "" {
		logref.Debugf(logRef, "discord: uploading embed image for bot/%s", channelID)
		if imageData, err := DownloadImage(ds.client, ds.rewriteTileURL(imageURL)); err == nil {
			normalized = ReplaceEmbedImageURL(normalized)
			buf, ct, err := BuildMultipartMessage(normalized, imageData, "files[0]")
			if err == nil {
				reqBody = buf
				contentType = ct
			}
		} else {
			logref.Warnf(logRef, "discord: image download failed for %s (%s), sending without image: %v", channelID, imageURL, err)
		}
	}

	if reqBody == nil {
		reqBody = bytes.NewReader(normalized)
		contentType = "application/json"
	}

	url := ds.baseURL + "/channels/" + channelID + "/messages"
	return ds.sendWithRetry(ctx, url, reqBody, contentType, true, channelID, logRef)
}

// postWebhook sends a message via a Discord webhook URL.
func (ds *DiscordSender) postWebhook(ctx context.Context, webhookURL string, message json.RawMessage, staticMapData []byte, logRef string) (*SentMessage, error) {
	normalized, imageURL, err := NormalizeAndExtractImage(message, ds.uploadImages)
	if err != nil {
		return nil, fmt.Errorf("normalizing message: %w", err)
	}

	var reqBody io.Reader
	var contentType string

	if len(staticMapData) > 0 && imageURL != "" {
		// Inline tile: bytes already available, skip download
		logref.Debugf(logRef, "discord: using inline tile for webhook/%s (%d bytes)", webhookURL, len(staticMapData))
		normalized = ReplaceEmbedImageURL(normalized)
		buf, ct, err := BuildMultipartMessage(normalized, staticMapData, "file")
		if err == nil {
			reqBody = buf
			contentType = ct
		}
	} else if imageURL != "" {
		logref.Debugf(logRef, "discord: uploading embed image for webhook/%s", webhookURL)
		if imageData, err := DownloadImage(ds.client, ds.rewriteTileURL(imageURL)); err == nil {
			normalized = ReplaceEmbedImageURL(normalized)
			buf, ct, err := BuildMultipartMessage(normalized, imageData, "file")
			if err == nil {
				reqBody = buf
				contentType = ct
			}
		} else {
			logref.Warnf(logRef, "discord: image download failed for webhook %s (%s), sending without image: %v", webhookURL, imageURL, err)
		}
	}

	if reqBody == nil {
		reqBody = bytes.NewReader(normalized)
		contentType = "application/json"
	}

	url := webhookURL + "?wait=true"
	return ds.sendWithRetry(ctx, url, reqBody, contentType, false, webhookURL, logRef)
}

// sendWithRetry sends a Discord POST and interprets the result (2xx → SentMessage,
// permanent Discord error codes → PermanentError). The rate-limit gating, 429
// Retry-After backoff, and 5xx retry live in the shared doWithRetry.
func (ds *DiscordSender) sendWithRetry(ctx context.Context, url string, body io.Reader, contentType string, auth bool, rateLimitKey, logRef string) (*SentMessage, error) {
	bodyBytes, err := io.ReadAll(body)
	if err != nil {
		return nil, fmt.Errorf("reading request body: %w", err)
	}

	logref.Debugf(logRef, "discord: sending to %s", rateLimitKey)

	respBody, status, err := ds.doWithRetry(ctx, http.MethodPost, url, bodyBytes, contentType, auth, rateLimitKey, logRef)
	if err != nil {
		return nil, err
	}

	if status >= 200 && status < 300 {
		var result struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(respBody, &result); err != nil {
			return nil, fmt.Errorf("decoding response: %w", err)
		}
		logref.Debugf(logRef, "discord: delivered to %s (msg %s)", rateLimitKey, result.ID)
		return &SentMessage{ID: rateLimitKey + ":" + result.ID}, nil
	}

	code := extractErrorCode(respBody)
	if code == 50007 || code == 10003 || code == 10013 {
		logref.Warnf(logRef, "discord: permanent error for %s: %s (code: %d)", rateLimitKey, truncate(string(respBody), 200), code)
		return nil, &PermanentError{
			Err:    fmt.Errorf("discord error %d: %s", code, respBody),
			Reason: fmt.Sprintf("discord error code %d", code),
		}
	}

	return nil, fmt.Errorf("discord API returned status %d: %s", status, respBody)
}

// roundTrip executes ONE Discord HTTP request while holding a platform
// concurrency slot, and releases the slot before returning — so the caller's
// 429/5xx backoff runs slot-free. auth selects the bot (discordSem) vs webhook
// (webhookSem) semaphore. Returns the response body, status, and headers.
func (ds *DiscordSender) roundTrip(ctx context.Context, method, url string, bodyBytes []byte, contentType string, auth bool) ([]byte, int, http.Header, error) {
	sem, inFly := ds.discordSem, &ds.discordInFly
	if !auth {
		sem, inFly = ds.webhookSem, &ds.webhookInFly
	}
	if sem != nil {
		select {
		case sem <- struct{}{}:
			defer func() { <-sem }()
		case <-ctx.Done():
			return nil, 0, nil, ctx.Err()
		}
	}
	inFly.Add(1)
	metrics.DeliveryInFlight.WithLabelValues("discord").Inc()
	defer func() {
		inFly.Add(-1)
		metrics.DeliveryInFlight.WithLabelValues("discord").Dec()
	}()

	var body io.Reader
	if bodyBytes != nil {
		body = bytes.NewReader(bodyBytes)
	}
	resp, err := ds.doRequest(ctx, method, url, body, contentType, auth)
	if err != nil {
		return nil, 0, nil, err
	}
	respBody, readErr := io.ReadAll(resp.Body)
	resp.Body.Close()
	if readErr != nil {
		return nil, resp.StatusCode, resp.Header, fmt.Errorf("reading response body: %w", readErr)
	}
	return respBody, resp.StatusCode, resp.Header, nil
}

// doWithRetry executes an HTTP request against Discord with the same
// header-driven limiter updates, 429 Retry-After backoff, and 5xx retry the
// send path uses — so edits and clean-deletes get the same treatment as posts
// instead of a single unguarded request. It does NOT call rateLimiter.Wait; the
// proactive per-route/global gate is the caller's job (the FairQueue does it for
// send/edit; Delete does it itself since clean-deletion bypasses the queue) —
// and acquires a concurrency slot per attempt via roundTrip, released before
// any backoff sleep. Returns the final response body and status after retries;
// bodyBytes may be nil (DELETE). rateLimitKey is the destination
// (channel/webhook) so every method to a destination shares one limiter bucket.
func (ds *DiscordSender) doWithRetry(ctx context.Context, method, url string, bodyBytes []byte, contentType string, auth bool, rateLimitKey, logRef string) ([]byte, int, error) {
	const maxRetries = 5
	for attempt := 0; attempt <= maxRetries; attempt++ {
		respBody, status, header, err := ds.roundTrip(ctx, method, url, bodyBytes, contentType, auth)
		if err != nil {
			if status != 0 {
				// read error after a response — surface it (non-retryable)
				return nil, status, err
			}
			if ctx.Err() != nil {
				return nil, 0, ctx.Err()
			}
			logref.Warnf(logRef, "discord: %s to %s failed (attempt %d/%d): %v", method, rateLimitKey, attempt+1, maxRetries+1, err)
			if attempt < maxRetries {
				select {
				case <-ctx.Done():
					return nil, 0, ctx.Err()
				case <-time.After(time.Duration(attempt+1) * time.Second):
				}
				continue
			}
			return nil, 0, err
		}

		ds.rateLimiter.Update(rateLimitKey, header)

		if status == http.StatusTooManyRequests {
			var rl struct {
				RetryAfter float64 `json:"retry_after"`
			}
			json.Unmarshal(respBody, &rl) //nolint:errcheck
			d := ParseRetryAfter(rl.RetryAfter)
			metrics.DeliveryRateLimited.WithLabelValues("discord").Inc()
			ds.rateLimiter.Record429()
			logref.Warnf(logRef, "discord: 429 for %s %s, retry_after=%.1fs (attempt %d/%d)", method, rateLimitKey, rl.RetryAfter, attempt+1, maxRetries+1)
			if attempt < maxRetries {
				select {
				case <-ctx.Done():
					return respBody, status, ctx.Err()
				case <-time.After(d):
				}
				continue
			}
			return respBody, status, nil
		}

		if status >= 500 && attempt < maxRetries {
			logref.Warnf(logRef, "discord: %s to %s status=%d (attempt %d/%d), retrying", method, rateLimitKey, status, attempt+1, maxRetries+1)
			select {
			case <-ctx.Done():
				return respBody, status, ctx.Err()
			case <-time.After(time.Duration(attempt+1) * time.Second):
			}
			continue
		}

		return respBody, status, nil
	}
	return nil, 0, fmt.Errorf("discord API: max retries exceeded")
}

// doRequest builds and executes an HTTP request.
func (ds *DiscordSender) doRequest(ctx context.Context, method, url string, body io.Reader, contentType string, auth bool) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if auth {
		req.Header.Set("Authorization", "Bot "+ds.token)
	}
	return ds.client.Do(req)
}

// injectMessageReference adds a Discord message_reference field to a JSON
// message body so the post is rendered as a reply. Operates on the raw JSON
// because the surrounding code paths pass json.RawMessage through to the
// HTTP request body and multipart builder.
//
// fail_if_not_exists=false makes Discord gracefully degrade to a plain
// message if the prior was deleted between tracking and send, rather than
// returning HTTP 400.
//
// Returns the input unchanged on parse/encode failure — a missing reply is
// strictly preferable to dropping the alert.
func injectMessageReference(raw json.RawMessage, msgID string) json.RawMessage {
	if msgID == "" || len(raw) == 0 {
		return raw
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		return raw
	}
	body["message_reference"] = map[string]any{
		"message_id":         msgID,
		"fail_if_not_exists": false,
	}
	out, err := marshalNoEscape(body)
	if err != nil {
		return raw
	}
	return out
}

// extractErrorCode reads the "code" field from a Discord error response body.
func extractErrorCode(body []byte) int {
	var errResp struct {
		Code int `json:"code"`
	}
	if json.Unmarshal(body, &errResp) == nil {
		return errResp.Code
	}
	return 0
}

// truncate returns s truncated to maxLen characters, with "..." appended if truncated.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
