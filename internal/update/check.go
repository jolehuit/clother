package update

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/jolehuit/clother/internal/config"
)

const (
	defaultMetadataURL = "https://github.com/jolehuit/clother/releases/latest/download/latest.json"
	checkTTL           = 24 * time.Hour
	notifyTTL          = 24 * time.Hour
)

type cacheFile struct {
	LastCheckedUnix  int64  `json:"last_checked_unix,omitempty"`
	LatestVersion    string `json:"latest_version,omitempty"`
	LatestURL        string `json:"latest_url,omitempty"`
	LastNotifiedUnix int64  `json:"last_notified_unix,omitempty"`
	LastNotifiedFor  string `json:"last_notified_for,omitempty"`
}

type remoteMetadata struct {
	Version string `json:"version"`
	URL     string `json:"url,omitempty"`
}

type fetchFunc func(context.Context, string) (remoteMetadata, error)

func MaybeMessage(paths config.Paths, current string, now time.Time) (string, error) {
	return maybeMessage(paths, current, now, fetchMetadata)
}

func maybeMessage(paths config.Paths, current string, now time.Time, fetch fetchFunc) (string, error) {
	if os.Getenv("CLOTHER_NO_UPDATE_CHECK") == "1" {
		return "", nil
	}
	if normalizeVersion(current) == "" || strings.EqualFold(strings.TrimSpace(current), "dev") {
		return "", nil
	}

	cache, err := loadCache(paths.UpdateCacheFile)
	if err != nil {
		return "", err
	}

	dirty := false
	if shouldRefresh(cache, now) {
		// Persist the check timestamp even when remote fetch fails, so we don't
		// retry on every command invocation in offline/unstable networks.
		cache.LastCheckedUnix = now.Unix()
		dirty = true
		ctx, cancel := context.WithTimeout(context.Background(), 1200*time.Millisecond)
		defer cancel()
		meta, err := fetch(ctx, metadataURL())
		if err == nil && normalizeVersion(meta.Version) != "" {
			cache.LatestVersion = displayVersion(meta.Version)
			cache.LatestURL = strings.TrimSpace(meta.URL)
			if cache.LatestURL == "" {
				cache.LatestURL = releaseURL(meta.Version)
			}
		}
	}

	if !isNewer(cache.LatestVersion, current) {
		if dirty {
			return "", saveCache(paths.UpdateCacheFile, cache)
		}
		return "", nil
	}

	notify := shouldNotify(cache, now)
	if notify {
		cache.LastNotifiedUnix = now.Unix()
		cache.LastNotifiedFor = cache.LatestVersion
		dirty = true
	}

	if dirty {
		if err := saveCache(paths.UpdateCacheFile, cache); err != nil {
			return "", err
		}
	}

	if !notify && cache.LastNotifiedFor == cache.LatestVersion {
		return "", nil
	}
	return fmt.Sprintf(
		"Update available: Clother %s (current %s). Run: clother install",
		displayVersion(cache.LatestVersion),
		displayVersion(current),
	), nil
}

func metadataURL() string {
	if override := strings.TrimSpace(os.Getenv("CLOTHER_UPDATE_URL")); override != "" {
		return override
	}
	return defaultMetadataURL
}

func shouldRefresh(cache cacheFile, now time.Time) bool {
	if cache.LastCheckedUnix == 0 {
		return true
	}
	return now.After(time.Unix(cache.LastCheckedUnix, 0).Add(checkTTL))
}

func shouldNotify(cache cacheFile, now time.Time) bool {
	if cache.LastNotifiedFor != cache.LatestVersion {
		return true
	}
	if cache.LastNotifiedUnix == 0 {
		return true
	}
	return now.After(time.Unix(cache.LastNotifiedUnix, 0).Add(notifyTTL))
}

func fetchMetadata(ctx context.Context, url string) (remoteMetadata, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return remoteMetadata{}, err
	}
	req.Header.Set("User-Agent", "clother-update-check")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return remoteMetadata{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return remoteMetadata{}, fmt.Errorf("update metadata status %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 32*1024))
	if err != nil {
		return remoteMetadata{}, err
	}
	var meta remoteMetadata
	if err := json.Unmarshal(body, &meta); err != nil {
		return remoteMetadata{}, err
	}
	return meta, nil
}

func loadCache(path string) (cacheFile, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return cacheFile{}, nil
	}
	if err != nil {
		return cacheFile{}, err
	}
	var cache cacheFile
	if err := json.Unmarshal(data, &cache); err != nil {
		return cacheFile{}, nil
	}
	return cache, nil
}

func saveCache(path string, cache cacheFile) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(path), ".update-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func releaseURL(version string) string {
	tag := displayVersion(version)
	return "https://github.com/jolehuit/clother/releases/tag/" + tag
}

func displayVersion(version string) string {
	version = normalizeVersion(version)
	if version == "" {
		return ""
	}
	return "v" + version
}

func normalizeVersion(version string) string {
	version = strings.TrimSpace(version)
	version = strings.TrimPrefix(strings.ToLower(version), "v")
	if version == "" {
		return ""
	}
	parts := strings.Split(version, ".")
	for _, part := range parts {
		if part == "" {
			return ""
		}
		if _, err := strconv.Atoi(part); err != nil {
			return ""
		}
	}
	return version
}

func isNewer(candidate, current string) bool {
	left := parseVersion(candidate)
	right := parseVersion(current)
	if len(left) == 0 || len(right) == 0 {
		return false
	}
	maxLen := len(left)
	if len(right) > maxLen {
		maxLen = len(right)
	}
	for len(left) < maxLen {
		left = append(left, 0)
	}
	for len(right) < maxLen {
		right = append(right, 0)
	}
	for i := 0; i < maxLen; i++ {
		if left[i] > right[i] {
			return true
		}
		if left[i] < right[i] {
			return false
		}
	}
	return false
}

func parseVersion(version string) []int {
	version = normalizeVersion(version)
	if version == "" {
		return nil
	}
	parts := strings.Split(version, ".")
	out := make([]int, 0, len(parts))
	for _, part := range parts {
		value, err := strconv.Atoi(part)
		if err != nil {
			return nil
		}
		out = append(out, value)
	}
	return out
}
