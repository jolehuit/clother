package config

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/jolehuit/clother/internal/providers"
)

var envKeyPattern = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*$`)

type Secrets map[string]string

func LoadSecrets(path string) (Secrets, error) {
	secrets := Secrets{}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return secrets, nil
	}
	if err != nil {
		return nil, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("secrets file is a symlink: %s", path)
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
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
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

func MaskSecret(value string) string {
	if value == "" {
		return ""
	}
	if len(value) <= 8 {
		return "****"
	}
	return value[:4] + "****" + value[len(value)-4:]
}

func decodeShellValue(value string) (string, error) {
	if len(value) >= 2 && value[0] == '\'' && value[len(value)-1] == '\'' {
		return value[1 : len(value)-1], nil
	}
	if strings.HasPrefix(value, "$'") && strings.HasSuffix(value, "'") {
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
	if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
		value = value[1 : len(value)-1]
	}
	return strings.ReplaceAll(value, `\"`, `"`), nil
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	if !strings.ContainsAny(value, "\n\t'\\ ") {
		return value
	}
	replacer := strings.NewReplacer(`\`, `\\`, "\n", `\n`, "\t", `\t`, `'`, `\'`)
	return "$'" + replacer.Replace(value) + "'"
}
