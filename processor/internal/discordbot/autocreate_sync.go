package discordbot

import (
	"fmt"
	"path/filepath"
	"sync"

	"github.com/bwmarrin/discordgo"
	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/config"
	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/geofence"
)

// syncFenceCandidate is one fence that classifyFences decided to act on.
// args and rawArgs are the rendered params ready to pass to applyAutocreate.
type syncFenceCandidate struct {
	fence   geofence.Fence
	args    []string // lower-cased, for bot parser
	rawArgs []string // original case, for channel/category names
}

// syncSkip records a fence that couldn't be processed and why.
type syncSkip struct {
	Fence  string
	Reason string
}

// syncFenceLog records what happened to one fence during the removal
// phase: which Discord IDs were touched (or would be, in dry-run) and a
// reason if the action was skipped.
type syncFenceLog struct {
	Fence   string            `json:"fence"`
	Channel string            `json:"channel,omitempty"`
	Threads map[string]string `json:"threads,omitempty"`
	Reason  string            `json:"reason,omitempty"`
}

// classifyResult is the output of classifyFences.
type classifyResult struct {
	toCreate []syncFenceCandidate // fences that need a new channel/category
	toReuse  []syncFenceCandidate // fences already in state — reuse their channel
	orphans  []string             // fence names in state no longer in the current fence list
	skipped  []syncSkip           // fences that failed filter or params rendering, with reasons
}

// classifyFences diffs the current geofence list for a rule against the
// rule's cached state, producing the sets the runner needs to act on.
//
// Filter rendering errors and params rendering errors cause the fence to
// be moved to skipped rather than propagating a hard error — a single bad
// fence (e.g. one with a missing property the filter references) must not
// abort the entire run.
func classifyFences(rule config.AutocreateRule, fences []geofence.Fence, state autocreateRuleState) classifyResult {
	var res classifyResult

	// Build a set of fence names currently in state for orphan detection.
	inState := make(map[string]bool, len(state.Fences))
	for name := range state.Fences {
		inState[name] = true
	}

	// Track which state entries we matched against a live fence.
	matched := make(map[string]bool)

	for _, f := range fences {
		// Evaluate filter.
		ok, err := renderFilter(rule.Filter, f)
		if err != nil {
			log.Warnf("autocreate sync %q: filter error for fence %q: %v (skipping)", rule.Name, f.Name, err)
			res.skipped = append(res.skipped, syncSkip{Fence: f.Name, Reason: fmt.Sprintf("filter render error: %v", err)})
			continue
		}
		if !ok {
			// Filtered out — not an error, not an orphan.
			continue
		}

		// Render params, then tokenise each rendered element so a single
		// param like "{{group}} {{name}}" expands to two args. Quoted
		// segments stay as one token (`"Gent Centrum"` → one arg) to match
		// the bot parser's behaviour.
		rendered, err := renderParams(rule.Params, f)
		if err != nil {
			log.Warnf("autocreate sync %q: params error for fence %q: %v (skipping)", rule.Name, f.Name, err)
			res.skipped = append(res.skipped, syncSkip{Fence: f.Name, Reason: fmt.Sprintf("params render error: %v", err)})
			continue
		}
		var rawArgs []string
		for _, p := range rendered {
			rawArgs = append(rawArgs, tokenizeParamString(p)...)
		}
		args := make([]string, len(rawArgs))
		for i, a := range rawArgs {
			args[i] = lowerASCII(a)
		}

		candidate := syncFenceCandidate{fence: f, args: args, rawArgs: rawArgs}

		if _, exists := state.Fences[f.Name]; exists {
			matched[f.Name] = true
			res.toReuse = append(res.toReuse, candidate)
		} else {
			res.toCreate = append(res.toCreate, candidate)
		}
	}

	// Any state entry not matched by a live fence is an orphan.
	for name := range state.Fences {
		if !matched[name] {
			res.orphans = append(res.orphans, name)
		}
	}

	return res
}

