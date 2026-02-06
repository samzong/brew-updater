package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/samzong/brew-updater/internal/brew"
	"github.com/samzong/brew-updater/internal/check"
	"github.com/samzong/brew-updater/internal/config"
	"github.com/samzong/brew-updater/internal/launchd"
	"github.com/samzong/brew-updater/internal/lock"
	"github.com/samzong/brew-updater/internal/tui"
)

var (
	cfgPath string
	quiet   bool
	verbose bool
)

var rootCmd = &cobra.Command{
	Use:   "brew-updater",
	Short: "Aggressive Homebrew updater",
}

func Execute() {
	rootCmd.SilenceErrors = true
	rootCmd.SilenceUsage = true
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVar(&cfgPath, "config", "", "config file path")
	rootCmd.PersistentFlags().BoolVar(&quiet, "quiet", false, "reduce output")
	rootCmd.PersistentFlags().BoolVar(&verbose, "verbose", false, "verbose output")

	rootCmd.AddCommand(initCmd())
	rootCmd.AddCommand(watchCmd())
	rootCmd.AddCommand(listCmd())
	rootCmd.AddCommand(checkCmd())
	rootCmd.AddCommand(upgradeCmd())
	rootCmd.AddCommand(statusCmd())
	rootCmd.AddCommand(setCmd())
	rootCmd.AddCommand(launchdCmd())
}

func initCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize config and state",
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := config.ResolveConfigPath(cfgPath)
			if err != nil {
				return err
			}
			if _, err := os.Stat(path); err == nil {
				return fmt.Errorf("config already exists: %s", path)
			}
			cfg := config.DefaultConfig()
			st := config.DefaultState()
			if err := config.SaveConfig(path, cfg); err != nil {
				return err
			}
			if err := config.SaveState(config.StatePathFromConfigPath(path), st); err != nil {
				return err
			}
			fmt.Println("Initialized:", path)
			return nil
		},
	}
	return cmd
}

func watchCmd() *cobra.Command {
	var typ string
	var policy string
	var interval int
	cmd := &cobra.Command{
		Use:   "watch",
		Short: "Select packages to watch",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, st, path, statePath, err := loadConfigState(true)
			if err != nil {
				return err
			}
			if err := validateType(typ); err != nil {
				return err
			}

			formulae, casks, err := brew.ListInstalled()
			if err != nil {
				return err
			}
			existing := map[string]config.WatchItem{}
			for _, w := range cfg.Watchlist {
				existing[config.WatchKey(w.Name, w.Type)] = w
			}

			items := []tui.Item{}
			if typ == "formula" {
				for name := range formulae {
					items = append(items, tui.Item{Name: name, Type: "formula"})
				}
			} else if typ == "cask" {
				for name := range casks {
					items = append(items, tui.Item{Name: name, Type: "cask"})
				}
			} else {
				for name := range formulae {
					items = append(items, tui.Item{Name: name, Type: "formula"})
				}
				for name := range casks {
					items = append(items, tui.Item{Name: name, Type: "cask"})
				}
			}

			if len(items) == 0 {
				fmt.Println("No new packages to watch")
				return nil
			}
			sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
			if err := validatePolicy(policy); err != nil {
				return err
			}
			if interval != 0 && (interval < config.MinIntervalMin || interval > config.MaxIntervalMin) {
				return errors.New("interval-min must be 1-1440")
			}
			defaultPolicy := cfg.DefaultPolicy
			defaultInterval := config.DefaultIntervalMin
			if policy != "" {
				defaultPolicy = policy
			}
			if interval > 0 {
				defaultInterval = interval
			}

			preset := map[string]tui.Selection{}
			for _, item := range items {
				key := config.WatchKey(item.Name, item.Type)
				if w, ok := existing[key]; ok {
					preset[key] = tui.Selection{
						Name:        item.Name,
						Type:        item.Type,
						Policy:      w.Policy,
						IntervalMin: w.IntervalMin,
					}
				}
			}

			selected, cancelled, err := tui.RunWatch(items, defaultPolicy, defaultInterval, preset)
			if err != nil {
				return err
			}
			if cancelled {
				fmt.Println("Canceled")
				return nil
			}
			keep := []config.WatchItem{}
			if typ != "all" {
				for _, w := range cfg.Watchlist {
					if typ == "formula" && w.Type == "cask" {
						keep = append(keep, w)
					}
					if typ == "cask" && w.Type == "formula" {
						keep = append(keep, w)
					}
				}
			}
			now := time.Now()
			newList := make([]config.WatchItem, 0, len(selected))
			for _, sel := range selected {
				key := config.WatchKey(sel.Name, sel.Type)
				addedAt := now
				if w, ok := existing[key]; ok && !w.AddedAt.IsZero() {
					addedAt = w.AddedAt
				}
				newList = append(newList, config.WatchItem{
					Name:        sel.Name,
					Type:        sel.Type,
					Policy:      sel.Policy,
					IntervalMin: sel.IntervalMin,
					AddedAt:     addedAt,
				})
			}
			cfg.Watchlist = append(keep, newList...)

			watched := map[string]bool{}
			for _, w := range cfg.Watchlist {
				key := config.WatchKey(w.Name, w.Type)
				watched[key] = true
				watched[w.Name] = true
			}
			for name := range st.NextCheckAt {
				if !watched[name] {
					delete(st.NextCheckAt, name)
				}
			}
			for name := range st.LastVersions {
				if !watched[name] {
					delete(st.LastVersions, name)
				}
			}

			if err := config.SaveConfig(path, cfg); err != nil {
				return err
			}
			if err := config.SaveState(statePath, st); err != nil {
				return err
			}
			fmt.Printf("Updated watchlist: %d selected\n", len(selected))
			return nil
		},
	}
	cmd.Flags().StringVar(&typ, "type", "all", "formula|cask|all")
	cmd.Flags().StringVar(&policy, "policy", "", "auto|notify")
	cmd.Flags().IntVar(&interval, "interval-min", 0, "1-1440")
	return cmd
}

