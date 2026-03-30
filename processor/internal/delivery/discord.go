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
	"time"

	log "github.com/sirupsen/logrus"
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

// Platform returns the platform identifier.
func (ds *DiscordSender) Platform() string { return "discord" }

// Send delivers a message to Discord. Routes by job.Type.
func (ds *DiscordSender) Send(ctx context.Context, job *Job) (*SentMessage, error) {
	switch job.Type {
	case "discord:user":
		channelID, err := ds.ensureDMChannel(ctx, job.Target)
		if err != nil {
			return nil, err
		}
		return ds.postMessage(ctx, channelID, job.Message)
	case "discord:channel", "discord:thread":
		return ds.postMessage(ctx, job.Target, job.Message)
	case "webhook":
		return ds.postWebhook(ctx, job.Target, job.Message)
	default:
		return nil, fmt.Errorf("unsupported discord job type: %s", job.Type)
	}
}

// Delete deletes a previously sent message.
func (ds *DiscordSender) Delete(ctx context.Context, sentID string) error {
	method := http.MethodDelete
	url, auth, err := ds.resolveMessageURL(sentID)
	if err != nil {
		return err
	}
	resp, err := ds.doRequest(ctx, method, url, nil, "", auth)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body) //nolint:errcheck

	switch resp.StatusCode {
	case http.StatusNoContent, http.StatusOK:
		return nil
	case http.StatusNotFound:
		return nil // already deleted
	case http.StatusForbidden, http.StatusUnauthorized:
		return nil // no permission, don't retry
	default:
		return fmt.Errorf("discord delete returned status %d", resp.StatusCode)
	}
}

// Edit edits a previously sent message.
func (ds *DiscordSender) Edit(ctx context.Context, sentID string, message json.RawMessage) error {
	url, auth, err := ds.resolveMessageURL(sentID)
	if err != nil {
		return err
	}
	resp, err := ds.doRequest(ctx, http.MethodPatch, url, bytes.NewReader(message), "application/json", auth)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body) //nolint:errcheck

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	return fmt.Errorf("discord edit returned status %d", resp.StatusCode)
}

// resolveMessageURL parses a sentID into a DELETE/PATCH URL plus auth flag.
func (ds *DiscordSender) resolveMessageURL(sentID string) (url string, auth bool, err error) {
	idx := strings.LastIndex(sentID, ":")
	if idx < 0 {
		return "", false, fmt.Errorf("invalid sentID format: %s", sentID)
	}
	target := sentID[:idx]
	messageID := sentID[idx+1:]

	if strings.HasPrefix(target, "http") {
		// Webhook: target is the webhook URL
		return target + "/messages/" + messageID, false, nil
	}
	// Bot: target is channelID
	return ds.baseURL + "/channels/" + target + "/messages/" + messageID, true, nil
}

// ensureDMChannel gets or creates a DM channel for the given user.
func (ds *DiscordSender) ensureDMChannel(ctx context.Context, userID string) (string, error) {
	if cached, ok := ds.dmChannels.Load(userID); ok {
		return cached.(string), nil
	}

	body := fmt.Sprintf(`{"recipient_id":"%s"}`, userID)
	resp, err := ds.doRequest(ctx, http.MethodPost, ds.baseURL+"/users/@me/channels",
		strings.NewReader(body), "application/json", true)
	if err != nil {
		return "", fmt.Errorf("creating DM channel: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		code := extractErrorCode(respBody)
		if code == 50007 || code == 10003 || code == 10013 {
			return "", &PermanentError{
				Err:    fmt.Errorf("discord error %d creating DM channel for %s", code, userID),
				Reason: fmt.Sprintf("discord error code %d", code),
			}
		}
		return "", fmt.Errorf("creating DM channel returned status %d: %s", resp.StatusCode, respBody)
	}

	var result struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decoding DM channel response: %w", err)
	}

	ds.dmChannels.Store(userID, result.ID)
	log.Infof("discord: created DM channel %s for user %s", result.ID, userID)
	return result.ID, nil
}

// postMessage sends a message to a Discord channel via the bot API.
func (ds *DiscordSender) postMessage(ctx context.Context, channelID string, message json.RawMessage) (*SentMessage, error) {
	ds.rateLimiter.Wait(channelID)

	normalized, err := NormalizeDiscordMessage(message)
	if err != nil {
		return nil, fmt.Errorf("normalizing message: %w", err)
	}

	var reqBody io.Reader
	var contentType string

	if ds.uploadImages {
		if imageURL := ExtractEmbedImageURL(normalized); imageURL != "" {
			log.Debugf("discord: uploading embed image for bot/%s", channelID)
			if imageData, err := DownloadImage(ds.client, imageURL); err == nil {
				normalized = ReplaceEmbedImageURL(normalized)
				buf, ct, err := BuildMultipartMessage(normalized, imageData, "files[0]")
				if err == nil {
					reqBody = buf
					contentType = ct
				}
			} else {
				log.Debugf("discord: image download failed for %s, sending without: %v", channelID, err)
			}
		}
	}

	if reqBody == nil {
		reqBody = bytes.NewReader(normalized)
		contentType = "application/json"
	}

	url := ds.baseURL + "/channels/" + channelID + "/messages"
	return ds.sendWithRetry(ctx, url, reqBody, contentType, true, channelID)
}

