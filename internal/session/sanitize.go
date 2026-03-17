package session

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jolehuit/clother/internal/config"
)

type Patch struct {
	OriginalPath  string `json:"original_path"`
	BackupPath    string `json:"backup_path"`
	SanitizedPath string `json:"sanitized_path"`
	MetadataPath  string `json:"metadata_path"`
	Messages      int    `json:"messages"`
	Blocks        int    `json:"blocks"`
	AppliedUnix   int64  `json:"applied_unix"`
}

func PrepareTemporaryPatch(paths config.Paths, sessionPath string) (*Patch, Analysis, error) {
	analysis, err := Analyze(sessionPath)
	if err != nil || !analysis.NeedsSanitization {
		return nil, analysis, err
	}
	if err := os.MkdirAll(paths.SessionPatchDir, 0o755); err != nil {
		return nil, analysis, err
	}
	base := strings.TrimSuffix(filepath.Base(sessionPath), filepath.Ext(sessionPath))
	return &Patch{
		OriginalPath:  sessionPath,
		BackupPath:    filepath.Join(paths.SessionPatchDir, base+".orig"),
		SanitizedPath: filepath.Join(paths.SessionPatchDir, base+".san"),
		MetadataPath:  filepath.Join(paths.SessionPatchDir, base+".json"),
		Messages:      analysis.MessagesTouched,
		Blocks:        analysis.BlocksRemoved,
		AppliedUnix:   time.Now().Unix(),
	}, analysis, nil
}

func (p *Patch) Apply() error {
	original, err := os.ReadFile(p.OriginalPath)
	if err != nil {
		return err
	}
	if err := configWriteFile(p.BackupPath, original, 0o600); err != nil {
		return err
	}

	sanitized, err := sanitizeJSONL(original)
	if err != nil {
		return err
	}
	if err := configWriteFile(p.SanitizedPath, sanitized, 0o600); err != nil {
		return err
	}
	meta, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	meta = append(meta, '\n')
	if err := configWriteFile(p.MetadataPath, meta, 0o600); err != nil {
		return err
	}
	return configWriteFile(p.OriginalPath, sanitized, 0o600)
}

func (p *Patch) Restore() error {
	backup, err := os.ReadFile(p.BackupPath)
	if err != nil {
		return err
	}
	current, err := os.ReadFile(p.OriginalPath)
	if err != nil {
		return err
	}
	sanitized, err := os.ReadFile(p.SanitizedPath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	restored, err := mergedRestoreContent(backup, sanitized, current)
	if err != nil {
		return err
	}
	if err := configWriteFile(p.OriginalPath, restored, 0o600); err != nil {
		return err
	}
	_ = os.Remove(p.BackupPath)
	_ = os.Remove(p.SanitizedPath)
	_ = os.Remove(p.MetadataPath)
	return nil
}

func RestoreStale(paths config.Paths) error {
	entries, err := os.ReadDir(paths.SessionPatchDir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		metaPath := filepath.Join(paths.SessionPatchDir, entry.Name())
		data, err := os.ReadFile(metaPath)
		if err != nil {
			continue
		}
		var patch Patch
		if err := json.Unmarshal(data, &patch); err != nil {
			continue
		}
		if _, err := os.Stat(patch.BackupPath); err != nil {
			_ = os.Remove(metaPath)
			continue
		}
		if patch.SanitizedPath == "" {
			base := strings.TrimSuffix(entry.Name(), ".json")
			patch.SanitizedPath = filepath.Join(paths.SessionPatchDir, base+".san")
		}
		_ = patch.Restore()
	}
	return nil
}

func mergedRestoreContent(backup, sanitized, current []byte) ([]byte, error) {
	if bytes.Equal(current, backup) {
		return backup, nil
	}
	if len(sanitized) == 0 {
		return nil, fmt.Errorf("missing sanitized snapshot; refusing restore to avoid data loss")
	}
	if !bytes.HasPrefix(current, sanitized) {
		return nil, fmt.Errorf("session changed unexpectedly; refusing restore to avoid data loss")
	}
	restored := append([]byte{}, backup...)
	restored = append(restored, current[len(sanitized):]...)
	return restored, nil
}

func sanitizeJSONL(input []byte) ([]byte, error) {
	var out strings.Builder
	scanner := bufio.NewScanner(strings.NewReader(string(input)))
	buf := make([]byte, 0, 1024*1024)
	scanner.Buffer(buf, 16*1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		var payload map[string]any
		if err := json.Unmarshal(line, &payload); err != nil {
			out.Write(line)
			out.WriteByte('\n')
			continue
		}
		model, role, content := extractMessage(payload)
		if role == "assistant" && isNonClaudeModel(model) && len(content) > 0 {
			cleaned, changed := sanitizeContent(content)
			if changed {
				if len(cleaned) == 0 {
					continue
				}
				message := payload["message"].(map[string]any)
				message["content"] = cleaned
				encoded, err := json.Marshal(payload)
				if err != nil {
					return nil, err
				}
				out.Write(encoded)
				out.WriteByte('\n')
				continue
			}
		}
		out.Write(line)
		out.WriteByte('\n')
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return []byte(out.String()), nil
}

func sanitizeContent(content []any) ([]any, bool) {
	var out []any
	changed := false
	for _, part := range content {
		block, ok := part.(map[string]any)
		if !ok {
			out = append(out, part)
			continue
		}
		blockType, _ := block["type"].(string)
		if isReasoningType(blockType) {
			changed = true
			continue
		}
		out = append(out, part)
	}
	return out, changed
}

func configWriteFile(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".patch-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("rename %s: %w", path, err)
	}
	return nil
}
