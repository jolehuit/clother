package config

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestSecretsRoundTripPreservesValues locks the Save -> Load contract for every
// shape a pasted API key can take. Before the fix, shellQuote only quoted
// values containing one of "\n\t'\\ ", so a value wrapped in double quotes was
// written raw and decodeShellValue stripped the quotes on the way back.
func TestSecretsRoundTripPreservesValues(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		value string
	}{
		{"empty", ""},
		{"plain", "sk-abcdef0123456789"},
		{"inner spaces", "sk abc def"},
		{"trailing space", "sk-abc "},
		{"leading space", " sk-abc"},
		{"newline", "sk-abc\ndef"},
		{"carriage return", "sk-abc\rdef"},
		{"tab", "sk-abc\tdef"},
		{"single quote", "sk-ab'cd"},
		{"double quotes around", `"sk-abc"`},
		{"double quote inside", `he said "hi"`},
		{"single quotes around", "'sk-abc'"},
		{"trailing backslash", `sk-abc\`},
		{"backslash quote", `sk\"abc`},
		{"hash", "sk-abc#def"},
		{"equals", "sk=abc=def"},
		{"dollar quote prefix", `$'sk-abc'`},
		{"leading hash", "#sk-abc"},
		{"base64ish", "c2stYWJjZGVm+/=="},
		{"unicode", "clé-secrète-日本語"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "secrets.env")
			if err := SaveSecrets(path, Secrets{"ZAI_API_KEY": tc.value}); err != nil {
				t.Fatalf("SaveSecrets(%q): %v", tc.value, err)
			}
			loaded, err := LoadSecrets(path)
			if err != nil {
				t.Fatalf("LoadSecrets after writing %q: %v", tc.value, err)
			}
			if got := loaded["ZAI_API_KEY"]; got != tc.value {
				t.Fatalf("round-trip corrupted the value: wrote %q, read back %q", tc.value, got)
			}
			if len(loaded) != 1 {
				t.Fatalf("expected a single key, got %+v", loaded)
			}
		})
	}
}

func TestSecretsRoundTripKeepsEveryKeyInOneFile(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "secrets.env")
	original := Secrets{
		"ZAI_API_KEY":        `"quoted"`,
		"KIMI_API_KEY":       "plain-value",
		"OPENROUTER_API_KEY": "with space and 'quote'",
	}
	if err := SaveSecrets(path, original); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadSecrets(path)
	if err != nil {
		t.Fatal(err)
	}
	for key, want := range original {
		if got := loaded[key]; got != want {
			t.Fatalf("%s = %q, want %q", key, got, want)
		}
	}
}

// TestLoadSecretsStillReadsHandEditedQuotedValues protects the compatibility
// path: files written by earlier versions (or edited by hand as KEY="value")
// must keep loading.
func TestLoadSecretsStillReadsHandEditedQuotedValues(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "secrets.env")
	if err := os.WriteFile(path, []byte("ZAI_API_KEY=\"sk-legacy\"\nKIMI_API_KEY=sk-bare\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadSecrets(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded["ZAI_API_KEY"] != "sk-legacy" || loaded["KIMI_API_KEY"] != "sk-bare" {
		t.Fatalf("unexpected legacy decode: %+v", loaded)
	}
}

func TestSaveSecretsCreatesPrivateDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data", "clother")
	path := filepath.Join(dir, "secrets.env")
	if err := SaveSecrets(path, Secrets{"ZAI_API_KEY": "sk-abc"}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		t.Fatalf("data dir mode = %v, want no group/other bits", perm)
	}
	fileInfo, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := fileInfo.Mode().Perm(); perm != 0o600 {
		t.Fatalf("secrets file mode = %v, want 0600", perm)
	}
}

func TestSaveSecretsTightensExistingWorldReadableDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "clother")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := SaveSecrets(filepath.Join(dir, "secrets.env"), Secrets{"ZAI_API_KEY": "sk-abc"}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		t.Fatalf("existing dir mode = %v, want tightened to 0700", perm)
	}
}

// TestLoadSecretsRefusesSymlinkBeforeReading checks the O_NOFOLLOW ordering:
// the symlink must be refused without the target ever being read.
func TestLoadSecretsRefusesSymlinkBeforeReading(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	victim := filepath.Join(dir, "victim.txt")
	if err := os.WriteFile(victim, []byte("OTHER_API_KEY=leaked\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "secrets.env")
	if err := os.Symlink(victim, link); err != nil {
		t.Fatal(err)
	}
	secrets, err := LoadSecrets(link)
	if err == nil {
		t.Fatalf("expected symlinked secrets file to be refused, got %+v", secrets)
	}
	if secrets != nil {
		t.Fatalf("expected no secrets to be returned, got %+v", secrets)
	}
	if !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("error = %v, want it to mention the symlink", err)
	}
}

// TestLoadSecretsDoesNotReadThroughSymlink proves the check happens before the
// read, not after: the symlink points at a FIFO with no writer, so any code
// that opens the target first blocks forever. With O_NOFOLLOW the open fails
// immediately and the target is never touched.
func TestLoadSecretsDoesNotReadThroughSymlink(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	fifo := filepath.Join(dir, "fifo")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Skipf("cannot create fifo: %v", err)
	}
	link := filepath.Join(dir, "secrets.env")
	if err := os.Symlink(fifo, link); err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := LoadSecrets(link)
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected symlinked secrets file to be refused")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("LoadSecrets followed the symlink and blocked on the target: the symlink check runs after the read")
	}
}

func TestMaskSecretRevealsAtMostFourCharacters(t *testing.T) {
	t.Parallel()

	cases := []string{"", "1", "12345678", "123456789", "1234567890", "123456789012345", "1234567890123456", strings.Repeat("a", 40) + "tail"}
	for _, value := range cases {
		masked := MaskSecret(value)
		if value == "" {
			if masked != "" {
				t.Fatalf("MaskSecret(\"\") = %q, want empty", masked)
			}
			continue
		}
		revealed := strings.ReplaceAll(masked, "*", "")
		if len(revealed) > 4 {
			t.Fatalf("MaskSecret(%q) = %q reveals %d characters, want at most 4", value, masked, len(revealed))
		}
		if len(value) >= 16 && !strings.HasSuffix(masked, value[len(value)-4:]) {
			t.Fatalf("MaskSecret(%q) = %q, want the last four characters kept", value, masked)
		}
		if len(value) < 16 && masked != "****" {
			t.Fatalf("MaskSecret(%q) = %q, want a fully masked short value", value, masked)
		}
	}
}

func TestWriteAtomicLeavesNoTemporaryFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := writeAtomic(path, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "config.json" {
		t.Fatalf("unexpected directory contents after writeAtomic: %+v", entries)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "{}\n" {
		t.Fatalf("content = %q", data)
	}
}