// postWebhook sends a message via a Discord webhook URL.
func (ds *DiscordSender) postWebhook(ctx context.Context, webhookURL string, message json.RawMessage) (*SentMessage, error) {
	ds.rateLimiter.Wait(webhookURL)

	normalized, err := NormalizeDiscordMessage(message)
	if err != nil {
		return nil, fmt.Errorf("normalizing message: %w", err)
	}

	var reqBody io.Reader
	var contentType string

	if ds.uploadImages {
		if imageURL := ExtractEmbedImageURL(normalized); imageURL != "" {
			log.Debugf("discord: uploading embed image for webhook/%s", webhookURL)
			if imageData, err := DownloadImage(ds.client, imageURL); err == nil {
				normalized = ReplaceEmbedImageURL(normalized)
				buf, ct, err := BuildMultipartMessage(normalized, imageData, "file")
				if err == nil {
					reqBody = buf
					contentType = ct
				}
			} else {
				log.Debugf("discord: image download failed for %s, sending without: %v", webhookURL, err)
			}
		}
	}

	if reqBody == nil {
		reqBody = bytes.NewReader(normalized)
		contentType = "application/json"
	}

	url := webhookURL + "?wait=true"
	return ds.sendWithRetry(ctx, url, reqBody, contentType, false, webhookURL)
}

// sendWithRetry sends a Discord request with retry logic for 429 and 5xx.
func (ds *DiscordSender) sendWithRetry(ctx context.Context, url string, body io.Reader, contentType string, auth bool, rateLimitKey string) (*SentMessage, error) {
	// Buffer the body so we can replay on retry.
	bodyBytes, err := io.ReadAll(body)
	if err != nil {
		return nil, fmt.Errorf("reading request body: %w", err)
	}

	log.Debugf("discord: sending to %s", rateLimitKey)

	const maxRetries = 5
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			default:
			}
		}

		resp, err := ds.doRequest(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes), contentType, auth)
		if err != nil {
			log.Warnf("discord: send to %s failed (attempt %d/%d): %v", rateLimitKey, attempt+1, maxRetries+1, err)
			if attempt < maxRetries {
				time.Sleep(time.Duration(attempt+1) * time.Second)
				continue
			}
			return nil, err
		}

		ds.rateLimiter.Update(rateLimitKey, resp.Header)

		respBody, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			return nil, fmt.Errorf("reading response body: %w", readErr)
		}

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			var result struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal(respBody, &result); err != nil {
				return nil, fmt.Errorf("decoding response: %w", err)
			}
			log.Debugf("discord: delivered to %s (msg %s)", rateLimitKey, result.ID)
			return &SentMessage{ID: rateLimitKey + ":" + result.ID}, nil
		}

		if resp.StatusCode == http.StatusTooManyRequests {
			var rateLimitResp struct {
				RetryAfter float64 `json:"retry_after"`
			}
			json.Unmarshal(respBody, &rateLimitResp) //nolint:errcheck
			d := ParseRetryAfter(rateLimitResp.RetryAfter)
			log.Warnf("discord: 429 rate limited for %s, retry_after=%.1fs (attempt %d/%d)", rateLimitKey, rateLimitResp.RetryAfter, attempt+1, maxRetries+1)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(d):
			}
			continue
		}

		code := extractErrorCode(respBody)
		if code == 50007 || code == 10003 || code == 10013 {
			log.Warnf("discord: permanent error for %s: %s (code: %d)", rateLimitKey, truncate(string(respBody), 200), code)
			return nil, &PermanentError{
				Err:    fmt.Errorf("discord error %d: %s", code, respBody),
				Reason: fmt.Sprintf("discord error code %d", code),
			}
		}

		if resp.StatusCode >= 500 && attempt < maxRetries {
			log.Warnf("discord: send to %s failed (attempt %d/%d): status=%d %s", rateLimitKey, attempt+1, maxRetries+1, resp.StatusCode, truncate(string(respBody), 200))
			time.Sleep(time.Duration(attempt+1) * time.Second)
			continue
		}

		return nil, fmt.Errorf("discord API returned status %d: %s", resp.StatusCode, respBody)
	}
	return nil, fmt.Errorf("discord API: max retries exceeded")
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
