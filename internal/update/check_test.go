package update

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jolehuit/clother/internal/config"
)

func TestIsNewer(t *testing.T) {
	t.Parallel()

	if !isNewer("v3.0.1", "3.0.0") {
		t.Fatal("expected v3.0.1 to be newer than 3.0.0")
	}
	if isNewer("3.0.0", "v3.0.0") {
		t.Fatal("same version should not be newer")
	}
}

func TestMaybeMessageFetchesAndCachesUpdate(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	paths := config.Paths{CacheDir: root, UpdateCacheFile: filepath.Join(root, "update.json")}
	now := time.Unix(1_700_000_000, 0)

	calls := 0
	msg, err := maybeMessage(paths, "3.0.0", now, func(_ context.Context, _ string) (remoteMetadata, error) {
		calls++
		return remoteMetadata{
			Version: "v3.0.1",
			URL:     "https://example.com/v3.0.1",
		}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("expected one remote fetch, got %d", calls)
	}
	if !strings.Contains(msg, "v3.0.1") || !strings.Contains(msg, "clother install") {
		t.Fatalf("unexpected message: %q", msg)
	}

	msg, err = maybeMessage(paths, "3.0.0", now.Add(time.Hour), func(_ context.Context, _ string) (remoteMetadata, error) {
		t.Fatal("cache should suppress fresh re-fetch")
		return remoteMetadata{}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if msg != "" {
		t.Fatalf("expected cached notification to be suppressed, got %q", msg)
	}
}

func TestMaybeMessageSkipsDevVersion(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	paths := config.Paths{CacheDir: root, UpdateCacheFile: filepath.Join(root, "update.json")}
	msg, err := maybeMessage(paths, "dev", time.Now(), func(_ context.Context, _ string) (remoteMetadata, error) {
		t.Fatal("dev builds should not fetch updates")
		return remoteMetadata{}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if msg != "" {
		t.Fatalf("expected no message for dev builds, got %q", msg)
	}
}

func TestMaybeMessageCachesFailedRefresh(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	paths := config.Paths{CacheDir: root, UpdateCacheFile: filepath.Join(root, "update.json")}
	now := time.Unix(1_700_000_000, 0)

	calls := 0
	fetchErr := context.DeadlineExceeded
	msg, err := maybeMessage(paths, "3.0.0", now, func(_ context.Context, _ string) (remoteMetadata, error) {
		calls++
		return remoteMetadata{}, fetchErr
	})
	if err != nil {
		t.Fatal(err)
	}
	if msg != "" {
		t.Fatalf("expected no update message after failed fetch, got %q", msg)
	}
	if calls != 1 {
		t.Fatalf("expected one fetch call, got %d", calls)
	}

	msg, err = maybeMessage(paths, "3.0.0", now.Add(time.Hour), func(_ context.Context, _ string) (remoteMetadata, error) {
		t.Fatal("failed refresh should be cached within TTL")
		return remoteMetadata{}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if msg != "" {
		t.Fatalf("expected no message while refresh is cached, got %q", msg)
	}
}

func TestMaybeMessageKeepsCachedUpdateWhenRefreshFails(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	paths := config.Paths{CacheDir: root, UpdateCacheFile: filepath.Join(root, "update.json")}
	now := time.Unix(1_700_000_000, 0)

	_, err := maybeMessage(paths, "3.0.0", now, func(_ context.Context, _ string) (remoteMetadata, error) {
		return remoteMetadata{Version: "3.0.2"}, nil
	})
	if err != nil {
		t.Fatal(err)
	}

	msg, err := maybeMessage(paths, "3.0.0", now.Add(25*time.Hour), func(_ context.Context, _ string) (remoteMetadata, error) {
		return remoteMetadata{}, context.DeadlineExceeded
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(msg, "v3.0.2") {
		t.Fatalf("expected cached newer version in message, got %q", msg)
	}
}
