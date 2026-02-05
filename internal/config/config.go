package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const (
	AppName             = "brew-updater"
	DefaultTickInterval = 60
	DefaultIntervalMin  = 5
	MinIntervalMin      = 1
	MaxIntervalMin      = 1440
	DefaultPolicy       = "auto"
	DefaultNotifyMethod = "terminal-notifier"
	ConfigFileName      = "config.json"
	StateFileName       = "state.json"
)

var (
	ErrInvalidInterval = errors.New("invalid interval")
)

type Config struct {
	Version               int         `json:"version"`
	TickIntervalSec       int         `json:"tick_interval_sec"`
	DefaultPolicy         string      `json:"default_policy"`
	NotifyMethod          string      `json:"notify_method"`
	IncludeAutoUpdateCask bool        `json:"include_auto_update_cask"`
	Watchlist             []WatchItem `json:"watchlist"`
}

type WatchItem struct {
	Name        string    `json:"name"`
	Type        string    `json:"type"`
	Policy      string    `json:"policy,omitempty"`
	IntervalMin int       `json:"interval_min"`
	AddedAt     time.Time `json:"added_at"`
}

func DefaultConfig() Config {
	return Config{
		Version:               1,
		TickIntervalSec:       DefaultTickInterval,
		DefaultPolicy:         DefaultPolicy,
		NotifyMethod:          DefaultNotifyMethod,
		IncludeAutoUpdateCask: true,
		Watchlist:             []WatchItem{},
	}
}

func ConfigDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "Application Support", AppName), nil
}

func ResolveConfigPath(path string) (string, error) {
	if path != "" {
		return path, nil
	}
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, ConfigFileName), nil
}

func StatePathFromConfigPath(configPath string) string {
	return filepath.Join(filepath.Dir(configPath), StateFileName)
}

func EnsureDir(path string) error {
	dir := filepath.Dir(path)
	return os.MkdirAll(dir, 0o755)
}

func LoadConfig(path string) (Config, error) {
	cfg := DefaultConfig()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, err
	}
	if len(data) == 0 {
		return cfg, nil
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, err
	}
	return NormalizeConfig(cfg)
}

func SaveConfig(path string, cfg Config) error {
	if err := EnsureDir(path); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func NormalizeConfig(cfg Config) (Config, error) {
	cfg.TickIntervalSec = DefaultTickInterval
	if cfg.DefaultPolicy == "" {
		cfg.DefaultPolicy = DefaultPolicy
	}
	if cfg.NotifyMethod == "" {
		cfg.NotifyMethod = DefaultNotifyMethod
	}
	deduped := make([]WatchItem, 0, len(cfg.Watchlist))
	seen := make(map[string]int)
	now := time.Now()
	for _, item := range cfg.Watchlist {
		if item.IntervalMin == 0 {
			item.IntervalMin = DefaultIntervalMin
		}
		if err := ValidateInterval(item.IntervalMin); err != nil {
			return cfg, fmt.Errorf("invalid interval for %s: %w", item.Name, err)
		}
		if item.AddedAt.IsZero() {
			item.AddedAt = now
		}
		key := WatchKey(item.Name, item.Type)
		if idx, ok := seen[key]; ok {
			if !deduped[idx].AddedAt.IsZero() && deduped[idx].AddedAt.Before(item.AddedAt) {
				item.AddedAt = deduped[idx].AddedAt
			}
			deduped[idx] = item
			continue
		}
		seen[key] = len(deduped)
		deduped = append(deduped, item)
	}
	cfg.Watchlist = deduped
	return cfg, nil
}

func WatchKey(name string, typ string) string {
	if typ == "" {
		return name
	}
	return typ + ":" + name
}

func ValidateInterval(min int) error {
	if min < MinIntervalMin || min > MaxIntervalMin {
		return ErrInvalidInterval
	}
	return nil
}
