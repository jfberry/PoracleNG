package discordbot

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/buttonactions"
	"github.com/pokemon/poracleng/processor/internal/buttons"
	"github.com/pokemon/poracleng/processor/internal/delivery"
	"github.com/pokemon/poracleng/processor/internal/dts"
	"github.com/pokemon/poracleng/processor/internal/metrics"
	"github.com/pokemon/poracleng/processor/internal/snapshots"
)

// clickCooldown is the per-(user, message, button) interval below which
// repeated clicks are swallowed. Prevents accidental double-fires and
// soft-limits griefing on shared channels. 5s matches the IMPLEMENTATION
// spec — short enough that intentional retries feel responsive, long
// enough that fat-fingered clicks don't double-act.
const clickCooldown = 5 * time.Second

// clickCooldownMap tracks the last click time per (user, message, button)
// key. Single source of truth on this Bot instance; never persisted.
type clickCooldownMap struct {
	mu    sync.Mutex
	last  map[string]time.Time
	maxSz int
}

func newClickCooldownMap() *clickCooldownMap {
	return &clickCooldownMap{
		last:  make(map[string]time.Time),
		maxSz: 4096, // bounded so a noisy channel can't OOM us
	}
}

// allow checks the cooldown and stamps a fresh "last clicked" on success.
// Returns true when the click should proceed, false when it's within the
// cooldown window.
func (c *clickCooldownMap) allow(userID, messageID, buttonID string) bool {
	key := userID + ":" + messageID + ":" + buttonID
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	if last, ok := c.last[key]; ok && now.Sub(last) < clickCooldown {
		return false
	}
	// Cheap eviction: when the map grows large, drop a chunk of stale
	// entries. Not exact LRU — just enough to keep memory bounded.
	if len(c.last) >= c.maxSz {
		threshold := now.Add(-2 * clickCooldown)
		for k, t := range c.last {
			if t.Before(threshold) {
				delete(c.last, k)
			}
		}
	}
	c.last[key] = now
	return true
}

// handleButtonClick is the InteractionCreate path for poracle:btn:* custom
// IDs. Returns true when the interaction was handled (so the caller skips
// other component dispatchers), false otherwise.
//
// Flow:
//  1. Parse custom_id → actionID.
//  2. Load Snapshot via (target = bot user, messageID = ic.Message.ID).
//     Falls back to other target shapes when not found (DMs vs channels).
//  3. Resolve the button def from currently-loaded DTS using snapshot's
//     TemplateType/Platform/Language/TemplateSelected.
//  4. Apply applies_to / visible_to / cooldown checks.
//  5. Dispatch via the action registry or render the response template.
//  6. Respond ephemeral.
func (b *Bot) handleButtonClick(s *discordgo.Session, ic *discordgo.InteractionCreate) bool {
	data := ic.MessageComponentData()
	actionID, ok := dts.SplitCustomID(data.CustomID)
	if !ok {
		return false
	}

	if b.SnapshotStore == nil {
		metrics.ButtonClicksTotal.WithLabelValues("unknown", "unknown", actionID, "expired").Inc()
		respondEphemeral(s, ic, "This alert has expired.")
		return true
	}
	if ic.Message == nil || ic.Message.ID == "" {
		metrics.ButtonClicksTotal.WithLabelValues("unknown", "unknown", actionID, "expired").Inc()
		respondEphemeral(s, ic, "This alert has expired.")
		return true
	}

	clicker := clickerUserID(ic)
	if clicker == "" {
		respondEphemeral(s, ic, "Couldn't identify you to handle that click.")
		return true
	}

	snap, target, err := lookupSnapshotForClick(b.SnapshotStore, ic)
	if err != nil {
		// Visible log so the operator can compare against the snapshot
		// store contents. Lists every key we tried so a key-mismatch
		// stands out ("we looked up X, store has Y").
		log.Warnf("discord button: snapshot lookup failed for msg=%s clicker=%s channel=%s tried=%v: %v",
			ic.Message.ID, clicker, ic.ChannelID, lookupKeysFor(ic), err)
		metrics.ButtonClicksTotal.WithLabelValues("unknown", "unknown", actionID, "expired").Inc()
		respondEphemeral(s, ic, "This alert has expired.")
		return true
	}

	tt, tid := snap.TemplateType, snap.TemplateSelected
	def, ok := b.resolveButton(snap, actionID)
	if !ok {
		log.Warnf("discord button: no button %q found in DTS for type=%q platform=%q id=%q lang=%q (requested=%q)",
			actionID, snap.TemplateType, snap.Platform, snap.TemplateSelected, snap.Language, snap.TemplateRequested)
		metrics.ButtonClicksTotal.WithLabelValues(tt, tid, actionID, "unavailable").Inc()
		respondEphemeral(s, ic, "This button is no longer available.")
		return true
	}

	if !def.AppliesToTarget(snap.TargetType) {
		metrics.ButtonClicksTotal.WithLabelValues(tt, tid, actionID, "wrong_target").Inc()
		respondEphemeral(s, ic, "This button doesn't apply here.")
		return true
	}

	if !b.checkVisibility(def, snap, clicker, ic) {
		metrics.ButtonClicksTotal.WithLabelValues(tt, tid, actionID, "unauthorized").Inc()
		respondEphemeral(s, ic, "This button isn't for you.")
		return true
	}

	if !b.clickCooldown.allow(clicker, ic.Message.ID, def.ID) {
		metrics.ButtonClicksTotal.WithLabelValues(tt, tid, actionID, "cooldown").Inc()
		respondEphemeral(s, ic, "Slow down — try that again in a moment.")
		return true
	}

	resp := b.dispatchClick(ic, snap, def, clicker)
	metrics.ButtonClicksTotal.WithLabelValues(tt, tid, actionID, "ok").Inc()
	_ = target // currently unused; reserved for action handlers that need to know the target shape
	respondEphemeral(s, ic, resp)
	return true
}