// syncCachePath returns the on-disk path for the bulk-autocreate cache,
// rooted at the project's config directory. Mirrors the convention used
// by other config/.cache files.
func syncCachePath(baseDir string) string {
	return filepath.Join(baseDir, "config", ".cache", "autocreate.json")
}

// SyncRuleOptions controls a single sync invocation.
type SyncRuleOptions struct {
	DryRun   bool
	Reset    bool // re-run template commands on reused channels
	Removals bool // permit orphan removal (PR 6 wires this up)
	Force    bool // bypass safety threshold (PR 6 wires this up)
}

// SyncOneRuleResult captures what one SyncOneRule invocation did.
type SyncOneRuleResult struct {
	// Rule is the name of the rule that was (or would be) synced.
	Rule string
	// DryRun mirrors the input opts.DryRun so callers rendering the result
	// (bot summary, API response) can label the run accordingly.
	DryRun bool
	// Note carries a short human-readable status when the call was short-circuited
	// (e.g. "already syncing").
	Note string
	// Created is the set of fence names for which a new channel was created.
	Created []string
	// Reused is the set of fence names whose channel was reused (no reset).
	Reused []string
	// Orphans lists fence names that were in the cache but not in the current
	// fence set. Raw classification output — populated regardless of whether
	// removal ran.
	Orphans []string
	// Removed is the per-fence log of orphan removals (or "would-remove"
	// entries when the rule does not opt in or the safety threshold blocked
	// the phase). Populated by the removal cascade.
	Removed []syncFenceLog
	// Skipped lists fences that were excluded due to filter or params rendering
	// errors, with the reason for each.
	Skipped []syncSkip
	// Errors contains non-fatal errors encountered during the run. The run
	// continues past them so a single failing fence doesn't abort the batch.
	Errors []error
}

// autocreateSyncer owns the per-rule mutexes and the shared on-disk cache
// for bulk-autocreate runs.
type autocreateSyncer struct {
	mu    sync.Mutex             // guards cache reads/writes
	cache autocreateCache        // loaded from disk, mutated in-place
	locks map[string]*sync.Mutex // per-rule mutex (lazy)
}

// newAutocreateSyncer constructs an empty syncer. The cache is populated
// on first use via ensureLoaded.
func newAutocreateSyncer() *autocreateSyncer {
	return &autocreateSyncer{
		cache: autocreateCache{},
		locks: map[string]*sync.Mutex{},
	}
}

// ruleLock returns (creating if necessary) the per-rule mutex. The global
// mu guards the locks map itself; callers take the returned mutex
// independently.
func (as *autocreateSyncer) ruleLock(name string) *sync.Mutex {
	as.mu.Lock()
	defer as.mu.Unlock()
	if m, ok := as.locks[name]; ok {
		return m
	}
	m := &sync.Mutex{}
	as.locks[name] = m
	return m
}

// loadCache reads the on-disk cache into as.cache. Missing file is benign.
// Must be called with as.mu held.
func (as *autocreateSyncer) loadCache(baseDir string) error {
	cache, err := loadAutocreateCache(syncCachePath(baseDir))
	if err != nil {
		return err
	}
	as.cache = cache
	return nil
}

