package update

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
)

const (
	defaultReleaseBaseURL = "https://github.com/jolehuit/clother/releases/download"
	checksumsAsset        = "checksums.txt"
	signatureAsset        = "checksums.txt.minisig"
)

// httpStatusError carries the HTTP status of a failed download so the caller can
// tell "the release publishes no signature" (404) from "the network is down".
type httpStatusError struct {
	url    string
	status string
	code   int
}

func (e *httpStatusError) Error() string {
	return fmt.Sprintf("download %s: status %s", e.url, e.status)
}

func isNotFound(err error) bool {
	var statusErr *httpStatusError
	return errors.As(err, &statusErr) && statusErr.code == http.StatusNotFound
}

func DownloadLatestIfNewer(ctx context.Context, current string) (string, string, func(), error) {
	if os.Getenv("CLOTHER_SKIP_SELF_UPDATE") == "1" {
		return "", "", nil, nil
	}

	metaURL, err := metadataURL()
	if err != nil {
		return "", "", nil, err
	}
	meta, err := fetchMetadata(ctx, metaURL)
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
	checksumsPath := filepath.Join(tmpDir, checksumsAsset)
	signaturePath := filepath.Join(tmpDir, signatureAsset)
	binaryPath := filepath.Join(tmpDir, "clother")

	assetURL, err := releaseAssetURL(version, assetName)
	if err != nil {
		cleanup()
		return "", nil, err
	}
	checksumsURL, err := releaseAssetURL(version, checksumsAsset)
	if err != nil {
		cleanup()
		return "", nil, err
	}
	signatureURL, err := releaseAssetURL(version, signatureAsset)
	if err != nil {
		cleanup()
		return "", nil, err
	}

	if err := downloadFile(ctx, assetURL, assetPath, maxAssetBytes); err != nil {
		cleanup()
		return "", nil, err
	}
	if err := downloadFile(ctx, checksumsURL, checksumsPath, maxMetadataBytes); err != nil {
		cleanup()
		return "", nil, err
	}
	// Authenticity first: the checksum file is served by the very origin it is
	// supposed to protect, so it is only worth anything once its signature has
	// been verified against a key embedded in this binary.
	if err := checkReleaseSignature(ctx, signatureURL, checksumsPath, signaturePath, releaseOriginIsHandPicked()); err != nil {
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

// allowUnsignedOriginEnv is the explicit, greppable opt-out for the documented
// local-mirror workflow (CLOTHER_RELEASE_BASE_URL=http://127.0.0.1:8000 while
// testing a release). Nothing else turns the refusal below back into a warning.
const allowUnsignedOriginEnv = "CLOTHER_ALLOW_UNSIGNED_ORIGIN"

// releaseOriginIsHandPicked reports whether the release origin was chosen
// through the environment instead of being the built-in GitHub one. Overridden
// in tests, which always serve from an httptest origin.
var releaseOriginIsHandPicked = func() bool {
	if os.Getenv(allowUnsignedOriginEnv) == "1" {
		return false
	}
	for _, key := range []string{"CLOTHER_UPDATE_URL", "CLOTHER_RELEASE_BASE_URL"} {
		if strings.TrimSpace(os.Getenv(key)) != "" {
			return true
		}
	}
	return false
}

// checkReleaseSignature downloads and verifies the minisign signature of
// checksums.txt. A signature that is published but does not verify is always
// fatal. A signature that is not published at all is fatal too as soon as this
// build embeds a real signing key (see signatureRequired); until then it is
// reported loudly on stderr rather than silently ignored.
//
// originHandPicked closes the gap that warning left open. The checksum file is
// served by the very origin it is supposed to protect, so on an origin the
// caller named by hand — CLOTHER_UPDATE_URL, CLOTHER_RELEASE_BASE_URL, a shell
// profile or a shared CI configuration — it proves strictly nothing, and a
// stderr warning drowns in the log while an arbitrary binary is installed over
// the running one. On such an origin a missing signature is fatal, whatever the
// keys this build embeds.
func checkReleaseSignature(ctx context.Context, signatureURL, checksumsPath, signaturePath string, originHandPicked bool) error {
	err := downloadFile(ctx, signatureURL, signaturePath, maxMetadataBytes)
	switch {
	case err == nil:
		return verifyReleaseSignature(checksumsPath, signaturePath)
	case isNotFound(err):
		if signatureRequired() {
			return fmt.Errorf("refusing to install: %w (%s)", errSignatureMissing, signatureURL)
		}
		if originHandPicked {
			return fmt.Errorf(
				"refusing to install from a hand-picked release origin: %w (%s); its checksum file proves nothing about authenticity — set %s=1 if you really mean it",
				errSignatureMissing, signatureURL, allowUnsignedOriginEnv)
		}
		fmt.Fprintf(warnOut,
			"warning: this release publishes no signature (%s); falling back to a checksum served by the same origin, which does not prove authenticity\n",
			signatureURL,
		)
		return nil
	default:
		return fmt.Errorf("release signature: %w", err)
	}
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

func releaseAssetURL(version, asset string) (string, error) {
	if base := strings.TrimRight(strings.TrimSpace(os.Getenv("CLOTHER_RELEASE_BASE_URL")), "/"); base != "" {
		validated, err := validateUpdateURL(base)
		if err != nil {
			return "", fmt.Errorf("CLOTHER_RELEASE_BASE_URL: %w", err)
		}
		return strings.TrimRight(validated, "/") + "/" + asset, nil
	}
	return strings.TrimRight(defaultReleaseBaseURL, "/") + "/" + displayVersion(version) + "/" + asset, nil
}

func downloadFile(ctx context.Context, url, path string, limit int64) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "clother-install")

	resp, err := downloadClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return &httpStatusError{url: url, status: resp.Status, code: resp.StatusCode}
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), ".download-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	// Read one byte past the limit so an oversized body is an error instead of a
	// silently truncated file.
	written, err := io.Copy(tmp, io.LimitReader(resp.Body, limit+1))
	if err != nil {
		tmp.Close()
		return err
	}
	if written > limit {
		tmp.Close()
		return fmt.Errorf("download %s: response exceeds %d bytes", url, limit)
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
		// A directory or symlink entry named "clother" used to produce an empty
		// 0755 file that was then installed over the working binary.
		if header.Typeflag != tar.TypeReg {
			return fmt.Errorf("clother entry in %s is not a regular file", assetPath)
		}
		if header.Size > maxBinaryBytes {
			return fmt.Errorf("clother entry in %s exceeds %d bytes", assetPath, int64(maxBinaryBytes))
		}

		tmp, err := os.CreateTemp(filepath.Dir(binaryPath), ".binary-*")
		if err != nil {
			return err
		}
		tmpPath := tmp.Name()
		defer os.Remove(tmpPath)

		written, err := io.Copy(tmp, io.LimitReader(tr, maxBinaryBytes+1))
		if err != nil {
			tmp.Close()
			return err
		}
		if written > maxBinaryBytes {
			tmp.Close()
			return fmt.Errorf("clother entry in %s exceeds %d bytes", assetPath, int64(maxBinaryBytes))
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
