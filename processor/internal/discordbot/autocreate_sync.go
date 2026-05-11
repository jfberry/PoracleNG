package discordbot

import (
	"fmt"
	"maps"
	"path/filepath"
	"strings"
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

	// Pre-compile filter and params once — expressions are invariant across
	// the fence loop. A compile error on either is surfaced uniformly for
	// every fence in res.skipped so operators can spot a bad rule at a glance.
	cf, err := compileFilter(rule.Filter)
	if err != nil {
		log.Warnf("autocreate sync %q: compile filter: %v (skipping all fences)", rule.Name, err)
		for _, f := range fences {
			res.skipped = append(res.skipped, syncSkip{Fence: f.Name, Reason: fmt.Sprintf("filter compile error: %v", err)})
		}
		return res
	}
	cp, err := compileParams(rule.Params)
	if err != nil {
		log.Warnf("autocreate sync %q: compile params: %v (skipping all fences)", rule.Name, err)
		for _, f := range fences {
			res.skipped = append(res.skipped, syncSkip{Fence: f.Name, Reason: fmt.Sprintf("params compile error: %v", err)})
		}
		return res
	}

	// Build a set of fence names currently in state for orphan detection.
	inState := make(map[string]bool, len(state.Fences))
	for name := range state.Fences {
		inState[name] = true
	}

	// Track which state entries we matched against a live fence.
	matched := make(map[string]bool)

	for _, f := range fences {
		// Evaluate filter using the pre-compiled template.
		ok, err := cf.matches(f)
		if err != nil {
			log.Warnf("autocreate sync %q: filter error for fence %q: %v (skipping)", rule.Name, f.Name, err)
			res.skipped = append(res.skipped, syncSkip{Fence: f.Name, Reason: fmt.Sprintf("filter render error: %v", err)})
			continue
		}
		if !ok {
			// Filtered out — not an error, not an orphan.
			continue
		}

		// Render params using pre-compiled templates. Each element of the
		// `params` array is exactly one positional arg — multi-word values
		// like a Group of "New York" stay as a single arg ("New York"),
		// they are NOT split on whitespace. quoteForCommand re-quotes the
		// arg later when it's substituted into a bot command, so the bot
		// parser sees one token.
		//
		// Operators who want multiple args render multiple `params`
		// elements (e.g. `params = ["{{group}}", "{{name}}"]`).
		rendered, err := cp.render(f)
		if err != nil {
			log.Warnf("autocreate sync %q: params error for fence %q: %v (skipping)", rule.Name, f.Name, err)
			res.skipped = append(res.skipped, syncSkip{Fence: f.Name, Reason: fmt.Sprintf("params render error: %v", err)})
			continue
		}
		rawArgs := append([]string(nil), rendered...)
		args := make([]string, len(rawArgs))
		for i, a := range rawArgs {
			args[i] = strings.ToLower(a)
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

// SyncStatus is the machine-readable outcome of a SyncOneRule call.
// API consumers should branch on this rather than substring-matching Note.
type SyncStatus string

const (
	// SyncStatusOK means the runner went through its normal path. May still
	// have errors in Errors[] for individual fences — check those separately.
	SyncStatusOK SyncStatus = "ok"
	// SyncStatusBusy means another sync of the same rule was already running
	// (TryLock failed). The caller can retry later.
	SyncStatusBusy SyncStatus = "busy"
	// SyncStatusSafetyBlocked means the removal phase was aborted because
	// the orphan ratio exceeded autocreate.removal_safety_max_percent.
	// Creates and reuses still ran. Re-trigger with `force` to override.
	SyncStatusSafetyBlocked SyncStatus = "safety_blocked"
	// SyncStatusSnapshotFailed means buildGuildSnapshot couldn't fetch
	// the full guild state (transient Discord API error). The runner
	// aborts the sync rather than risk reconcile wiping the cache or
	// applyAutocreate creating duplicates against incomplete data. The
	// caller can retry — the next scheduled sync will likely succeed.
	SyncStatusSnapshotFailed SyncStatus = "snapshot_failed"
)

// SyncOneRuleResult captures what one SyncOneRule invocation did.
type SyncOneRuleResult struct {
	// Rule is the name of the rule that was (or would be) synced.
	Rule string
	// Status is the machine-readable outcome — see SyncStatus*. Default ok.
	Status SyncStatus
	// DryRun mirrors the input opts.DryRun so callers rendering the result
	// (bot summary, API response) can label the run accordingly.
	DryRun bool
	// Note is the human-readable status message that pairs with Status.
	// Empty when Status==ok. Used by the bot summary and API debug output.
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

	// Granular action counters accumulated across every applyAutocreate
	// call for the rule. These exist because the fence-level Created /
	// Reused split hides cases where a "reused" fence actually had a new
	// thread or picker materialise under it. Sum >0 means "something
	// changed in Discord" (or would change, on dry-run).
	CategoriesCreated      int
	ChannelsCreated        int
	ChannelsReused         int
	ChannelsReset          int
	ChannelsMoved          int
	ChannelsRemoved        int // template-orphan channels actually removed
	ChannelsTemplateOrphan int // template-orphan channels logged as would-remove (no removals flag)
	ThreadsCreated         int
	ThreadsReused          int
	ThreadsReset           int
	ThreadsRemoved         int // template-orphan threads actually removed
	ThreadsTemplateOrphan  int // template-orphan threads logged as would-remove (no removals flag)
	PickerPostsCreated     int
	PickerPostsEdited      int
	PickerPostsDeleted     int
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

// accumulateApplyCounters folds the per-fence applyAutocreate result
// counters into the runner's rule-level summary. Same shape used by both
// the create and reuse loops.
func accumulateApplyCounters(res *SyncOneRuleResult, ar applyAutocreateResult) {
	res.CategoriesCreated += ar.CategoriesCreated
	res.ChannelsCreated += ar.ChannelsCreated
	res.ChannelsReused += ar.ChannelsReused
	res.ChannelsReset += ar.ChannelsReset
	res.ChannelsMoved += ar.ChannelsMoved
	res.ThreadsCreated += ar.ThreadsCreated
	res.ThreadsReused += ar.ThreadsReused
	res.ThreadsReset += ar.ThreadsReset
	res.ThreadsRemoved += ar.ThreadsRemoved
	res.ThreadsTemplateOrphan += ar.ThreadsTemplateOrphan
	res.PickerPostsCreated += ar.PickerPostsCreated
	res.PickerPostsEdited += ar.PickerPostsEdited
	res.PickerPostsDeleted += ar.PickerPostsDeleted
}

// SyncOneRule runs one autocreate rule: loads the channel template, fetches
// the current geofence list, classifies fences (create/reuse/orphan),
// calls applyAutocreate for each actionable fence, and persists the
// updated cache.
//
// The per-rule mutex prevents two concurrent triggers from racing on the
// same rule. Fences in the toReuse set are applied with ResetOnReuse=false
// so admin-tweaked tracking survives scheduled re-syncs. Setting opts.Reset
// flips that for explicit "reset" triggers.
func (b *Bot) SyncOneRule(s *discordgo.Session, rule config.AutocreateRule, opts SyncRuleOptions) SyncOneRuleResult {
	res := SyncOneRuleResult{Rule: rule.Name, Status: SyncStatusOK, DryRun: opts.DryRun}

	// Serialize concurrent runs of the same rule. TryLock returns false
	// immediately when another goroutine is already syncing this rule — the
	// caller gets a "already syncing" note instead of blocking indefinitely.
	lock := b.autocreateSync.ruleLock(rule.Name)
	if !lock.TryLock() {
		res.Status = SyncStatusBusy
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

	// Build the guild snapshot once. Drives both the reconcile pass below
	// and the per-fence applyAutocreate calls — a single GuildChannels +
	// GuildThreadsActive instead of per-fence round-trips that used to
	// dominate dry-run latency.
	snap := b.buildGuildSnapshot(rule.Guild)

	// On a transient Discord API failure the snapshot is incomplete.
	// Reconcile + pruneMissingForGuild interpret "absent from snapshot"
	// as "dead in Discord" and would wipe the cache for every fence in
	// the rule. Apply paths would also see live channels as missing and
	// create duplicates. Abort the sync entirely; the caller (or the
	// next scheduled run) can retry.
	if !snap.complete {
		res.Status = SyncStatusSnapshotFailed
		res.Note = "guild snapshot fetch failed (transient Discord API error); cache preserved, retry"
		log.Warnf("autocreate sync %q: %s", rule.Name, res.Note)
		return res
	}

	// Drop cache entries pointing at deleted Discord channels/categories/threads before the diff loop runs.
	reconcileCacheAgainstLive(&ruleState, snap)
	if pruned := b.threadCache.pruneMissingForGuild(rule.Guild, snap.threads); pruned > 0 {
		log.Infof("autocreate sync %q: pruned %d stale thread cache entries", rule.Name, pruned)
	}

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
			// Template-orphan thread removal uses the same gate as fence-
			// level orphan removal: rule opts in via RemoveMissing, run
			// opts in via the `removals` keyword.
			RemoveTemplateOrphans: rule.RemoveMissing && opts.Removals,
			DryRun:                opts.DryRun,
		}
		result := b.applyAutocreate(s, actor, snap, tmpl, c.args, c.rawArgs, rule.Guild, rep, applyOpts)
		accumulateApplyCounters(&res, result)
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

		if !opts.DryRun && result.MasterChannelID != "" {
			// Update cache state for this fence. Skipped on partial-
			// failure (no master) so the previous cache survives —
			// otherwise a half-applied sync wipes IDs the next sync
			// would need.
			fs := &autocreateFenceState{
				CategoryID: result.CategoryID,
				ChannelID:  result.MasterChannelID,
			}
			// Persist the full per-name channel map so reconcile, template-
			// orphan detection, and the removal cascade can act on every
			// channel — not just the master.
			if len(result.ChannelIDs) > 0 {
				fs.ChannelIDs = map[string]string{}
				maps.Copy(fs.ChannelIDs, result.ChannelIDs)
			}
			// Collect thread IDs per parent channel. Keep the per-channel
			// key so sibling channels with same-label threads don't
			// collide on save (the flat shape used to silently drop
			// the duplicate).
			if len(result.ThreadIDs) > 0 {
				fs.ThreadIDs = map[string]map[string]string{}
				for ch, labelMap := range result.ThreadIDs {
					perCh := map[string]string{}
					maps.Copy(perCh, labelMap)
					fs.ThreadIDs[ch] = perCh
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
			// Template-orphan thread removal uses the same gate as fence-
			// level orphan removal: rule opts in via RemoveMissing, run
			// opts in via the `removals` keyword.
			RemoveTemplateOrphans: rule.RemoveMissing && opts.Removals,
			DryRun:                opts.DryRun,
		}

		// Snapshot the previous cache state before apply so we can detect
		// template-orphan channels (cached channels whose name is no
		// longer in the template) after apply returns.
		var prevChannelIDs map[string]string
		if prev := ruleState.Fences[c.fence.Name]; prev != nil {
			prevChannelIDs = prev.ChannelIDs
			// Legacy upgrade path: cache file was written before the
			// per-name ChannelIDs map landed. Log once so the operator
			// knows orphan detection won't fire for THIS sync — the
			// next run will have ChannelIDs populated from result.
			if prev.ChannelID != "" && len(prev.ChannelIDs) == 0 {
				log.Infof("autocreate sync %q [%s]: cache pre-multi-channel; ChannelIDs will populate after this sync (template-orphan detection unavailable until then)", rule.Name, c.fence.Name)
			}
		}

		result := b.applyAutocreate(s, actor, snap, tmpl, c.args, c.rawArgs, rule.Guild, rep, applyOpts)
		accumulateApplyCounters(&res, result)
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

		// Template-orphan channels: any cached channel name not in the
		// new result.ChannelIDs. Same gate as the thread case.
		for cachedName, cachedID := range prevChannelIDs {
			if _, stillInTemplate := result.ChannelIDs[cachedName]; stillInTemplate {
				continue
			}
			if rule.RemoveMissing && opts.Removals {
				if opts.DryRun {
					log.Infof("autocreate sync %q [%s]: [dry-run] would remove template-orphan channel %q (%s)", rule.Name, c.fence.Name, cachedName, cachedID)
				} else {
					if errs := b.cascadeChannelDelete(b.session, cachedID, true); len(errs) > 0 {
						res.Errors = append(res.Errors, errs...)
					}
					log.Infof("autocreate sync %q [%s]: removed template-orphan channel %q (%s)", rule.Name, c.fence.Name, cachedName, cachedID)
				}
				res.ChannelsRemoved++
			} else {
				log.Infof("autocreate sync %q [%s]: cached channel %q (%s) is no longer in template — would remove with `removals` (rule.remove_missing must also be true)", rule.Name, c.fence.Name, cachedName, cachedID)
				res.ChannelsTemplateOrphan++
			}
		}

		// Update cache state from this run's result so a thread or
		// channel created during a reuse pass actually lands in the
		// cache (the previous code only persisted on the toCreate
		// loop, leaving reuse paths drifting). Skipped on partial-
		// failure (no master) so the previous cache survives.
		if !opts.DryRun && result.MasterChannelID != "" {
			fs := &autocreateFenceState{
				CategoryID: result.CategoryID,
				ChannelID:  result.MasterChannelID,
			}
			if len(result.ChannelIDs) > 0 {
				fs.ChannelIDs = map[string]string{}
				maps.Copy(fs.ChannelIDs, result.ChannelIDs)
			}
			if len(result.ThreadIDs) > 0 {
				fs.ThreadIDs = map[string]map[string]string{}
				for ch, labelMap := range result.ThreadIDs {
					perCh := map[string]string{}
					maps.Copy(perCh, labelMap)
					fs.ThreadIDs[ch] = perCh
				}
			}
			ruleState.Fences[c.fence.Name] = fs
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
			res.Status = SyncStatusSafetyBlocked
			res.Note = note
		} else {
			for _, name := range classified.orphans {
				res.Removed = append(res.Removed, b.removeOrphanFence(&ruleState, name, opts))
			}
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

	// Empty-category cleanup runs once at the end whenever any
	// removal path could have emptied a category — that's the
	// fence-level cascade above OR the template-orphan channel
	// path in the toReuse loop. Same gate as the rest:
	// rule.RemoveMissing && opts.Removals, AND not safety-blocked.
	if rule.RemoveMissing && opts.Removals && res.Status != SyncStatusSafetyBlocked {
		res.Removed = append(res.Removed, b.removeEmptyManagedCategories(&ruleState, opts)...)
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

// guildSnapshot is one-shot guild state used by both the reconcile pass
// (existence checks) and applyAutocreate (name lookups). Built once per
// sync to replace the per-fence GuildChannels round-trips that used to
// dominate dry-run latency.
type guildSnapshot struct {
	// channels indexes every channel/category/voice channel by its ID.
	// Used for O(1) metadata lookup on the reuse path — saves a Channel(id)
	// call per reused channel.
	channels map[string]*discordgo.Channel

	// categoriesByLowerName maps a lowercased category name to its ID.
	// Mirrors the previous findCategoryByName behaviour (Discord category
	// matches were already case-insensitive against ToLower(ch.Name)).
	categoriesByLowerName map[string]string

	// channelsByParentLowerName maps parent-ID → lowercased channel name → ID.
	// "" parent ID covers top-level channels. Mirrors findChannelByName.
	channelsByParentLowerName map[string]map[string]string

	// threads is the set of active thread IDs in the guild. Archived
	// threads are out of scope (the keep-alive sweeper handles those).
	threads map[string]bool

	// rolesByLowerName maps a lowercased role name to its ID. Built from
	// a single s.GuildRoles(guildID) call at sync start so per-fence
	// permission-overwrite computation doesn't fan out into 2N more
	// Discord round-trips.
	rolesByLowerName map[string]string

	// complete indicates every required Discord listing succeeded.
	// reconcileCacheAgainstLive treats absence-from-snapshot as "dead in
	// Discord" and prunes the cache accordingly — that's correct only
	// when the snapshot is authoritative. On a transient API failure
	// (incomplete snapshot) the runner skips reconcile to avoid wiping
	// the cache for every fence in the rule.
	complete bool
}

// buildGuildSnapshot makes one GuildChannels + GuildThreadsActive +
// GuildRoles call set and returns the indexed view. On any API error the
// returned snapshot has complete=false so reconcile / pruneMissingForGuild
// can refuse to act on incomplete data. The map fields are still non-nil
// so name/role/thread lookups against an incomplete snapshot return
// "not found" without nil-deref.
func (b *Bot) buildGuildSnapshot(guildID string) *guildSnapshot {
	out := &guildSnapshot{
		channels:                  map[string]*discordgo.Channel{},
		categoriesByLowerName:     map[string]string{},
		channelsByParentLowerName: map[string]map[string]string{},
		threads:                   map[string]bool{},
		rolesByLowerName:          map[string]string{},
	}
	chans, err := b.session.GuildChannels(guildID)
	if err != nil {
		log.Warnf("autocreate sync: GuildChannels(%s): %v", guildID, err)
		return out
	}
	for _, c := range chans {
		out.channels[c.ID] = c
		if c.Type == discordgo.ChannelTypeGuildCategory {
			out.categoriesByLowerName[strings.ToLower(c.Name)] = c.ID
			continue
		}
		byName, ok := out.channelsByParentLowerName[c.ParentID]
		if !ok {
			byName = map[string]string{}
			out.channelsByParentLowerName[c.ParentID] = byName
		}
		// Index by Discord's stored normalized form so template-rendered
		// pretty names (e.g. "Canterbury_(Wincheap)") match the slug
		// Discord actually stores (e.g. "canterbury_wincheap").
		byName[normalizeDiscordChannelName(c.Name)] = c.ID
	}
	threads, err := b.session.GuildThreadsActive(guildID)
	if err != nil {
		log.Warnf("autocreate sync: GuildThreadsActive(%s): %v", guildID, err)
		return out
	}
	for _, t := range threads.Threads {
		out.threads[t.ID] = true
	}
	roles, err := b.session.GuildRoles(guildID)
	if err != nil {
		log.Warnf("autocreate sync: GuildRoles(%s): %v", guildID, err)
		return out
	}
	for _, r := range roles {
		out.rolesByLowerName[strings.ToLower(r.Name)] = r.ID
	}
	out.complete = true
	return out
}

// findCategory returns the ID of the category whose name matches (case-
// insensitive), or "" if none. Empty snapshot returns "".
func (s *guildSnapshot) findCategory(name string) string {
	if s == nil {
		return ""
	}
	return s.categoriesByLowerName[strings.ToLower(name)]
}

// findChannel returns the ID of the channel under parentID whose name
// matches (case-insensitive, with Discord's slug normalization applied
// — parens and other disallowed chars stripped, spaces → hyphens), or
// "" if none. parentID may be "" to look for top-level channels. Empty
// snapshot returns "".
func (s *guildSnapshot) findChannel(parentID, name string) string {
	if s == nil {
		return ""
	}
	byName, ok := s.channelsByParentLowerName[parentID]
	if !ok {
		return ""
	}
	return byName[normalizeDiscordChannelName(name)]
}

// findChannelAnyParent looks up a channel by name across every category
// in the guild. Returns (channelID, currentParentID) for the first match,
// or ("", "") if no channel with that name exists anywhere. Used by the
// bulk runner to discover stranded channels left behind by a previous
// failed run (e.g. cross-category duplicates from before the snapshot-
// freshness fix) so they can be moved into the canonical category
// instead of creating yet another copy.
//
// If the same name exists under multiple parents (already-buggy state),
// the returned parent is whichever one Go iterates first — the caller
// then moves it into the chosen category, leaving any further duplicates
// to be cleaned up by orphan removal on a later sync.
func (s *guildSnapshot) findChannelAnyParent(name string) (string, string) {
	if s == nil {
		return "", ""
	}
	lname := normalizeDiscordChannelName(name)
	for parent, byName := range s.channelsByParentLowerName {
		if id, ok := byName[lname]; ok {
			return id, parent
		}
	}
	return "", ""
}

// removeChannel drops a channel from the by-parent index. Used after a
// move so subsequent same-sync lookups under the old parent miss it.
func (s *guildSnapshot) removeChannel(id, parentID, name string) {
	if s == nil {
		return
	}
	if byName, ok := s.channelsByParentLowerName[parentID]; ok {
		delete(byName, normalizeDiscordChannelName(name))
	}
	delete(s.channels, id)
}

// channelExists reports whether the given channel/category ID is in the
// live guild snapshot.
func (s *guildSnapshot) channelExists(id string) bool {
	if s == nil {
		return false
	}
	_, ok := s.channels[id]
	return ok
}

// threadExists reports whether the given thread ID is in the live active-
// threads set.
func (s *guildSnapshot) threadExists(id string) bool {
	if s == nil {
		return false
	}
	return s.threads[id]
}

// addCategory registers a freshly-created category so subsequent
// findCategory lookups within the same sync return its ID instead of ""
// (which would cause every fence in the same category to create its own
// duplicate). Safe to call multiple times.
func (s *guildSnapshot) addCategory(id, name string) {
	if s == nil {
		return
	}
	s.channels[id] = &discordgo.Channel{ID: id, Name: name, Type: discordgo.ChannelTypeGuildCategory}
	s.categoriesByLowerName[strings.ToLower(name)] = id
}

// addChannel registers a freshly-created channel under parentID so
// subsequent findChannel lookups within the same sync find it. Safe to
// call multiple times.
func (s *guildSnapshot) addChannel(id, parentID, name string) {
	if s == nil {
		return
	}
	s.channels[id] = &discordgo.Channel{ID: id, Name: name, ParentID: parentID, Type: discordgo.ChannelTypeGuildText}
	byName, ok := s.channelsByParentLowerName[parentID]
	if !ok {
		byName = map[string]string{}
		s.channelsByParentLowerName[parentID] = byName
	}
	byName[normalizeDiscordChannelName(name)] = id
}

// findRole returns the role ID matching the given name (case-insensitive),
// or "" if no live role with that name exists in the guild snapshot.
func (s *guildSnapshot) findRole(name string) string {
	if s == nil {
		return ""
	}
	return s.rolesByLowerName[strings.ToLower(name)]
}

// addRole registers a freshly-created role so subsequent findRole lookups
// within the same sync return its ID instead of "" (which would cause each
// fence to attempt its own GuildRoleCreate). Safe to call multiple times.
func (s *guildSnapshot) addRole(id, name string) {
	if s == nil {
		return
	}
	s.rolesByLowerName[strings.ToLower(name)] = id
}

// reconcileCacheAgainstLive drops cache entries pointing at IDs that no
// longer exist in Discord. Pure cache-pruning — never touches Discord.
func reconcileCacheAgainstLive(state *autocreateRuleState, snap *guildSnapshot) {
	if state == nil || snap == nil {
		return
	}

	// Drop dead categories.
	if len(state.Categories) > 0 {
		kept := state.Categories[:0]
		dead := map[string]bool{}
		for _, cat := range state.Categories {
			if snap.channelExists(cat.ID) {
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
		// Drop dead siblings from the per-name channel map.
		if len(fs.ChannelIDs) > 0 {
			for name, id := range fs.ChannelIDs {
				if !snap.channelExists(id) {
					delete(fs.ChannelIDs, name)
				}
			}
			if len(fs.ChannelIDs) == 0 {
				fs.ChannelIDs = nil
			}
		}
		if fs.ChannelID != "" && !snap.channelExists(fs.ChannelID) {
			fs.ChannelID = ""
			fs.ThreadIDs = nil // a deleted parent channel deletes its threads
			continue
		}
		// Walk the per-channel thread map; drop dead threads, drop the
		// channel entry entirely if all its threads went away.
		if len(fs.ThreadIDs) > 0 {
			for chName, labelMap := range fs.ThreadIDs {
				for label, tid := range labelMap {
					if !snap.threadExists(tid) {
						delete(labelMap, label)
					}
				}
				if len(labelMap) == 0 {
					delete(fs.ThreadIDs, chName)
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

	// Copy thread IDs (flat label→ID for the log) so the entry captures
	// what we touched even after deletion. Cross-channel label collisions
	// in a multi-channel template would clobber here, but the cache-side
	// schema preserves them and the cascade deletes both regardless of
	// which one shows up in the log.
	if len(fs.ThreadIDs) > 0 {
		entry.Threads = map[string]string{}
		for _, labelMap := range fs.ThreadIDs {
			for label, tid := range labelMap {
				entry.Threads[label] = tid
			}
		}
	}
	entry.Channel = fs.ChannelID

	// Bare orphan path: cache says fence is gone but reconcile already
	// cleared its IDs (channel/threads weren't in the live snapshot when
	// the sync started). The cache pruning still happens — but flag the
	// reason so the operator knows the cascade had nothing to act on.
	if fs.ChannelID == "" && len(fs.ThreadIDs) == 0 {
		entry.Reason = "no live channel/thread IDs in cache (pruned by reconcile) — Discord side already gone, or bot lacks visibility"
		if !opts.DryRun {
			delete(state.Fences, fenceName)
		}
		return entry
	}

	if !opts.DryRun {
		// Remove thread humans. Discord cascades thread deletion when the
		// parent channel is deleted below, so per-thread ChannelDelete calls
		// are unnecessary — they would only burn rate-limit budget.
		for _, labelMap := range fs.ThreadIDs {
			for label, tid := range labelMap {
				if err := db.DeleteHumanAndTracking(b.DB, tid); err != nil {
					log.Warnf("autocreate sync: remove orphan %q thread %s (%s): %v", fenceName, label, tid, err)
				}
			}
		}

		// Remove Poracle webhooks, the channel human row, and the Discord
		// channel itself (which cascades thread deletion server-side) for
		// every channel cached for this fence — master and any siblings
		// from multi-channel templates. Surface any cascade error in the
		// entry's Reason field so the summary tells the operator why a
		// channel survived (most often missing Manage Channels
		// permission).
		toDelete := map[string]struct{}{}
		if fs.ChannelID != "" {
			toDelete[fs.ChannelID] = struct{}{}
		}
		for _, id := range fs.ChannelIDs {
			if id != "" {
				toDelete[id] = struct{}{}
			}
		}
		var allErrs []error
		for chID := range toDelete {
			if errs := b.cascadeChannelDelete(b.session, chID, true); len(errs) > 0 {
				allErrs = append(allErrs, errs...)
			}
		}
		if len(allErrs) > 0 {
			msgs := make([]string, 0, len(allErrs))
			for _, e := range allErrs {
				msgs = append(msgs, e.Error())
			}
			entry.Reason = "cascade had errors: " + strings.Join(msgs, "; ")
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