// SyncOneRule runs one autocreate rule: loads the channel template, fetches
// the current geofence list, classifies fences (create/reuse/orphan),
// calls applyAutocreate for each actionable fence, and persists the
// updated cache.
//
// The per-rule mutex prevents two concurrent triggers from racing on the
// same rule. Fences in the toReuse set are applied with ResetOnReuse=false
// so admin-tweaked tracking survives scheduled re-syncs. opts.ResetOnReuse
// may override this for explicit "reset" triggers.
//
// Orphan removal is not yet implemented (PR 6). Orphans surface in the
// returned result's Orphans slice with a log entry.
func (b *Bot) SyncOneRule(s *discordgo.Session, rule config.AutocreateRule, opts SyncRuleOptions) SyncOneRuleResult {
	res := SyncOneRuleResult{Rule: rule.Name, DryRun: opts.DryRun}

	// Serialize concurrent runs of the same rule. TryLock returns false
	// immediately when another goroutine is already syncing this rule — the
	// caller gets a "already syncing" note instead of blocking indefinitely.
	lock := b.autocreateSync.ruleLock(rule.Name)
	if !lock.TryLock() {
		res.Note = "already syncing"
		return res
	}
	defer lock.Unlock()

	// Load templates.
	templates, err := b.loadChannelTemplates()
	if err != nil {
		log.Errorf("autocreate sync %q: load templates: %v", rule.Name, err)
		res.Errors = append(res.Errors, err)
		return res
	}

	var tmpl *channelTemplate
	for i := range templates {
		if templates[i].Name == rule.Template {
			tmpl = &templates[i]
			break
		}
	}
	if tmpl == nil {
		err := fmt.Errorf("channel template %q not found", rule.Template)
		log.Errorf("autocreate sync %q: %v", rule.Name, err)
		res.Errors = append(res.Errors, err)
		return res
	}

	// Fetch current geofence list.
	st := b.StateMgr.Get()
	var fences []geofence.Fence
	if st != nil {
		fences = st.Fences
	}

	// Load the cache to get existing state for this rule.
	as := b.autocreateSync
	as.mu.Lock()
	if err := as.loadCache(b.Cfg.BaseDir); err != nil {
		log.Warnf("autocreate sync %q: load cache: %v (proceeding with empty state)", rule.Name, err)
	}
	ruleState := autocreateRuleState{}
	if existing, ok := as.cache[rule.Name]; ok && existing != nil {
		ruleState = *existing
	}
	as.mu.Unlock()

	// Drop cache entries pointing at deleted Discord channels/categories/threads before the diff loop runs.
	reconcileCacheAgainstLive(&ruleState, b.fetchLiveIDs(rule.Guild))

	// Classify fences.
	classified := classifyFences(rule, fences, ruleState)
	res.Skipped = classified.skipped
	res.Orphans = classified.orphans

	// Build the actor from the bot's own identity. The bulk runner has no
	// originating MessageCreate; commands run as the bot user.
	actor := &autocreateActor{
		UserID:    s.State.User.ID,
		UserName:  s.State.User.Username,
		ChannelID: "",
	}

	// Ensure ruleState.Fences is initialised.
	if ruleState.Fences == nil {
		ruleState.Fences = map[string]*autocreateFenceState{}
	}
	ruleState.GuildID = rule.Guild

	// Process fences to create.
	for _, c := range classified.toCreate {
		rep := &collectingReporter{}
		applyOpts := applyAutocreateOptions{
			ResetOnReuse: false, // new channels have nothing to reset
			DryRun:       opts.DryRun,
		}
		result := b.applyAutocreate(s, actor, tmpl, c.args, c.rawArgs, rule.Guild, rep, applyOpts)
		for _, msg := range rep.infos {
			log.Infof("autocreate sync %q [%s]: %s", rule.Name, c.fence.Name, msg)
		}
		for _, msg := range rep.warns {
			log.Warnf("autocreate sync %q [%s]: %s", rule.Name, c.fence.Name, msg)
		}
		for _, msg := range rep.errors {
			log.Errorf("autocreate sync %q [%s]: %s", rule.Name, c.fence.Name, msg)
		}
		if len(result.Errors) > 0 {
			res.Errors = append(res.Errors, result.Errors...)
			continue
		}

		if !opts.DryRun {
			// Update cache state for this fence.
			fs := &autocreateFenceState{
				CategoryID: result.CategoryID,
			}
			// Find the channel ID from the result. The first channel in the
			// template's channel list is the authoritative master.
			for _, chID := range result.ChannelIDs {
				fs.ChannelID = chID
				break
			}
			// Collect thread IDs.
			if len(result.ThreadIDs) > 0 {
				fs.ThreadIDs = map[string]string{}
				for _, labelMap := range result.ThreadIDs {
					for label, id := range labelMap {
						fs.ThreadIDs[label] = id
					}
				}
			}
			ruleState.Fences[c.fence.Name] = fs

			// Ensure category is tracked.
			if result.CategoryID != "" {
				found := false
				for _, cat := range ruleState.Categories {
					if cat.ID == result.CategoryID {
						found = true
						break
					}
				}
				if !found {
					// Use first rawArg as a proxy for category name (it's the
					// group name by convention). Real name comes from the
					// template's categoryName field but we don't have it here.
					catName := ""
					if len(c.rawArgs) > 0 {
						catName = c.rawArgs[0]
					}
					ruleState.Categories = append(ruleState.Categories, autocreateCategory{
						Name: catName,
						ID:   result.CategoryID,
					})
				}
			}
		}

		res.Created = append(res.Created, c.fence.Name)
	}

	// Process fences to reuse.
	for _, c := range classified.toReuse {
		rep := &collectingReporter{}
		applyOpts := applyAutocreateOptions{
			// Bulk sync defaults to not resetting existing tracking;
			// only override if the trigger explicitly requested reset.
			ResetOnReuse: opts.Reset,
			DryRun:       opts.DryRun,
		}
		result := b.applyAutocreate(s, actor, tmpl, c.args, c.rawArgs, rule.Guild, rep, applyOpts)
		for _, msg := range rep.infos {
			log.Infof("autocreate sync %q [%s]: %s", rule.Name, c.fence.Name, msg)
		}
		for _, msg := range rep.warns {
			log.Warnf("autocreate sync %q [%s]: %s", rule.Name, c.fence.Name, msg)
		}
		for _, msg := range rep.errors {
			log.Errorf("autocreate sync %q [%s]: %s", rule.Name, c.fence.Name, msg)
		}
		if len(result.Errors) > 0 {
			res.Errors = append(res.Errors, result.Errors...)
		}

		res.Reused = append(res.Reused, c.fence.Name)
	}

	// Orphan removals (gated on rule.RemoveMissing AND opts.Removals AND
	// the safety threshold). Runs after creates/reuses so a sync that
	// fails the safety check still adds new fences.
	if rule.RemoveMissing && opts.Removals && len(classified.orphans) > 0 {
		allowed, note := applyRemovalSafety(len(ruleState.Fences), len(classified.orphans),
			b.Cfg.Autocreate.RemovalSafetyMaxPercent, opts)
		if !allowed {
			res.Note = note
		} else {
			for _, name := range classified.orphans {
				res.Removed = append(res.Removed, b.removeOrphanFence(&ruleState, name, opts))
			}
			res.Removed = append(res.Removed, b.removeEmptyManagedCategories(&ruleState, opts)...)
		}
	} else if len(classified.orphans) > 0 {
		for _, name := range classified.orphans {
			reason := "remove_missing=false"
			if !opts.Removals {
				reason = "trigger did not request removals"
			}
			res.Removed = append(res.Removed, syncFenceLog{Fence: name, Reason: reason})
		}
	}

	// Persist updated cache (skip on dry run).
	if !opts.DryRun {
		as.mu.Lock()
		if as.cache == nil {
			as.cache = autocreateCache{}
		}
		as.cache[rule.Name] = &ruleState
		if err := saveAutocreateCache(syncCachePath(b.Cfg.BaseDir), as.cache); err != nil {
			log.Warnf("autocreate sync %q: save cache: %v", rule.Name, err)
		}
		as.mu.Unlock()
	}

	// Trigger a state reload if anything was created. Skipped on dry-run
	// since no DB rows were touched and a reload would just be a wasteful
	// round-trip.
	if !opts.DryRun && len(res.Created) > 0 && b.ReloadFunc != nil {
		b.ReloadFunc()
	}

	return res
}

