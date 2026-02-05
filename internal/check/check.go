package check

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/samzong/brew-updater/internal/api"
	"github.com/samzong/brew-updater/internal/brew"
	"github.com/samzong/brew-updater/internal/config"
	"github.com/samzong/brew-updater/internal/notify"
)

type Options struct {
	DryRun      bool
	ForceUpdate bool
	NotifyOnly  bool
	Verbose     bool
}

type OutdatedItem struct {
	Item      config.WatchItem
	Installed string
	Latest    string
}

type Result struct {
	Checked      int
	CheckedNames []string
	Outdated     []OutdatedItem
	Removed      []config.WatchItem
	Errors       []string
}

func Run(ctx context.Context, cfg config.Config, st config.State, opts Options) (Result, config.Config, config.State, error) {
	res := Result{}

	formulae, casks, err := brew.ListInstalled()
	if err != nil {
		return res, cfg, st, err
	}
	normalizeStateKeys(cfg, &st)

	// remove missing
	filtered := make([]config.WatchItem, 0, len(cfg.Watchlist))
	installed := make(map[string]string)
	for _, item := range cfg.Watchlist {
		version, typ, ok := installedVersion(formulae, casks, item)
		if !ok {
			res.Removed = append(res.Removed, item)
			delete(st.NextCheckAt, config.WatchKey(item.Name, item.Type))
			delete(st.LastVersions, config.WatchKey(item.Name, item.Type))
			delete(st.NextCheckAt, item.Name)
			delete(st.LastVersions, item.Name)
			continue
		}
		// keep type in sync with installed
		item.Type = typ
		installed[config.WatchKey(item.Name, item.Type)] = version
		filtered = append(filtered, item)
	}
	cfg.Watchlist = filtered
	cleanupStateKeys(cfg, &st)

	now := time.Now()
	due := dueItems(cfg, st, now)
	res.Checked = len(due)
	res.CheckedNames = namesFromItems(due)
	if len(due) == 0 {
		st.LastCheckAt = ptrTime(now)
		return res, cfg, st, nil
	}

	client := api.New()
	results := fetchLatest(ctx, client, due, &st)

	outdated := make([]OutdatedItem, 0)
	for _, r := range results {
		if r.err != nil {
			appendError(&st, fmt.Sprintf("%s: %v", r.item.Name, r.err))
			continue
		}
		url := api.URLFor(r.item)
		key := config.WatchKey(r.item.Name, r.item.Type)
		prevScheme := st.LastSchemes[key]
		if r.notModified {
			if last, ok := st.LastVersions[key]; ok {
				r.latest = last
			} else if last, ok := st.LastVersions[r.item.Name]; ok {
				r.latest = last
			}
			if scheme, ok := st.LastSchemes[key]; ok {
				r.scheme = scheme
			} else if scheme, ok := st.LastSchemes[r.item.Name]; ok {
				r.scheme = scheme
			}
		} else {
			if r.etag != "" {
				st.ETagCache[url] = r.etag
			}
			if r.latest != "" {
				st.LastVersions[key] = r.latest
				if key != r.item.Name {
					delete(st.LastVersions, r.item.Name)
				}
			}
			st.LastSchemes[key] = r.scheme
			if key != r.item.Name {
				delete(st.LastSchemes, r.item.Name)
			}
		}
		installedVersion := installed[key]
		if isOutdated(installedVersion, r.latest, r.scheme, prevScheme) {
			outdated = append(outdated, OutdatedItem{Item: r.item, Installed: installedVersion, Latest: r.latest})
		}
		// update next check time for this item
		st.NextCheckAt[key] = now.Add(time.Duration(r.item.IntervalMin) * time.Minute).Format(time.RFC3339)
		if key != r.item.Name {
			delete(st.NextCheckAt, r.item.Name)
		}
	}
	res.Outdated = outdated

	updated := false
	if opts.ForceUpdate && !opts.DryRun && !opts.NotifyOnly {
		if err := brew.Update(opts.Verbose); err != nil {
			appendError(&st, fmt.Sprintf("brew update failed: %v", err))
			notifyFailure(cfg, "brew update failed", err)
			st.LastCheckAt = ptrTime(now)
			return res, cfg, st, nil
		}
		updated = true
	}

	if len(outdated) == 0 {
		st.LastCheckAt = ptrTime(now)
		return res, cfg, st, nil
	}

	if opts.DryRun || opts.NotifyOnly {
		notifyUpdates(cfg, outdated, "Update available", true)
		st.LastCheckAt = ptrTime(now)
		return res, cfg, st, nil
	}

	if !updated && len(outdated) > 0 {
		if err := brew.Update(opts.Verbose); err != nil {
			appendError(&st, fmt.Sprintf("brew update failed: %v", err))
			notifyFailure(cfg, "brew update failed", err)
			st.LastCheckAt = ptrTime(now)
			return res, cfg, st, nil
		}
	}

	toUpgradeFormula, toUpgradeCask := splitByType(outdated, cfg)
	if len(toUpgradeFormula) > 0 {
		if names, err := brew.OutdatedFormula(toUpgradeFormula); err == nil {
			toUpgradeFormula = names
		} else {
			appendError(&st, fmt.Sprintf("brew outdated formula failed: %v", err))
		}
	}
	if len(toUpgradeCask) > 0 {
		if names, err := brew.OutdatedCask(toUpgradeCask, cfg.IncludeAutoUpdateCask); err == nil {
			toUpgradeCask = names
		} else {
			appendError(&st, fmt.Sprintf("brew outdated cask failed: %v", err))
		}
	}
	if len(toUpgradeFormula) == 0 && len(toUpgradeCask) == 0 {
		st.LastCheckAt = ptrTime(now)
		return res, cfg, st, nil
	}
	res.Outdated = filterOutdated(outdated, toUpgradeFormula, toUpgradeCask)
	if err := brew.UpgradeFormula(toUpgradeFormula, opts.Verbose); err != nil {
		appendError(&st, fmt.Sprintf("formula upgrade failed: %v", err))
		notifyFailure(cfg, "formula upgrade failed", err)
	}
	if err := brew.UpgradeCask(toUpgradeCask, cfg.IncludeAutoUpdateCask, opts.Verbose); err != nil {
		appendError(&st, fmt.Sprintf("cask upgrade failed: %v", err))
		notifyFailure(cfg, "cask upgrade failed", err)
	}

	st.LastUpdateAt = ptrTime(time.Now())
	st.LastCheckAt = ptrTime(time.Now())
	notifyUpdates(cfg, outdated, "Updated", false)

	return res, cfg, st, nil
}

