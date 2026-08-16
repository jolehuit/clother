package session

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"strings"
)

// maxSessionLine caps how large a single JSONL record may be before the
// scanner gives up on it.
const maxSessionLine = 16 * 1024 * 1024

type Analysis struct {
	NeedsSanitization bool
	MessagesTouched   int
	BlocksRemoved     int
}

func Analyze(path string) (Analysis, error) {
	file, err := os.Open(path)
	if err != nil {
		return Analysis{}, err
	}
	defer file.Close()

	var analysis Analysis
	scanner := bufio.NewScanner(file)
	buf := make([]byte, 0, 1024*1024)
	scanner.Buffer(buf, maxSessionLine)

	for scanner.Scan() {
		line := scanner.Bytes()
		var payload map[string]any
		if err := json.Unmarshal(line, &payload); err != nil {
			continue
		}
		model, role, content := extractMessage(payload)
		if role != "assistant" || !isNonClaudeModel(model) {
			continue
		}
		removed := countReasoningBlocks(content)
		if removed == 0 {
			continue
		}
		analysis.NeedsSanitization = true
		analysis.MessagesTouched++
		analysis.BlocksRemoved += removed
	}
	if err := scanner.Err(); err != nil {
		if errors.Is(err, bufio.ErrTooLong) {
			// A single record larger than maxSessionLine cannot be inspected,
			// but that is no reason to refuse to open the session: the worst
			// case is claude seeing foreign thinking blocks, not a transcript
			// the user can never resume again.
			return Analysis{}, nil
		}
		return analysis, err
	}
	return analysis, nil
}

func extractMessage(payload map[string]any) (model string, role string, content []any) {
	message, ok := payload["message"].(map[string]any)
	if !ok {
		return "", "", nil
	}
	role, _ = message["role"].(string)
	model, _ = message["model"].(string)
	if model == "" {
		model, _ = payload["model"].(string)
	}
	content, _ = message["content"].([]any)
	return model, role, content
}

func countReasoningBlocks(content []any) int {
	count := 0
	for _, part := range content {
		block, ok := part.(map[string]any)
		if !ok {
			continue
		}
		blockType, _ := block["type"].(string)
		if isReasoningType(blockType) {
			count++
		}
	}
	return count
}

func isReasoningType(blockType string) bool {
	switch strings.ToLower(blockType) {
	case "thinking", "reasoning":
		return true
	default:
		return false
	}
}

func isNonClaudeModel(model string) bool {
	model = strings.TrimSpace(strings.ToLower(model))
	return model != "" && !strings.Contains(model, "claude")
}
