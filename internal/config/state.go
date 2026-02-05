package config

import (
	"encoding/json"
	"os"
	"time"
)

type State struct {
	LastCheckAt  *time.Time        `json:"last_check_at,omitempty"`
	LastUpdateAt *time.Time        `json:"last_update_at,omitempty"`
	LastVersions map[string]string `json:"last_versions"`
	LastSchemes  map[string]int    `json:"last_schemes"`
	ETagCache    map[string]string `json:"etag_cache"`
	LastErrors   []string          `json:"last_errors"`
	NextCheckAt  map[string]string `json:"next_check_at"`
}

func DefaultState() State {
	return State{
		LastVersions: make(map[string]string),
		LastSchemes:  make(map[string]int),
		ETagCache:    make(map[string]string),
		LastErrors:   []string{},
		NextCheckAt:  make(map[string]string),
	}
}

func LoadState(path string) (State, error) {
	st := DefaultState()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return st, nil
		}
		return st, err
	}
	if len(data) == 0 {
		return st, nil
	}
	if err := json.Unmarshal(data, &st); err != nil {
		return st, err
	}
	if st.LastVersions == nil {
		st.LastVersions = make(map[string]string)
	}
	if st.LastSchemes == nil {
		st.LastSchemes = make(map[string]int)
	}
	if st.ETagCache == nil {
		st.ETagCache = make(map[string]string)
	}
	if st.NextCheckAt == nil {
		st.NextCheckAt = make(map[string]string)
	}
	if st.LastErrors == nil {
		st.LastErrors = []string{}
	}
	return st, nil
}

func SaveState(path string, st State) error {
	if err := EnsureDir(path); err != nil {
		return err
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