// liveDiscordIDs is the set of currently-existing channel/category/thread
// IDs in a guild — used by reconcile to prune dead entries from the rule's
// cache before the diff loop runs.
type liveDiscordIDs struct {
	channels map[string]bool // includes both regular channels and categories
	threads  map[string]bool // active threads only (archived are handled by the keep-alive sweeper)
}

// fetchLiveIDs queries Discord for all current channel/category/thread IDs
// in a guild. Returns empty sets on error so reconcile becomes a safe no-op
// instead of nuking the cache on a transient API hiccup.
func (b *Bot) fetchLiveIDs(guildID string) liveDiscordIDs {
	out := liveDiscordIDs{
		channels: map[string]bool{},
		threads:  map[string]bool{},
	}
	chans, err := b.session.GuildChannels(guildID)
	if err != nil {
		log.Warnf("autocreate sync: GuildChannels(%s): %v", guildID, err)
		return out
	}
	for _, c := range chans {
		out.channels[c.ID] = true
	}
	threads, err := b.session.GuildThreadsActive(guildID)
	if err != nil {
		log.Warnf("autocreate sync: GuildThreadsActive(%s): %v", guildID, err)
		return out
	}
	for _, t := range threads.Threads {
		out.threads[t.ID] = true
	}
	return out
}