type fetchResult struct {
	item        config.WatchItem
	latest      string
	scheme      int
	etag        string
	notModified bool
	err         error
}

func fetchLatest(ctx context.Context, client *api.Client, items []config.WatchItem, st *config.State) []fetchResult {
	jobs := make(chan config.WatchItem)
	results := make(chan fetchResult)
	workers := 4
	var wg sync.WaitGroup
	wg.Add(workers)
	for range workers {
		go func() {
			defer wg.Done()
			for item := range jobs {
				url := api.URLFor(item)
				etag := st.ETagCache[url]
				latest, newETag, notModified, err := client.FetchLatest(ctx, item, etag)
				results <- fetchResult{item: item, latest: latest.Version, scheme: latest.Scheme, etag: newETag, notModified: notModified, err: err}
			}
		}()
	}

	go func() {
		for _, item := range items {
			jobs <- item
		}
		close(jobs)
		wg.Wait()
		close(results)
	}()

	out := make([]fetchResult, 0, len(items))
	for r := range results {
		out = append(out, r)
	}
	return out
}

func dueItems(cfg config.Config, st config.State, now time.Time) []config.WatchItem {
	items := make([]config.WatchItem, 0)
	for _, item := range cfg.Watchlist {
		if item.IntervalMin == 0 {
			item.IntervalMin = config.DefaultIntervalMin
		}
		key := config.WatchKey(item.Name, item.Type)
		nextStr, ok := st.NextCheckAt[key]
		if !ok && key != item.Name {
			nextStr, ok = st.NextCheckAt[item.Name]
		}
		if !ok || nextStr == "" {
			items = append(items, item)
			continue
		}
		nextTime, err := time.Parse(time.RFC3339, nextStr)
		if err != nil {
			items = append(items, item)
			continue
		}
		if !now.Before(nextTime) {
			items = append(items, item)
		}
	}
	return items
}

