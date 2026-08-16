package runtime

import "strings"

// argRole classifies every token of a claude command line so that Clother only
// ever rewrites or inspects tokens that really sit in a flag position.
//
// Without that classification a token that is DATA (the value of an option, or
// anything after the `--` end-of-options separator) is mistaken for a flag:
// `claude -- --yolo` used to be rewritten into
// `claude -- --dangerously-skip-permissions`, and `--model` / `--resume`
// appearing after `--` used to drive the model overlay and the session patcher.
type argRole int

const (
	roleFlag argRole = iota
	roleValue
	rolePositional
	roleSeparator
	rolePastSeparator
)

// flagsTakingValue lists the claude options whose value is the NEXT token.
// Anything in that position is data and must never be treated as a flag.
var flagsTakingValue = map[string]struct{}{
	"--model":                     {},
	"--fallback-model":            {},
	"--agents":                    {},
	"--add-dir":                   {},
	"--allowedTools":              {},
	"--allowed-tools":             {},
	"--disallowedTools":           {},
	"--disallowed-tools":          {},
	"--permission-mode":           {},
	"--permission-prompt-tool":    {},
	"--system-prompt":             {},
	"--system-prompt-file":        {},
	"--append-system-prompt":      {},
	"--append-system-prompt-file": {},
	"--mcp-config":                {},
	"--settings":                  {},
	"--setting-sources":           {},
	"--session-id":                {},
	"--max-turns":                 {},
	"--output-format":             {},
	"--input-format":              {},
	"--name":                      {},
	"-n":                          {},
}

// flagsTakingOptionalValue lists options whose value may be omitted
// (`claude --resume` opens the picker). The next token counts as their value
// only when it does not itself look like a flag.
var flagsTakingOptionalValue = map[string]struct{}{
	"--resume": {},
	"-r":       {},
}

// classifyArgs returns one role per input token. It stops interpreting flags
// for good at the first bare `--`.
func classifyArgs(args []string) []argRole {
	roles := make([]argRole, len(args))
	past := false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if past {
			roles[i] = rolePastSeparator
			continue
		}
		if arg == "--" {
			roles[i] = roleSeparator
			past = true
			continue
		}
		if len(arg) < 2 || !strings.HasPrefix(arg, "-") {
			roles[i] = rolePositional
			continue
		}
		roles[i] = roleFlag
		if strings.Contains(arg, "=") {
			// --opt=value carries its own value; nothing to consume.
			continue
		}
		name := arg
		if _, ok := flagsTakingValue[name]; ok {
			if i+1 < len(args) {
				i++
				roles[i] = roleValue
			}
			continue
		}
		if _, ok := flagsTakingOptionalValue[name]; ok {
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				i++
				roles[i] = roleValue
			}
			continue
		}
	}
	return roles
}

func NormalizeClaudeArgs(args []string) []string {
	roles := classifyArgs(args)
	out := make([]string, 0, len(args))
	hasDangerous := false
	for i, arg := range args {
		if roles[i] == roleFlag && arg == "--dangerously-skip-permissions" {
			hasDangerous = true
		}
	}
	for i, arg := range args {
		if roles[i] == roleFlag && arg == "--yolo" {
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

// IsInfoOnlyInvocation reports whether the command line only asks claude to
// print something about itself (`--help`, `--version`) and therefore makes no
// API call. Such an invocation must stay reachable before any API key exists:
// `clother-zai --help` failing with "ZAI_API_KEY not configured" hides the very
// help that explains how to configure it.
func IsInfoOnlyInvocation(args []string) bool {
	roles := classifyArgs(args)
	found := false
	for i, arg := range args {
		switch roles[i] {
		case roleFlag:
			switch arg {
			case "--help", "-h", "--version", "-v":
				found = true
			default:
				// Any other flag means claude is being asked to do real work.
				return false
			}
		case roleValue, rolePositional, roleSeparator, rolePastSeparator:
			return false
		}
	}
	return found
}

func ModelOverride(args []string) string {
	roles := classifyArgs(args)
	for i, arg := range args {
		if roles[i] != roleFlag {
			continue
		}
		if arg == "--model" {
			if i+1 < len(args) && roles[i+1] == roleValue {
				return strings.TrimSpace(args[i+1])
			}
			return ""
		}
		if strings.HasPrefix(arg, "--model=") {
			return strings.TrimSpace(strings.TrimPrefix(arg, "--model="))
		}
	}
	return ""
}

// ResumeOverride extracts the session id of `--resume`/`-r` using the same
// positional discipline as ModelOverride. session.ResumeID does a raw token
// scan, which also matched data positions and everything after `--`; the id it
// returned then drove FindSession and a rewrite of the transcript on disk.
func ResumeOverride(args []string) string {
	roles := classifyArgs(args)
	for i, arg := range args {
		if roles[i] != roleFlag {
			continue
		}
		if arg == "--resume" || arg == "-r" {
			if i+1 < len(args) && roles[i+1] == roleValue {
				return strings.TrimSpace(args[i+1])
			}
			return ""
		}
		if strings.HasPrefix(arg, "--resume=") {
			return strings.TrimSpace(strings.TrimPrefix(arg, "--resume="))
		}
	}
	return ""
}