func listCmd() *cobra.Command {
	var typ string
	var policy string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List watched packages",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _, _, _, err := loadConfigState(true)
			if err != nil {
				return err
			}
			if err := validateType(typ); err != nil {
				return err
			}
			if err := validatePolicy(policy); err != nil {
				return err
			}
			tw := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
			fmt.Fprintln(tw, "NAME\tTYPE\tPOLICY\tINTERVAL")
			for _, w := range cfg.Watchlist {
				if typ != "" && typ != "all" && w.Type != typ {
					continue
				}
				p := w.Policy
				if p == "" {
					p = cfg.DefaultPolicy
				}
				if policy != "" && policy != p {
					continue
				}
				fmt.Fprintf(tw, "%s\t%s\t%s\t%dm\n", w.Name, w.Type, p, w.IntervalMin)
			}
			tw.Flush()
			return nil
		},
	}
	cmd.Flags().StringVar(&typ, "type", "all", "formula|cask|all")
	cmd.Flags().StringVar(&policy, "policy", "", "auto|notify")
	return cmd
}

func checkCmd() *cobra.Command {
	var dryRun bool
	var forceUpdate bool
	var notifyOnly bool
	cmd := &cobra.Command{
		Use:   "check",
		Short: "Check updates and upgrade if needed",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, st, path, _, err := loadConfigState(true)
			if err != nil {
				return err
			}
			lockPath := filepath.Join(filepath.Dir(path), "lock")
			l, err := lock.Acquire(lockPath, 10*time.Minute)
			if err != nil {
				if !quiet {
					fmt.Println("skip: another check running")
				}
				return nil
			}
			defer l.Release()

			if running, err := brew.HasRunningBrew(); err == nil && running {
				if !quiet {
					fmt.Println("skip: brew already running")
				}
				return nil
			}

			if !quiet {
				fmt.Println("checking...")
			}
			res, cfg, st, err := check.Run(context.Background(), cfg, st, check.Options{
				DryRun:      dryRun,
				ForceUpdate: forceUpdate,
				NotifyOnly:  notifyOnly,
				Verbose:     verbose,
			})
			if err != nil {
				return err
			}
			if err := config.SaveConfig(path, cfg); err != nil {
				return err
			}
			if err := config.SaveState(config.StatePathFromConfigPath(path), st); err != nil {
				return err
			}
			if quiet {
				return nil
			}
			if res.Checked == 0 {
				fmt.Println("no packages due for check")
				return nil
			}
			if verbose {
				fmt.Printf("checked=%d\n", res.Checked)
				fmt.Printf("checked packages: %s\n", joinNames(res.CheckedNames))
			} else {
				fmt.Printf("checked=%d: %s\n", res.Checked, joinNames(res.CheckedNames))
			}
			if len(res.Outdated) == 0 {
				fmt.Println("outdated=0")
			} else {
				if verbose {
					fmt.Printf("outdated=%d\n", len(res.Outdated))
					for _, item := range res.Outdated {
						fmt.Printf("- %s %s -> %s\n", item.Item.Name, item.Installed, item.Latest)
					}
				} else {
					names := make([]string, 0, len(res.Outdated))
					for _, item := range res.Outdated {
						names = append(names, item.Item.Name)
					}
					sort.Strings(names)
					fmt.Printf("outdated=%d: %s\n", len(names), joinNames(names))
				}
			}
			if len(res.Removed) > 0 {
				names := make([]string, 0, len(res.Removed))
				for _, r := range res.Removed {
					names = append(names, r.Name)
				}
				sort.Strings(names)
				fmt.Printf("removed=%d: %s\n", len(names), joinNames(names))
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "check only")
	cmd.Flags().BoolVar(&forceUpdate, "force-update", false, "force brew update")
	cmd.Flags().BoolVar(&notifyOnly, "notify-only", false, "notify only")
	return cmd
}

func upgradeCmd() *cobra.Command {
	var typ string
	var all bool
	cmd := &cobra.Command{
		Use:   "upgrade [name...]",
		Short: "Upgrade watched packages",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _, _, _, err := loadConfigState(true)
			if err != nil {
				return err
			}
			if err := validateType(typ); err != nil {
				return err
			}
			var targets []config.WatchItem
			if all || len(args) == 0 {
				targets = cfg.Watchlist
			} else {
				m := map[string]bool{}
				for _, n := range args {
					m[n] = true
				}
				for _, w := range cfg.Watchlist {
					if m[w.Name] {
						targets = append(targets, w)
					}
				}
			}
			if len(targets) == 0 {
				if !quiet {
					fmt.Println("no watched packages matched")
				}
				return nil
			}
			formulae, casks := splitTargets(targets, typ)
			if len(formulae) == 0 && len(casks) == 0 {
				if !quiet {
					fmt.Println("no watched packages matched")
				}
				return nil
			}
			sort.Strings(formulae)
			sort.Strings(casks)
			if !quiet {
				total := len(formulae) + len(casks)
				fmt.Printf("targets=%d\n", total)
				if len(formulae) > 0 {
					fmt.Printf("formula: %s\n", joinNames(formulae))
				}
				if len(casks) > 0 {
					fmt.Printf("cask: %s\n", joinNames(casks))
				}
				fmt.Println("brew update...")
			}
			if err := brew.Update(verbose); err != nil {
				return err
			}
			if len(formulae) > 0 {
				if names, err := brew.OutdatedFormula(formulae); err == nil {
					formulae = names
				} else {
					return err
				}
			}
			if len(casks) > 0 {
				if names, err := brew.OutdatedCask(casks, cfg.IncludeAutoUpdateCask); err == nil {
					casks = names
				} else {
					return err
				}
			}
			if len(formulae) == 0 && len(casks) == 0 {
				if !quiet {
					fmt.Println("no outdated packages")
				}
				return nil
			}
			if !quiet && len(formulae) > 0 {
				fmt.Printf("outdated formula: %s\n", joinNames(formulae))
				fmt.Println("brew upgrade formula...")
			}
			if err := brew.UpgradeFormula(formulae, verbose); err != nil {
				return err
			}
			if !quiet && len(casks) > 0 {
				fmt.Printf("outdated cask: %s\n", joinNames(casks))
				if cfg.IncludeAutoUpdateCask {
					fmt.Println("brew upgrade cask (greedy)...")
				} else {
					fmt.Println("brew upgrade cask...")
				}
			}
			if err := brew.UpgradeCask(casks, cfg.IncludeAutoUpdateCask, verbose); err != nil {
				return err
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "upgrade all watched packages")
	cmd.Flags().StringVar(&typ, "type", "all", "formula|cask|all")
	return cmd
}

func statusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show last check status",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, st, _, _, err := loadConfigState(true)
			if err != nil {
				return err
			}
			fmt.Println("last_check:", formatTime(st.LastCheckAt))
			fmt.Println("last_update:", formatTime(st.LastUpdateAt))
			if len(st.LastErrors) > 0 {
				fmt.Println("errors:")
				for _, e := range st.LastErrors {
					fmt.Println("-", e)
				}
			}
			return nil
		},
	}
	return cmd
}

