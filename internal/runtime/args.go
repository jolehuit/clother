package runtime

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
