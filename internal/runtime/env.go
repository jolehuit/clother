package runtime

import (
	"fmt"
	"os"
	"strings"

	"github.com/jolehuit/clother/internal/config"
	"github.com/jolehuit/clother/internal/profiles"
	"github.com/jolehuit/clother/internal/providers"
)

func BuildEnv(target profiles.Target, secrets config.Secrets) ([]string, error) {
	envMap := map[string]string{}
	for _, pair := range os.Environ() {
		key, value, ok := splitEnv(pair)
		if ok {
			envMap[key] = value
		}
	}
	clearAnthropicEnv(envMap)

	if target.BaseURL != "" {
		envMap["ANTHROPIC_BASE_URL"] = target.BaseURL
	}
	if target.Model != "" {
		envMap["ANTHROPIC_MODEL"] = target.Model
	}
	for key, value := range target.ModelTiers {
		switch key {
		case "haiku":
			envMap["ANTHROPIC_DEFAULT_HAIKU_MODEL"] = value
		case "sonnet":
			envMap["ANTHROPIC_DEFAULT_SONNET_MODEL"] = value
		case "opus":
			envMap["ANTHROPIC_DEFAULT_OPUS_MODEL"] = value
		case "small":
			envMap["ANTHROPIC_SMALL_FAST_MODEL"] = value
		}
	}

	switch target.AuthMode {
	case providers.AuthNone:
	case providers.AuthLiteral:
		envMap["ANTHROPIC_AUTH_TOKEN"] = target.LiteralAuthToken
		envMap["ANTHROPIC_API_KEY"] = ""
	case providers.AuthSecret:
		value := secrets[target.SecretKey]
		if value == "" {
			return nil, fmt.Errorf("%s not configured", target.SecretKey)
		}
		envMap["ANTHROPIC_AUTH_TOKEN"] = value
		if target.Family == providers.FamilyOpenRouter || target.Family == providers.FamilyLocal {
			envMap["ANTHROPIC_API_KEY"] = ""
		}
	default:
		return nil, fmt.Errorf("unsupported auth mode %q", target.AuthMode)
	}

	return flattenEnv(envMap), nil
}

func clearAnthropicEnv(envMap map[string]string) {
	for key := range envMap {
		if strings.HasPrefix(key, "ANTHROPIC_") {
			delete(envMap, key)
		}
	}
}

func splitEnv(pair string) (string, string, bool) {
	for i := 0; i < len(pair); i++ {
		if pair[i] == '=' {
			return pair[:i], pair[i+1:], true
		}
	}
	return "", "", false
}

func flattenEnv(envMap map[string]string) []string {
	env := make([]string, 0, len(envMap))
	for key, value := range envMap {
		env = append(env, key+"="+value)
	}
	return env
}