func setCmd() *cobra.Command {
	var policy string
	var interval int
	cmd := &cobra.Command{
		Use:   "set <name...>",
		Short: "Update watchlist settings",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return errors.New("name required")
			}
			if err := validatePolicy(policy); err != nil {
				return err
			}
			if interval != 0 && (interval < config.MinIntervalMin || interval > config.MaxIntervalMin) {
				return errors.New("interval-min must be 1-1440")
			}
			cfg, _, path, _, err := loadConfigState(true)
			if err != nil {
				return err
			}
			set := map[string]bool{}
			for _, n := range args {
				set[n] = true
			}
			for i := range cfg.Watchlist {
				if !set[cfg.Watchlist[i].Name] {
					continue
				}
				if policy != "" {
					cfg.Watchlist[i].Policy = policy
				}
				if interval > 0 {
					cfg.Watchlist[i].IntervalMin = interval
				}
			}
			if err := config.SaveConfig(path, cfg); err != nil {
				return err
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&policy, "policy", "", "auto|notify")
	cmd.Flags().IntVar(&interval, "interval-min", 0, "1-1440")
	return cmd
}

func launchdCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "launchd"}
	cmd.AddCommand(launchdInstallCmd())
	cmd.AddCommand(launchdUninstallCmd())
	cmd.AddCommand(launchdStatusCmd())
	return cmd
}

