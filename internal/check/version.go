package check

import (
	"strings"

	"github.com/Masterminds/semver/v3"
)

func normalizeVersion(v string) string {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "v")
	v = strings.ReplaceAll(v, "_", ".")
	v = strings.ReplaceAll(v, ",", ".")
	return v
}

func isLatest(v string) bool {
	return strings.EqualFold(strings.TrimSpace(v), "latest")
}

func isOutdated(installed, latest string, scheme int, prevScheme int) bool {
	if installed == "" || latest == "" {
		return false
	}
	if isLatest(installed) || isLatest(latest) {
		return false
	}
	if scheme > prevScheme && installed != latest {
		return true
	}
	inst := normalizeVersion(installed)
	lat := normalizeVersion(latest)

	iv, err1 := semver.NewVersion(inst)
	lv, err2 := semver.NewVersion(lat)
	if err1 == nil && err2 == nil {
		return lv.GreaterThan(iv)
	}
	return installed != latest
}
