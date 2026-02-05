package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/samzong/brew-updater/internal/config"
)

const (
	baseURL = "https://formulae.brew.sh/api"
)

type Client struct {
	httpClient *http.Client
}

type Latest struct {
	Version string
	Scheme  int
}

func New() *Client {
	return &Client{
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

func (c *Client) FetchLatest(ctx context.Context, item config.WatchItem, etag string) (Latest, string, bool, error) {
	url := buildURL(item)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Latest{}, "", false, err
	}
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return Latest{}, "", false, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotModified {
		return Latest{}, etag, true, nil
	}
	if resp.StatusCode != http.StatusOK {
		return Latest{}, "", false, fmt.Errorf("api status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return Latest{}, "", false, err
	}
	newETag := resp.Header.Get("ETag")

	latest, err := parseLatest(item.Type, body)
	if err != nil {
		return Latest{}, "", false, err
	}
	return latest, newETag, false, nil
}

func buildURL(item config.WatchItem) string {
	if item.Type == "cask" {
		return fmt.Sprintf("%s/cask/%s.json", baseURL, item.Name)
	}
	return fmt.Sprintf("%s/formula/%s.json", baseURL, item.Name)
}

func URLFor(item config.WatchItem) string {
	return buildURL(item)
}

type formulaResp struct {
	Version       string `json:"version"`
	Revision      int    `json:"revision"`
	VersionScheme int    `json:"version_scheme"`
	Versions      struct {
		Stable string `json:"stable"`
	} `json:"versions"`
}

type caskResp struct {
	Version string `json:"version"`
}

func parseLatest(typ string, body []byte) (Latest, error) {
	switch typ {
	case "cask":
		var c caskResp
		if err := json.Unmarshal(body, &c); err != nil {
			return Latest{}, err
		}
		return Latest{Version: c.Version, Scheme: 0}, nil
	default:
		var f formulaResp
		if err := json.Unmarshal(body, &f); err != nil {
			return Latest{}, err
		}
		version := f.Versions.Stable
		if version == "" {
			version = f.Version
		}
		if version != "" && f.Revision > 0 {
			version = fmt.Sprintf("%s_%d", version, f.Revision)
		}
		return Latest{Version: version, Scheme: f.VersionScheme}, nil
	}
}
