package session

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"
)

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
	scanner.Buffer(buf, 16*1024*1024)

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
	return analysis, scanner.Err()
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