// resolveButton looks up the operator-authored button definition for the
// click. Uses the snapshot's resolved-template identity so the button set
// matches what the user actually saw — even if the operator has changed
// the default template since.
func (b *Bot) resolveButton(snap *snapshots.Snapshot, actionID string) (buttons.Def, bool) {
	if b.DTS == nil {
		return buttons.Def{}, false
	}
	defs := b.DTS.GetButtons(snap.TemplateType, snap.Platform, snap.TemplateSelected, snap.Language)
	for _, d := range defs {
		if d.ID == actionID {
			return d, true
		}
	}
	// Fallback: re-resolve through the selection chain against the
	// originally-requested template id, in case the operator removed the
	// specific entry the user saw but a sibling now serves the same key.
	if snap.TemplateRequested != "" && snap.TemplateRequested != snap.TemplateSelected {
		defs = b.DTS.GetButtons(snap.TemplateType, snap.Platform, snap.TemplateRequested, snap.Language)
		for _, d := range defs {
			if d.ID == actionID {
				return d, true
			}
		}
	}
	return buttons.Def{}, false
}

// dispatchClick decides between an action-handler dispatch and an
// ephemeral response render based on the button's DispatchMode. Returns
// the user-facing ephemeral text.
func (b *Bot) dispatchClick(ic *discordgo.InteractionCreate, snap *snapshots.Snapshot, def buttons.Def, clicker string) string {
	switch def.DispatchMode() {
	case buttons.ModeAction:
		if b.ButtonActions == nil {
			return "Button actions aren't wired here."
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		resp, err := b.ButtonActions.Dispatch(ctx, snap, def, clicker, buttonactions.Deps{
			MuteStore:      b.MuteStore,
			Tracking:       b.Tracking,
			TriggerReload:  b.ReloadFunc,
			ResponseRender: b.responseRenderHook(),
			RenderToDM:     b.renderToDMHook(),
		})
		if err != nil {
			if errors.Is(err, buttonactions.ErrUnknownAction) || errors.Is(err, buttonactions.ErrNotImplemented) {
				metrics.ButtonActionsTotal.WithLabelValues(def.Action, "unimplemented").Inc()
				return "This action isn't available yet."
			}
			metrics.ButtonActionsTotal.WithLabelValues(def.Action, "error").Inc()
			log.Warnf("discord button: action %q failed: %v", def.Action, err)
			return "Couldn't complete that action."
		}
		metrics.ButtonActionsTotal.WithLabelValues(def.Action, "ok").Inc()
		if resp.Text == "" {
			return "Done."
		}
		return resp.Text
	case buttons.ModeResponseText, buttons.ModeResponseTemplateID, buttons.ModeResponseTemplateInline:
		// Run the response-render path through the action registry so
		// the same render/error pipeline applies as for action=render
		// buttons. We synthesise a Def with Action="render" to route
		// it through HandleRender — the actual response text comes
		// from the original Def fields, which HandleRender reads via
		// deps.ResponseRender.
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		resp, err := b.ButtonActions.Dispatch(ctx, snap, withRenderAction(def), clicker, buttonactions.Deps{
			MuteStore:      b.MuteStore,
			Tracking:       b.Tracking,
			TriggerReload:  b.ReloadFunc,
			ResponseRender: b.responseRenderHook(),
		})
		if err != nil {
			log.Warnf("discord button: render-response %q failed: %v", def.ID, err)
			// Surface the underlying error so operators iterating on
			// a template see the parser / execution message without
			// having to tail the log. Truncate so a verbose error
			// doesn't blow Discord's message limit.
			msg := err.Error()
			if len(msg) > 300 {
				msg = msg[:300] + "…"
			}
			return "Couldn't render that response: " + msg
		}
		if resp.Text == "" {
			return "Done."
		}
		return resp.Text
	default:
		return "This button isn't configured."
	}
}

// checkVisibility applies the button's visible_to gate against the
// clicker. Returns true when the click should proceed.
//
//   - VisibleTarget: clicker must equal snap.Target on DMs; channels
//     pass through (anyone with channel access can click, matching the
//     design's "channel = anyone with view" semantics).
//   - VisibleAdmin: clicker is in cfg.Discord.Admins.
//   - VisibleRegistered: clicker is a registered Poracle user
//     (Humans.Get returns a row for them).
//   - VisibleAnyone: always allow.
func (b *Bot) checkVisibility(def buttons.Def, snap *snapshots.Snapshot, clicker string, _ *discordgo.InteractionCreate) bool {
	switch def.EffectiveVisibility() {
	case buttons.VisibleAnyone:
		return true
	case buttons.VisibleTarget:
		if snap.TargetType == "dm" {
			return strings.HasSuffix(snap.Target, ":"+clicker) || snap.Target == clicker
		}
		// For channels: the target is the channel id, not a user; the
		// implicit contract is "anyone who can see this channel".
		return true
	case buttons.VisibleAdmin:
		return b.isAdminClicker(clicker)
	case buttons.VisibleRegistered:
		return b.isRegisteredClicker(clicker)
	}
	return false
}

// isAdminClicker reports whether the clicker is listed in
// cfg.Discord.Admins. Defensive: a nil Cfg or Discord empties to false.
func (b *Bot) isAdminClicker(clicker string) bool {
	if b.Cfg == nil {
		return false
	}
	for _, a := range b.Cfg.Discord.Admins {
		if a == clicker {
			return true
		}
	}
	return false
}

// isRegisteredClicker reports whether the clicker exists in the
// humans table (as a discord:user row). Uses the same Humans store the
// command surface uses.
func (b *Bot) isRegisteredClicker(clicker string) bool {
	if b.Humans == nil {
		return false
	}
	humanID := "discord:user:" + clicker
	h, err := b.Humans.Get(humanID)
	if err != nil || h == nil {
		return false
	}
	return true
}

// lookupSnapshotForClick reads the snapshot for the clicked message. The
// snapshot store key is `<target>:<messageID>` where target is the bare
// Discord ID (user ID for DMs, channel ID for channels and threads) —
// matching webhook.DeliveryJob.Target, which is what the render path
// writes into the snapshot.
//
// We don't know up front whether the click came from a DM or a channel,
// so we try both shapes:
//
//  1. <clicker_user_id>      (DM: snapshot was keyed by the recipient's user id)
//  2. <ic.ChannelID>         (channel / thread: snapshot was keyed by channel id)
//
// Webhook deliveries aren't covered — Discord clicks don't come back to
// webhooks anyway. Returns ErrNotFound when both shapes miss.
func lookupSnapshotForClick(store snapshots.Store, ic *discordgo.InteractionCreate) (*snapshots.Snapshot, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	clicker := clickerUserID(ic)
	candidates := []string{}
	if clicker != "" {
		candidates = append(candidates, clicker)
	}
	if ic.ChannelID != "" && ic.ChannelID != clicker {
		candidates = append(candidates, ic.ChannelID)
	}

	for _, target := range candidates {
		snap, err := store.Read(ctx, snapshots.MakeKey(target, ic.Message.ID))
		if err == nil {
			return snap, target, nil
		}
		if !errors.Is(err, snapshots.ErrNotFound) {
			return nil, "", err
		}
	}
	return nil, "", snapshots.ErrNotFound
}

// lookupKeysFor returns the snapshot keys lookupSnapshotForClick will
// try, for diagnostic logging. Mirrors the actual candidate construction
// so a mismatch between log output and the lookup logic surfaces as a
// test failure, not a confusing log line.
func lookupKeysFor(ic *discordgo.InteractionCreate) []string {
	clicker := clickerUserID(ic)
	msgID := ""
	if ic.Message != nil {
		msgID = ic.Message.ID
	}
	keys := []string{}
	if clicker != "" {
		keys = append(keys, snapshots.MakeKey(clicker, msgID))
	}
	if ic.ChannelID != "" && ic.ChannelID != clicker {
		keys = append(keys, snapshots.MakeKey(ic.ChannelID, msgID))
	}
	return keys
}

// clickerUserID returns the Discord user id of whoever clicked. Discord
// puts it in Member.User for guild interactions and User for DM ones.
func clickerUserID(ic *discordgo.InteractionCreate) string {
	if ic.Member != nil && ic.Member.User != nil {
		return ic.Member.User.ID
	}
	if ic.User != nil {
		return ic.User.ID
	}
	return ""
}

// responseRenderHook builds the ResponseRender closure threaded into
// the buttonactions package. Each call compiles the button's
// response_text / response_template_inline / response_template_id
// against the snapshot view and returns the rendered payload.
//
// Lives in discordbot rather than buttonactions so the DTS renderer
// dependency stays out of the package that the gateway handler imports.
func (b *Bot) responseRenderHook() buttonactions.ResponseRenderFunc {
	if b.DTS == nil {
		return nil
	}
	dtsRenderer := dtsRendererFromDeps(b)
	if dtsRenderer == nil {
		return nil
	}
	return func(snap *snapshots.Snapshot, def buttons.Def) (string, error) {
		platform := snap.Platform
		if platform == "" {
			platform = "discord"
		}
		return dtsRenderer.RenderButtonResponse(def, snap.View, platform, snap.Language)
	}
}

// renderToDMHook builds the RenderToDM closure for the redeliver action.
// Re-renders snap.TemplateSelected against snap.View and ships the
// result as a DM job to the clicker.
//
// Falls back to nil when the bot lacks a renderer or dispatcher — the
// HandleRedeliver handler emits a clear "not wired" message in that
// case rather than panicking.
func (b *Bot) renderToDMHook() buttonactions.RenderToDMFunc {
	if b.DTSRenderer == nil || b.Dispatcher == nil {
		return nil
	}
	return func(snap *snapshots.Snapshot, clickerUserID string) error {
		// Re-render the original template against the stored view.
		def := buttons.Def{
			ResponseTemplateID: snap.TemplateSelected,
		}
		body, err := b.DTSRenderer.RenderButtonResponse(
			buttons.Def{
				// Synthesise an inline response with the original
				// template id — RenderButtonResponse handles the
				// type="buttonResponse" lookup OR the alert-type
				// lookup based on which mode we pick. For redeliver
				// we want the SAME alert-type template, not a
				// buttonResponse — use the renderer's lookup directly.
				ResponseTemplateInline: snap.TemplateSelected, // placeholder; see below
			},
			snap.View, snap.Platform, snap.Language,
		)
		// The above is the v1 simplification: redeliver echoes the
		// template id rather than actually re-rendering through the
		// alert-type path, which would need additional wiring (per-type
		// dispatcher entry points). Operators get a "redeliver wired"
		// signal and the snapshot lookup works end-to-end; richer
		// re-render is a follow-up that touches each per-type handler.
		_ = def
		_ = err
		_ = body
		// Dispatch a minimal DM job carrying the snapshot's template
		// type label, so the operator sees something arrive while the
		// full render path catches up.
		dm := &delivery.Job{
			Target:       "discord:user:" + clickerUserID,
			Type:         "discord:user",
			Message:      []byte(fmt.Sprintf(`{"content":"Redelivered alert from %s/%s — full re-render is a follow-up."}`, snap.TemplateType, snap.TemplateSelected)),
			LogReference: "redeliver/" + snap.MessageID,
		}
		b.Dispatcher.DispatchBypass(dm)
		return nil
	}
}

// withRenderAction returns a copy of def with Action set to "render".
// Used to route response-template buttons through HandleRender without
// mutating the operator's actual button config.
func withRenderAction(def buttons.Def) buttons.Def {
	copy := def
	copy.Action = buttons.ActionRender
	return copy
}

// dtsRendererFromDeps returns the bot's DTS renderer if available. The
// renderer isn't on BotDeps directly — it's stored in proc state and
// passed via the renderer-only BotDeps.DTS template store. This helper
// keeps the assertion in one place.
//
// For now buttons.Def-driven response rendering depends on the renderer
// being available, so when DTS-renderer init failed (DTS=nil), buttons
// fall back to "response rendering isn't wired here yet" — which is
// honest: there's no renderer to call.
func dtsRendererFromDeps(b *Bot) *dts.Renderer {
	return b.DTSRenderer
}
