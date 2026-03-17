package config

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/jolehuit/clother/internal/providers"
)

var exportPattern = regexp.MustCompile(`^export ([A-Z0-9_]+)="?(.*?)"?$`)

func MigrateLegacyLaunchers(binDir string, catalog providers.Catalog, cfg *File) error {
	entries, err := os.ReadDir(binDir)
	if err != nil && !errorsIsNotExist(err) {
		return err
	}
	builtin := map[string]struct{}{}
	for _, id := range catalog.IDs() {
		builtin[id] = struct{}{}
	}

	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, "clother-") || name == "clother" {
			continue
		}
		path := filepath.Join(binDir, name)
		lines, err := readLines(path)
		if err != nil {
			continue
		}
		profile := strings.TrimPrefix(name, "clother-")
		envs := map[string]string{}
		for _, line := range lines {
			match := exportPattern.FindStringSubmatch(strings.TrimSpace(line))
			if len(match) == 3 {
				envs[match[1]] = match[2]
			}
		}
		if _, ok := builtin[profile]; ok {
			if model := envs["ANTHROPIC_MODEL"]; model != "" {
				if provider, found := catalog.Get(profile); found && model != provider.DefaultModel {
					cfg.ProviderOverrides[profile] = ProviderOverride{Model: model}
				}
			}
			continue
		}
		if strings.HasPrefix(profile, "or-") {
			if model := firstNonEmpty(envs["ANTHROPIC_DEFAULT_OPUS_MODEL"], envs["ANTHROPIC_SMALL_FAST_MODEL"]); model != "" {
				cfg.OpenRouterAliases[strings.TrimPrefix(profile, "or-")] = model
			}
			continue
		}
		if baseURL := envs["ANTHROPIC_BASE_URL"]; baseURL != "" {
			cfg.CustomProviders[profile] = CustomProvider{
				Name:         profile,
				DisplayName:  profile,
				BaseURL:      baseURL,
				APIKeyEnv:    strings.ToUpper(strings.ReplaceAll(profile, "-", "_")) + "_API_KEY",
				DefaultModel: envs["ANTHROPIC_MODEL"],
			}
		}
	}
	return nil
}

func readLines(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var lines []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	return lines, scanner.Err()
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func errorsIsNotExist(err error) bool {
	return err != nil && os.IsNotExist(err)
}
