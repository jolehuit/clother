package clother_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestClotherShBootstrapsInstallerWhenPipedToBash(t *testing.T) {
	script, err := os.ReadFile("clother.sh")
	if err != nil {
		t.Fatal(err)
	}

	root := t.TempDir()
	bootstrapPath := filepath.Join(root, "install.sh")
	markerPath := filepath.Join(root, "ran.txt")
	argsPath := filepath.Join(root, "args.txt")

	bootstrap := fmt.Sprintf(`#!/usr/bin/env bash
set -euo pipefail
printf 'ok\n' > %q
printf '%%s\n' "$@" > %q
`, markerPath, argsPath)
	if err := os.WriteFile(bootstrapPath, []byte(bootstrap), 0o755); err != nil {
		t.Fatal(err)
	}

	workdir := filepath.Join(root, "workdir")
	if err := os.MkdirAll(workdir, 0o755); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("bash", "-s", "--", "install", "--bin-dir", "/tmp/clother-bin")
	cmd.Dir = workdir
	cmd.Stdin = bytes.NewReader(script)
	cmd.Env = append(os.Environ(), "CLOTHER_BOOTSTRAP_URL=file://"+bootstrapPath)

	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("bash failed: %v\n%s", err, output)
	}

	if _, err := os.Stat(markerPath); err != nil {
		t.Fatalf("expected bootstrap installer to run: %v", err)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(argsData)); got != "install\n--bin-dir\n/tmp/clother-bin" {
		t.Fatalf("bootstrap args = %q", got)
	}
}

func TestClotherShRefusesInsecureBootstrapURL(t *testing.T) {
	script, err := os.ReadFile("clother.sh")
	if err != nil {
		t.Fatal(err)
	}

	workdir := t.TempDir()
	cmd := exec.Command("bash", "-s", "--", "install")
	cmd.Dir = workdir
	cmd.Stdin = bytes.NewReader(script)
	cmd.Env = append(os.Environ(), "CLOTHER_BOOTSTRAP_URL=http://attacker.invalid/install.sh")

	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected a cleartext CLOTHER_BOOTSTRAP_URL to be refused, output:\n%s", output)
	}
	if !strings.Contains(string(output), "must use https") {
		t.Fatalf("unexpected output: %s", output)
	}
}

// ---------------------------------------------------------------------------
// scripts/install.sh
//
// install.sh is the documented `curl | bash` entry point: it downloads the
// release archive, verifies its SHA-256 and executes the binary. It had no test
// at all, and its checksum verification used to be fail-open.
// ---------------------------------------------------------------------------

type fakeRelease struct {
	url       string
	assetName string
	markerDir string
}

func releaseAsset(t *testing.T) string {
	t.Helper()
	switch runtime.GOOS {
	case "darwin", "linux":
	default:
		t.Skipf("install.sh does not support %s", runtime.GOOS)
	}
	return fmt.Sprintf("clother_%s_%s.tar.gz", runtime.GOOS, runtime.GOARCH)
}

// newFakeRelease serves an archive whose "clother" binary records the arguments
// it was called with, so a test can tell installed from refused.
func newFakeRelease(t *testing.T, mutate func(assetName string, files map[string][]byte)) fakeRelease {
	t.Helper()

	assetName := releaseAsset(t)
	markerDir := t.TempDir()
	markerPath := filepath.Join(markerDir, "installed.txt")

	payload := fmt.Sprintf("#!/usr/bin/env bash\nprintf '%%s\\n' \"$@\" > %q\n", markerPath)

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{
		Name:     "clother",
		Mode:     0o755,
		Size:     int64(len(payload)),
		Typeflag: tar.TypeReg,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte(payload)); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}

	archive := buf.Bytes()
	sum := sha256.Sum256(archive)
	files := map[string][]byte{
		assetName:       archive,
		"checksums.txt": []byte(fmt.Sprintf("%s  %s\n", hex.EncodeToString(sum[:]), assetName)),
	}
	if mutate != nil {
		mutate(assetName, files)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := files[strings.TrimPrefix(r.URL.Path, "/")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(body)
	}))
	t.Cleanup(server.Close)

	return fakeRelease{url: server.URL, assetName: assetName, markerDir: markerDir}
}

