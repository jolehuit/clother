package update

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
)

const defaultReleaseBaseURL = "https://github.com/jolehuit/clother/releases/download"

func DownloadLatestIfNewer(ctx context.Context, current string) (string, string, func(), error) {
	if os.Getenv("CLOTHER_SKIP_SELF_UPDATE") == "1" {
		return "", "", nil, nil
	}

	meta, err := fetchMetadata(ctx, metadataURL())
	if err != nil {
		return "", "", nil, err
	}
	if !isNewer(meta.Version, current) {
		return "", "", nil, nil
	}

	version := displayVersion(meta.Version)
	binaryPath, cleanup, err := downloadReleaseBinary(ctx, version)
	if err != nil {
		return "", "", nil, err
	}
	return binaryPath, version, cleanup, nil
}

func downloadReleaseBinary(ctx context.Context, version string) (string, func(), error) {
	tmpDir, err := os.MkdirTemp("", "clother-update-*")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() { _ = os.RemoveAll(tmpDir) }

	assetName, err := releaseAssetName()
	if err != nil {
		cleanup()
		return "", nil, err
	}

	assetPath := filepath.Join(tmpDir, assetName)
	checksumsPath := filepath.Join(tmpDir, "checksums.txt")
	binaryPath := filepath.Join(tmpDir, "clother")

	if err := downloadFile(ctx, releaseAssetURL(version, assetName), assetPath); err != nil {
		cleanup()
		return "", nil, err
	}
	if err := downloadFile(ctx, releaseAssetURL(version, "checksums.txt"), checksumsPath); err != nil {
		cleanup()
		return "", nil, err
	}
	if err := verifyChecksum(assetPath, checksumsPath, assetName); err != nil {
		cleanup()
		return "", nil, err
	}
	if err := extractBinary(assetPath, binaryPath); err != nil {
		cleanup()
		return "", nil, err
	}
	return binaryPath, cleanup, nil
}

func releaseAssetName() (string, error) {
	switch goruntime.GOOS {
	case "darwin", "linux":
	default:
		return "", fmt.Errorf("unsupported operating system %q", goruntime.GOOS)
	}

	var arch string
	switch goruntime.GOARCH {
	case "amd64":
		arch = "amd64"
	case "arm64":
		arch = "arm64"
	default:
		return "", fmt.Errorf("unsupported architecture %q", goruntime.GOARCH)
	}

	return fmt.Sprintf("clother_%s_%s.tar.gz", goruntime.GOOS, arch), nil
}

func releaseAssetURL(version, asset string) string {
	if base := strings.TrimRight(strings.TrimSpace(os.Getenv("CLOTHER_RELEASE_BASE_URL")), "/"); base != "" {
		return base + "/" + asset
	}
	return strings.TrimRight(defaultReleaseBaseURL, "/") + "/" + displayVersion(version) + "/" + asset
}

func downloadFile(ctx context.Context, url, path string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "clother-install")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: status %s", url, resp.Status)
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), ".download-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if _, err := io.Copy(tmp, resp.Body); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func verifyChecksum(assetPath, checksumsPath, assetName string) error {
	data, err := os.ReadFile(checksumsPath)
	if err != nil {
		return err
	}

	expected := ""
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		if filepath.Base(fields[len(fields)-1]) == assetName {
			expected = fields[0]
			break
		}
	}
	if expected == "" {
		return fmt.Errorf("checksum for %s not found", assetName)
	}

	file, err := os.Open(assetPath)
	if err != nil {
		return err
	}
	defer file.Close()

	sum := sha256.New()
	if _, err := io.Copy(sum, file); err != nil {
		return err
	}
	if got := hex.EncodeToString(sum.Sum(nil)); !strings.EqualFold(got, expected) {
		return fmt.Errorf("checksum mismatch for %s", assetName)
	}
	return nil
}

func extractBinary(assetPath, binaryPath string) error {
	file, err := os.Open(assetPath)
	if err != nil {
		return err
	}
	defer file.Close()

	gz, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if filepath.Base(header.Name) != "clother" {
			continue
		}

		tmp, err := os.CreateTemp(filepath.Dir(binaryPath), ".binary-*")
		if err != nil {
			return err
		}
		tmpPath := tmp.Name()
		defer os.Remove(tmpPath)

		if _, err := io.Copy(tmp, tr); err != nil {
			tmp.Close()
			return err
		}
		if err := tmp.Chmod(0o755); err != nil {
			tmp.Close()
			return err
		}
		if err := tmp.Close(); err != nil {
			return err
		}
		return os.Rename(tmpPath, binaryPath)
	}
	return fmt.Errorf("clother binary not found in %s", assetPath)
}