func launchdInstallCmd() *cobra.Command {
	var interval int
	var startNow bool
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install launchd agent",
		RunE: func(cmd *cobra.Command, args []string) error {
			if interval != 0 && interval != 60 {
				return errors.New("interval-sec fixed to 60")
			}
			_, _, path, _, err := loadConfigState(true)
			if err != nil {
				return err
			}
			bin, err := os.Executable()
			if err != nil {
				return err
			}
			plist, err := launchd.Install(bin, path, startNow)
			if err != nil {
				return err
			}
			fmt.Println("installed:", plist)
			return nil
		},
	}
	cmd.Flags().IntVar(&interval, "interval-sec", 60, "fixed to 60")
	cmd.Flags().BoolVar(&startNow, "start-now", false, "run immediately")
	return cmd
}

func launchdUninstallCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Uninstall launchd agent",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := launchd.Uninstall(); err != nil {
				return err
			}
			fmt.Println("uninstalled")
			return nil
		},
	}
	return cmd
}

func launchdStatusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show launchd status",
		RunE: func(cmd *cobra.Command, args []string) error {
			on, err := launchd.Status()
			if err != nil {
				return err
			}
			fmt.Println("running:", on)
			return nil
		},
	}
	return cmd
}

func splitTargets(items []config.WatchItem, typ string) ([]string, []string) {
	formulae := []string{}
	casks := []string{}
	for _, item := range items {
		if typ != "all" && item.Type != typ {
			continue
		}
		if item.Type == "cask" {
			casks = append(casks, item.Name)
		} else {
			formulae = append(formulae, item.Name)
		}
	}
	return formulae, casks
}

func formatTime(t *time.Time) string {
	if t == nil {
		return "-"
	}
	return t.Format(time.RFC3339)
}

func loadConfigState(require bool) (config.Config, config.State, string, string, error) {
	path, err := config.ResolveConfigPath(cfgPath)
	if err != nil {
		return config.Config{}, config.State{}, "", "", err
	}
	if require {
		if _, err := os.Stat(path); err != nil {
			if os.IsNotExist(err) {
				return config.Config{}, config.State{}, "", "", errors.New("config not found, run 'brew-updater init'")
			}
			return config.Config{}, config.State{}, "", "", err
		}
	}
	cfg, err := config.LoadConfig(path)
	if err != nil {
		return config.Config{}, config.State{}, "", "", err
	}
	statePath := config.StatePathFromConfigPath(path)
	st, err := config.LoadState(statePath)
	if err != nil {
		return config.Config{}, config.State{}, "", "", err
	}
	return cfg, st, path, statePath, nil
}

func validatePolicy(policy string) error {
	if policy == "" {
		return nil
	}
	if policy != "auto" && policy != "notify" {
		return fmt.Errorf("invalid policy: %s", policy)
	}
	return nil
}

func validateType(typ string) error {
	if typ == "" {
		typ = "all"
	}
	if typ != "formula" && typ != "cask" && typ != "all" {
		return fmt.Errorf("invalid type: %s", typ)
	}
	return nil
}

func joinNames(names []string) string {
	if len(names) == 0 {
		return "-"
	}
	return strings.Join(names, ", ")
}