func runInstallSh(t *testing.T, release fakeRelease, extraEnv []string, args ...string) (string, error) {
	t.Helper()
	script, err := filepath.Abs("scripts/install.sh")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", append([]string{script}, args...)...)
	cmd.Dir = t.TempDir()
	cmd.Env = append(os.Environ(),
		"CLOTHER_INSTALL_MODE=release",
		"CLOTHER_RELEASE_BASE_URL="+release.url,
	)
	cmd.Env = append(cmd.Env, extraEnv...)
	output, err := cmd.CombinedOutput()
	return string(output), err
}

func TestInstallShInstallsVerifiedArchive(t *testing.T) {
	release := newFakeRelease(t, nil)
	output, err := runInstallSh(t, release, nil, "install", "--bin-dir", "/tmp/clother-bin")
	if err != nil {
		t.Fatalf("install.sh failed: %v\n%s", err, output)
	}
	marker, err := os.ReadFile(filepath.Join(release.markerDir, "installed.txt"))
	if err != nil {
		t.Fatalf("expected the verified binary to run: %v\n%s", err, output)
	}
	if got := strings.TrimSpace(string(marker)); got != "install\n--bin-dir\n/tmp/clother-bin" {
		t.Fatalf("binary args = %q", got)
	}
}

func TestInstallShRefusesTamperedArchive(t *testing.T) {
	release := newFakeRelease(t, func(assetName string, files map[string][]byte) {
		files["checksums.txt"] = []byte(strings.Repeat("0", 64) + "  " + assetName + "\n")
	})
	output, err := runInstallSh(t, release, nil, "install")
	if err == nil {
		t.Fatalf("expected a checksum mismatch to abort, output:\n%s", output)
	}
	if !strings.Contains(output, "checksum mismatch") {
		t.Fatalf("unexpected output: %s", output)
	}
	if _, statErr := os.Stat(filepath.Join(release.markerDir, "installed.txt")); !os.IsNotExist(statErr) {
		t.Fatal("the archive must not be executed when the checksum does not match")
	}
}

func TestInstallShRefusesMissingChecksumEntry(t *testing.T) {
	release := newFakeRelease(t, func(_ string, files map[string][]byte) {
		files["checksums.txt"] = []byte(strings.Repeat("0", 64) + "  some_other_asset.tar.gz\n")
	})
	output, err := runInstallSh(t, release, nil, "install")
	if err == nil {
		t.Fatalf("expected a missing checksum entry to abort, output:\n%s", output)
	}
	if !strings.Contains(output, "no checksum entry") {
		t.Fatalf("unexpected output: %s", output)
	}
}

// The asset name used to be injected into a grep regular expression, so its dots
// matched any character: a checksums.txt line for clother_darwin_arm64Xtar.gz
// was accepted for clother_darwin_arm64.tar.gz.
func TestInstallShDoesNotTreatAssetNameAsRegexp(t *testing.T) {
	release := newFakeRelease(t, func(assetName string, files map[string][]byte) {
		archive := files[assetName]
		sum := sha256.Sum256(archive)
		lookalike := strings.Replace(assetName, ".tar.gz", "Xtar.gz", 1)
		files["checksums.txt"] = []byte(fmt.Sprintf("%s  %s\n", hex.EncodeToString(sum[:]), lookalike))
	})
	output, err := runInstallSh(t, release, nil, "install")
	if err == nil {
		t.Fatalf("expected a lookalike checksum entry to be refused, output:\n%s", output)
	}
	if !strings.Contains(output, "no checksum entry") {
		t.Fatalf("unexpected output: %s", output)
	}
}

func TestInstallShFailsOnMissingAsset(t *testing.T) {
	release := newFakeRelease(t, func(assetName string, files map[string][]byte) {
		delete(files, assetName)
	})
	if output, err := runInstallSh(t, release, nil, "install"); err == nil {
		t.Fatalf("expected a 404 on the archive to abort, output:\n%s", output)
	}
}

func TestInstallShRefusesInsecureReleaseBaseURL(t *testing.T) {
	release := newFakeRelease(t, nil)
	release.url = "http://attacker.invalid/clother"
	output, err := runInstallSh(t, release, nil, "install")
	if err == nil {
		t.Fatalf("expected a cleartext CLOTHER_RELEASE_BASE_URL to be refused, output:\n%s", output)
	}
	if !strings.Contains(output, "must use https") {
		t.Fatalf("unexpected output: %s", output)
	}
}

