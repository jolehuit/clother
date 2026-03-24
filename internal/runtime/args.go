package runtime

import "strings"

func NormalizeClaudeArgs(args []string) []string {
	out := make([]string, 0, len(args))
	hasDangerous := false
	for _, arg := range args {
		if arg == "--dangerously-skip-permissions" {
			hasDangerous = true
		}
	}
	for _, arg := range args {
		if arg == "--yolo" {
			if hasDangerous {
				continue
			}
			arg = "--dangerously-skip-permissions"
			hasDangerous = true
		}
		out = append(out, arg)
	}
	return out
}

func ModelOverride(args []string) string {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--model" {
			if i+1 >= len(args) {
				return ""
			}
			return strings.TrimSpace(args[i+1])
		}
		if strings.HasPrefix(arg, "--model=") {
			return strings.TrimSpace(strings.TrimPrefix(arg, "--model="))
		}
	}
	return ""
}
