package config

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"

	"github.com/jolehuit/clother/internal/providers"
)

var envKeyPattern = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*$`)

// shellSafeValue matches the values that can be written to secrets.env without
// any quoting. Anything else is encoded in full with the $'...' form so that
// Save -> Load is strictly symmetric: a partial rule (the previous code only
// quoted values containing "\n\t'\\ ") silently truncated values wrapped in
// double quotes.
var shellSafeValue = regexp.MustCompile(`^[A-Za-z0-9._:/+@-]+$`)

// ValidEnvKey reports whether key can be used as an environment variable name
// in secrets.env. Callers that derive a key from a provider name must check it
// before writing anything, otherwise SaveSecrets fails after config.json has
// already been persisted.
func ValidEnvKey(key string) bool {
	return envKeyPattern.MatchString(key)
}

type Secrets map[string]string

func LoadSecrets(path string) (Secrets, error) {
	secrets := Secrets{}
	// O_NOFOLLOW makes the symlink check happen *before* the read: the previous
	// order (ReadFile then Lstat) had already followed the link by the time it
	// refused it.
	file, err := os.OpenFile(path, os.O_RDONLY|openNoFollow, 0)
	if errors.Is(err, os.ErrNotExist) {
		return secrets, nil
	}
	if err != nil {
		if errors.Is(err, syscall.ELOOP) {
			return nil, fmt.Errorf("secrets file is a symlink: %s", path)
		}
		return nil, err
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("secrets file is not a regular file: %s", path)
	}

	data, err := io.ReadAll(file)
	if err != nil {
		return nil, err
	}

	scanner := bufio.NewScanner(bytes.NewReader(data))
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok || !envKeyPattern.MatchString(key) {
			return nil, fmt.Errorf("invalid secrets file line %d", lineNo)
		}
		unquoted, err := decodeShellValue(value)
		if err != nil {
			return nil, fmt.Errorf("decode %s: %w", key, err)
		}
		secrets[key] = unquoted
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return secrets, nil
}

func SaveSecrets(path string, secrets Secrets) error {
	if err := ensurePrivateDir(filepath.Dir(path)); err != nil {
		return err
	}
	return saveSecretsFile(path, secrets)
}

// saveSecretsFile is the write half of SaveSecrets, without the directory
// creation, so SaveSecretsMerged can call it from inside the file lock.
func saveSecretsFile(path string, secrets Secrets) error {
	var keys []string
	for key := range secrets {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var buf bytes.Buffer
	for _, key := range keys {
		if !envKeyPattern.MatchString(key) {
			return fmt.Errorf("invalid secret key %q", key)
		}
		fmt.Fprintf(&buf, "%s=%s\n", key, shellQuote(secrets[key]))
	}
	if err := writeAtomic(path, buf.Bytes(), 0o600); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}

func NormalizeLegacySecrets(secrets Secrets, catalog providers.Catalog) {
	builtinSecretKeys := catalog.BuiltinSecretKeys()
	for key, value := range secrets {
		switch {
		case strings.HasPrefix(key, "OPENROUTER_MODEL_"):
			if looksLikeLauncherName(value) || normalizeOpenRouterAliasName(strings.ToLower(strings.ReplaceAll(strings.TrimPrefix(key, "OPENROUTER_MODEL_"), "_", "-"))) == "" {
				delete(secrets, key)
			}
		case strings.HasPrefix(key, "CLOTHER_") && strings.HasSuffix(key, "_BASE_URL"):
			secretKey := strings.TrimSuffix(strings.TrimPrefix(key, "CLOTHER_"), "_BASE_URL")
			if _, ok := builtinSecretKeys[secretKey]; ok {
				delete(secrets, key)
			}
		}
	}
}

// MaskSecret renders a credential for display. Only the last four characters
// of a sufficiently long value are shown: the previous version revealed eight
// characters, which left a single character hidden on a short key.
func MaskSecret(value string) string {
	if value == "" {
		return ""
	}
	if len(value) < 16 {
		return "****"
	}
	return "****" + value[len(value)-4:]
}

func decodeShellValue(value string) (string, error) {
	if len(value) >= 2 && value[0] == '\'' && value[len(value)-1] == '\'' {
		return value[1 : len(value)-1], nil
	}
	if strings.HasPrefix(value, "$'") && strings.HasSuffix(value, "'") && len(value) >= 3 {
		var out strings.Builder
		escaped := value[2 : len(value)-1]
		for i := 0; i < len(escaped); i++ {
			if escaped[i] != '\\' {
				out.WriteByte(escaped[i])
				continue
			}
			if i+1 >= len(escaped) {
				return "", fmt.Errorf("unterminated escape")
			}
			i++
			switch escaped[i] {
			case 'n':
				out.WriteByte('\n')
			case 'r':
				out.WriteByte('\r')
			case 't':
				out.WriteByte('\t')
			case '\\':
				out.WriteByte('\\')
			case '\'':
				out.WriteByte('\'')
			default:
				out.WriteByte(escaped[i])
			}
		}
		return out.String(), nil
	}
	// Compatibility path: secrets.env files written by earlier versions, or
	// edited by hand as KEY="value", are still accepted. shellQuote never
	// produces this form any more.
	if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
		value = value[1 : len(value)-1]
	}
	return strings.ReplaceAll(value, `\"`, `"`), nil
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	if shellSafeValue.MatchString(value) {
		return value
	}
	replacer := strings.NewReplacer(`\`, `\\`, "\n", `\n`, "\r", `\r`, "\t", `\t`, `'`, `\'`)
	return "$'" + replacer.Replace(value) + "'"
}