// reconcileCacheAgainstLive drops cache entries pointing at IDs that no
// longer exist in Discord. Pure cache-pruning — never touches Discord.
func reconcileCacheAgainstLive(state *autocreateRuleState, live liveDiscordIDs) {
	if state == nil {
		return
	}

	// Drop dead categories.
	if len(state.Categories) > 0 {
		kept := state.Categories[:0]
		dead := map[string]bool{}
		for _, cat := range state.Categories {
			if live.channels[cat.ID] {
				kept = append(kept, cat)
			} else {
				dead[cat.ID] = true
			}
		}
		state.Categories = kept
		// Wipe fence.category_id refs to dead categories.
		for _, fs := range state.Fences {
			if dead[fs.CategoryID] {
				fs.CategoryID = ""
			}
		}
	}

	// Per-fence: drop missing channel + thread IDs.
	for _, fs := range state.Fences {
		if fs.ChannelID != "" && !live.channels[fs.ChannelID] {
			fs.ChannelID = ""
			fs.ThreadIDs = nil // a deleted parent channel deletes its threads
			continue
		}
		if len(fs.ThreadIDs) > 0 {
			for label, tid := range fs.ThreadIDs {
				if !live.threads[tid] {
					delete(fs.ThreadIDs, label)
				}
			}
			if len(fs.ThreadIDs) == 0 {
				fs.ThreadIDs = nil
			}
		}
	}
}

// applyRemovalSafety decides whether the removal phase is allowed to
// proceed. Returns (true, "") when removal is permitted and
// (false, note) when it is blocked.
//
// Rules:
//   - DryRun never enforces — dry-run callers should always see what would
//     happen regardless of the threshold.
//   - Force bypasses the check.
//   - maxPercent == 0 disables the check entirely.
//   - The check only engages when the cache is large enough to make a
//     percentage meaningful (≥10 entries).
//   - Removal is blocked when orphanCount > (cacheSize * maxPercent / 100).
func applyRemovalSafety(cacheSize, orphanCount, maxPercent int, opts SyncRuleOptions) (allowed bool, note string) {
	if opts.DryRun || opts.Force {
		return true, ""
	}
	if maxPercent <= 0 || cacheSize < 10 {
		return true, ""
	}
	threshold := cacheSize * maxPercent / 100
	if orphanCount > threshold {
		return false, fmt.Sprintf(
			"removal safety: would remove %d/%d fences (%.0f%%, threshold %d%%); use force to override",
			orphanCount, cacheSize, float64(orphanCount)*100/float64(cacheSize), maxPercent,
		)
	}
	return true, ""
}