// Without any SHA-256 tool the installer used to print a warning and execute the
// archive anyway.
func TestInstallShFailsClosedWithoutChecksumTool(t *testing.T) {
	release := newFakeRelease(t, nil)

	binDir := t.TempDir()
	for _, tool := range []string{"curl", "tar", "basename", "dirname", "mktemp", "uname", "awk", "chmod", "rm", "tr", "cat", "sed", "grep", "ls", "cp", "mv"} {
		path, err := exec.LookPath(tool)
		if err != nil {
			continue
		}
		if err := os.Symlink(path, filepath.Join(binDir, tool)); err != nil {
			t.Fatal(err)
		}
	}
	for _, forbidden := range []string{"shasum", "sha256sum", "openssl"} {
		if _, err := os.Stat(filepath.Join(binDir, forbidden)); err == nil {
			t.Fatalf("%s leaked into the restricted PATH", forbidden)
		}
	}

	output, err := runInstallSh(t, release, []string{"PATH=" + binDir}, "install")
	if err == nil {
		t.Fatalf("expected the installer to refuse to run without a SHA-256 tool, output:\n%s", output)
	}
	if !strings.Contains(output, "no SHA-256 tool available") {
		t.Fatalf("unexpected output: %s", output)
	}
	if _, statErr := os.Stat(filepath.Join(release.markerDir, "installed.txt")); !os.IsNotExist(statErr) {
		t.Fatal("the archive must not be executed when integrity cannot be checked")
	}
}

// openssl alone must be enough: the fallback exists so that the hard failure
// above only triggers when nothing at all can hash.
func TestInstallShFallsBackToOpenSSL(t *testing.T) {
	openssl, err := exec.LookPath("openssl")
	if err != nil {
		t.Skip("openssl not available")
	}
	release := newFakeRelease(t, nil)

	binDir := t.TempDir()
	for _, tool := range []string{"curl", "tar", "basename", "dirname", "mktemp", "uname", "awk", "chmod", "rm", "tr", "cat", "sed", "grep", "ls", "cp", "mv", "env", "bash"} {
		path, lookErr := exec.LookPath(tool)
		if lookErr != nil {
			continue
		}
		if err := os.Symlink(path, filepath.Join(binDir, tool)); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(openssl, filepath.Join(binDir, "openssl")); err != nil {
		t.Fatal(err)
	}

	output, err := runInstallSh(t, release, []string{"PATH=" + binDir}, "install")
	if err != nil {
		t.Fatalf("install.sh should verify with openssl alone: %v\n%s", err, output)
	}
	if _, err := os.Stat(filepath.Join(release.markerDir, "installed.txt")); err != nil {
		t.Fatalf("expected the verified binary to run: %v\n%s", err, output)
	}
}

// The EXIT trap that removes TMP_DIR never fired: the script ended on
// `exec "$TMP_DIR/clother"`, which replaces the shell, so every installation
// left a directory holding the archive and the extracted binary behind. Run the
// installer with a TMPDIR of our own and count what survives.
func TestInstallShCleansUpItsTemporaryDirectory(t *testing.T) {
	release := newFakeRelease(t, nil)
	tmpRoot := t.TempDir()

	output, err := runInstallSh(t, release, []string{"TMPDIR=" + tmpRoot}, "install")
	if err != nil {
		t.Fatalf("install.sh failed: %v\n%s", err, output)
	}

	entries, err := os.ReadDir(tmpRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		var names []string
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		t.Fatalf("install.sh left %d entries behind in TMPDIR: %v", len(entries), names)
	}
}

// The two installation channels must tell the same story about authenticity:
// the Go updater warns when a release publishes no signature, and so does this
// script now.
func TestInstallShReportsTheSignatureState(t *testing.T) {
	release := newFakeRelease(t, nil)
	output, err := runInstallSh(t, release, nil, "install")
	if err != nil {
		t.Fatalf("install.sh failed: %v\n%s", err, output)
	}
	if !strings.Contains(output, "no signature") {
		t.Fatalf("install.sh said nothing about the missing signature:\n%s", output)
	}
}
