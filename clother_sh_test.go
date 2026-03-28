package clother_test

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
