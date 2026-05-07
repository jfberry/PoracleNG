package discordbot

import (
	"fmt"
	"path/filepath"
	"sync"

	"github.com/bwmarrin/discordgo"
	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/config"
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

		// Render params.
		rawArgs, err := renderParams(rule.Params, f)
		if err != nil {
			log.Warnf("autocreate sync %q: params error for fence %q: %v (skipping)", rule.Name, f.Name, err)
			res.skipped = append(res.skipped, syncSkip{Fence: f.Name, Reason: fmt.Sprintf("params render error: %v", err)})
			continue
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
	// Note carries a short human-readable status when the call was short-circuited
	// (e.g. "already syncing").
	Note string
	// Created is the set of fence names for which a new channel was created.
	Created []string
	// Reused is the set of fence names whose channel was reused (no reset).
	Reused []string
	// Orphans lists fence names that were in the cache but not in the current
	// fence set. In this PR removals are not yet implemented — they are
	// surfaced here as "would-remove" log entries only.
	Orphans []string
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
	res := SyncOneRuleResult{Rule: rule.Name}

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

	// Classify fences.
	classified := classifyFences(rule, fences, ruleState)
	res.Skipped = classified.skipped
	_ = opts.Removals // wired up in PR 6
	_ = opts.Force    // wired up in PR 6

	// Log orphans — removal is not yet implemented.
	for _, name := range classified.orphans {
		log.Infof("autocreate sync %q: orphan fence %q (would-remove, not yet implemented)", rule.Name, name)
	}
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

	// Trigger a state reload if anything was created.
	if len(res.Created) > 0 && b.ReloadFunc != nil {
		b.ReloadFunc()
	}

	return res
}