func splitByType(outdated []OutdatedItem, cfg config.Config) ([]string, []string) {
	formulae := []string{}
	casks := []string{}
	for _, item := range outdated {
		policy := item.Item.Policy
		if policy == "" {
			policy = cfg.DefaultPolicy
		}
		if policy != "auto" {
			continue
		}
		if item.Item.Type == "cask" {
			casks = append(casks, item.Item.Name)
		} else {
			formulae = append(formulae, item.Item.Name)
		}
	}
	sort.Strings(formulae)
	sort.Strings(casks)
	return formulae, casks
}

func notifyUpdates(cfg config.Config, items []OutdatedItem, action string, forceAll bool) {
	n := notify.New(cfg.NotifyMethod)
	for _, item := range items {
		policy := item.Item.Policy
		if policy == "" {
			policy = cfg.DefaultPolicy
		}
		if forceAll || policy == "notify" || action == "Updated" {
			msg := fmt.Sprintf("%s %s → %s", item.Item.Name, item.Installed, item.Latest)
			_ = n.Notify("brew-updater", msg, "brew-updater upgrade "+item.Item.Name)
		}
	}
}

func notifyFailure(cfg config.Config, title string, err error) {
	n := notify.New(cfg.NotifyMethod)
	msg := strings.TrimSpace(err.Error())
	_ = n.Notify("brew-updater failed", title+": "+msg, "brew-updater status")
}

func appendError(st *config.State, msg string) {
	st.LastErrors = append(st.LastErrors, msg)
	if len(st.LastErrors) > 20 {
		st.LastErrors = st.LastErrors[len(st.LastErrors)-20:]
	}
}

func ptrTime(t time.Time) *time.Time {
	return &t
}

func namesFromItems(items []config.WatchItem) []string {
	names := make([]string, 0, len(items))
	for _, it := range items {
		names = append(names, it.Name)
	}
	sort.Strings(names)
	return names
}

func installedVersion(formulae map[string]string, casks map[string]string, item config.WatchItem) (string, string, bool) {
	switch item.Type {
	case "cask":
		v, ok := casks[item.Name]
		return v, "cask", ok
	case "formula":
		v, ok := formulae[item.Name]
		return v, "formula", ok
	}
	if v, ok := formulae[item.Name]; ok {
		return v, "formula", true
	}
	if v, ok := casks[item.Name]; ok {
		return v, "cask", true
	}
	return "", "", false
}

func normalizeStateKeys(cfg config.Config, st *config.State) {
	for _, item := range cfg.Watchlist {
		key := config.WatchKey(item.Name, item.Type)
		if key == item.Name {
			continue
		}
		if v, ok := st.NextCheckAt[item.Name]; ok {
			if _, exists := st.NextCheckAt[key]; !exists {
				st.NextCheckAt[key] = v
			}
		}
		if v, ok := st.LastVersions[item.Name]; ok {
			if _, exists := st.LastVersions[key]; !exists {
				st.LastVersions[key] = v
			}
		}
		if v, ok := st.LastSchemes[item.Name]; ok {
			if _, exists := st.LastSchemes[key]; !exists {
				st.LastSchemes[key] = v
			}
		}
	}
}

func cleanupStateKeys(cfg config.Config, st *config.State) {
	watched := make(map[string]bool)
	for _, item := range cfg.Watchlist {
		key := config.WatchKey(item.Name, item.Type)
		watched[key] = true
		watched[item.Name] = true
	}
	for key := range st.NextCheckAt {
		if !watched[key] {
			delete(st.NextCheckAt, key)
		}
	}
	for key := range st.LastVersions {
		if !watched[key] {
			delete(st.LastVersions, key)
		}
	}
	for key := range st.LastSchemes {
		if !watched[key] {
			delete(st.LastSchemes, key)
		}
	}
}

func filterOutdated(items []OutdatedItem, formulas []string, casks []string) []OutdatedItem {
	if len(items) == 0 {
		return items
	}
	allowed := map[string]bool{}
	for _, name := range formulas {
		allowed["formula:"+name] = true
	}
	for _, name := range casks {
		allowed["cask:"+name] = true
	}
	out := make([]OutdatedItem, 0, len(items))
	for _, item := range items {
		key := config.WatchKey(item.Item.Name, item.Item.Type)
		if allowed[key] {
			out = append(out, item)
		}
	}
	return out
}