// removeOrphanFence deletes all Poracle humans + Discord artefacts for one
// orphaned fence and drops the fence from the rule's cache. Returns a
// syncFenceLog describing what was done (or what would be done in dry-run).
func (b *Bot) removeOrphanFence(state *autocreateRuleState, fenceName string, opts SyncRuleOptions) syncFenceLog {
	entry := syncFenceLog{Fence: fenceName}

	fs, ok := state.Fences[fenceName]
	if !ok || fs == nil {
		entry.Reason = "not in cache"
		return entry
	}

	// Copy thread IDs so the log captures them even after deletion.
	if len(fs.ThreadIDs) > 0 {
		entry.Threads = make(map[string]string, len(fs.ThreadIDs))
		for label, tid := range fs.ThreadIDs {
			entry.Threads[label] = tid
		}
	}
	entry.Channel = fs.ChannelID

	if !opts.DryRun {
		// Remove thread humans + Discord channels.
		for label, tid := range fs.ThreadIDs {
			if err := db.DeleteHumanAndTracking(b.DB, tid); err != nil {
				log.Warnf("autocreate sync: remove orphan %q thread %s (%s): %v", fenceName, label, tid, err)
			}
			if _, err := b.session.ChannelDelete(tid); err != nil {
				log.Warnf("autocreate sync: delete Discord thread %s for orphan %q: %v", tid, fenceName, err)
			}
		}

		// Remove Poracle webhooks on the channel + the channel human row.
		if fs.ChannelID != "" {
			webhooks, err := b.session.ChannelWebhooks(fs.ChannelID)
			if err != nil {
				log.Warnf("autocreate sync: list webhooks on %s for orphan %q: %v", fs.ChannelID, fenceName, err)
			}
			for _, wh := range webhooks {
				if wh.Name != "Poracle" {
					continue
				}
				url := fmt.Sprintf("https://discord.com/api/webhooks/%s/%s", wh.ID, wh.Token)
				if err := db.DeleteHumanAndTracking(b.DB, url); err != nil {
					log.Warnf("autocreate sync: remove orphan %q webhook %s tracking: %v", fenceName, wh.ID, err)
				}
				if err := b.session.WebhookDelete(wh.ID); err != nil {
					log.Warnf("autocreate sync: delete Discord webhook %s for orphan %q: %v", wh.ID, fenceName, err)
				}
			}
			if err := db.DeleteHumanAndTracking(b.DB, fs.ChannelID); err != nil {
				log.Warnf("autocreate sync: remove orphan %q channel %s tracking: %v", fenceName, fs.ChannelID, err)
			}
			if _, err := b.session.ChannelDelete(fs.ChannelID); err != nil {
				log.Warnf("autocreate sync: delete Discord channel %s for orphan %q: %v", fs.ChannelID, fenceName, err)
			}
		}

		delete(state.Fences, fenceName)
	}

	return entry
}

// removeEmptyManagedCategories deletes any category in state that no longer
// has a live fence pointing at it. Returns one syncFenceLog per category
// removed (using the category name as the Fence field and the category ID
// as the Channel field for operator visibility). Skipped on dry-run.
func (b *Bot) removeEmptyManagedCategories(state *autocreateRuleState, opts SyncRuleOptions) []syncFenceLog {
	if opts.DryRun || len(state.Categories) == 0 {
		return nil
	}

	// Build the set of category IDs still referenced by a surviving fence.
	used := map[string]bool{}
	for _, fs := range state.Fences {
		if fs != nil && fs.CategoryID != "" {
			used[fs.CategoryID] = true
		}
	}

	var logs []syncFenceLog
	kept := state.Categories[:0]
	for _, cat := range state.Categories {
		if used[cat.ID] {
			kept = append(kept, cat)
			continue
		}
		// Category has no surviving fences — delete it.
		if _, err := b.session.ChannelDelete(cat.ID); err != nil {
			log.Warnf("autocreate sync: delete empty category %s (%s): %v", cat.Name, cat.ID, err)
		}
		logs = append(logs, syncFenceLog{Fence: cat.Name, Channel: cat.ID, Reason: "empty category removed"})
	}
	state.Categories = kept
	return logs
}
