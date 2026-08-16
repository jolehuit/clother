package runtime

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// Several sessions ending at the same moment each merge their overlay's
// settings.json back into the user's own. The rename at the end is atomic, the
// read-merge-write around it was not, so the last process to finish erased what
// the others had just brought back: 6 to 8 keys of 8 survived across five
// rounds, and those keys are the theme, permissions and hooks claude wrote
// during the session.
//
// Real processes, not goroutines: an in-process lock would pass this test while
// doing nothing for two `clother-zai` in two terminals.

const (
	mergeHelperEnv     = "CLOTHER_TEST_MERGE_HELPER"
	mergeHelperKeyEnv  = "CLOTHER_TEST_MERGE_KEY"
	mergeHelperSrcEnv  = "CLOTHER_TEST_MERGE_SOURCE_DIR"
	mergeHelperGateEnv = "CLOTHER_TEST_MERGE_GATE"
)

// TestMergeBackHelperProcess is one session ending: it writes an overlay
// settings.json holding its own key, waits for the shared barrier, then merges
// back.
func TestMergeBackHelperProcess(t *testing.T) {
	if os.Getenv(mergeHelperEnv) == "" {
		t.Skip("helper process")
	}
	key := os.Getenv(mergeHelperKeyEnv)
	sourceDir := os.Getenv(mergeHelperSrcEnv)

	overlayDir := t.TempDir()
	overlayPath := filepath.Join(overlayDir, "settings.json")
	body, err := json.Marshal(map[string]any{
		"model":  "written-by-clother",
		key:      "written-by-claude-during-the-session",
		"shared": "value",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(overlayPath, body, 0o600); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(30 * time.Second)
	gate := os.Getenv(mergeHelperGateEnv)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(gate); err == nil {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	mergeBackSettings(sourceDir, overlayPath)
}

func TestConcurrentSessionsKeepEverySettingsKey(t *testing.T) {
	sourceDir := t.TempDir()
	sourcePath := filepath.Join(sourceDir, "settings.json")
	if err := os.WriteFile(sourcePath, []byte(`{"existingKey":"kept"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	gate := filepath.Join(t.TempDir(), "gate")
	var keys []string
	for i := 0; i < 8; i++ {
		keys = append(keys, fmt.Sprintf("key_%c", 'A'+i))
	}

	var procs []*exec.Cmd
	var outputs []*bytes.Buffer
	for _, key := range keys {
		out := &bytes.Buffer{}
		cmd := exec.Command(os.Args[0], "-test.run=TestMergeBackHelperProcess", "-test.v")
		cmd.Env = append(os.Environ(),
			mergeHelperEnv+"=1",
			mergeHelperKeyEnv+"="+key,
			mergeHelperSrcEnv+"="+sourceDir,
			mergeHelperGateEnv+"="+gate,
		)
		cmd.Stdout = out
		cmd.Stderr = out
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		procs = append(procs, cmd)
		outputs = append(outputs, out)
	}

	time.Sleep(400 * time.Millisecond)
	if err := os.WriteFile(gate, []byte("go"), 0o600); err != nil {
		t.Fatal(err)
	}
	for i, cmd := range procs {
		if err := cmd.Wait(); err != nil {
			t.Fatalf("helper %s failed: %v\n%s", keys[i], err, outputs[i].String())
		}
	}

	data, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	var merged map[string]any
	if err := json.Unmarshal(data, &merged); err != nil {
		t.Fatalf("settings.json is no longer valid JSON: %v (%s)", err, data)
	}
	if merged["existingKey"] != "kept" {
		t.Errorf("the pre-existing key was lost: %s", data)
	}
	// `model` is written by clother itself and must never be merged back.
	if _, ok := merged["model"]; ok {
		t.Errorf("clother's own session model leaked into the user's settings: %s", data)
	}
	missing := 0
	for _, key := range keys {
		if merged[key] != "written-by-claude-during-the-session" {
			missing++
			t.Errorf("%s was lost", key)
		}
	}
	if missing > 0 {
		t.Fatalf("%d of %d keys merged back: %s", len(keys)-missing, len(keys), data)
	}
}
