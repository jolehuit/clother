package config

import (
	"fmt"
	"path/filepath"
)

// Saving state is a read-modify-write cycle: the file is read once at startup,
// changed in memory by a command, and written back whole. Two concurrent
// commands therefore both start from the version from before and the second
// rename erases the first one's work.
//
// SaveSecretsMerged and SaveConfigMerged close that window: they take the file
// lock, re-read the file from disk inside it, and replay only what actually
// changed in memory (baseline -> current) onto that fresh copy. A key another
// process added meanwhile survives; a key this command deleted is still deleted.

// Clone returns a deep copy of the secrets, for use as a baseline.
func (s Secrets) Clone() Secrets {
	out := make(Secrets, len(s))
	for key, value := range s {
		out[key] = value
	}
	return out
}

// Clone returns a deep copy of the config file, for use as a baseline.
func (cfg *File) Clone() *File {
	if cfg == nil {
		return nil
	}
	out := &File{
		Version:           cfg.Version,
		ProviderOverrides: make(map[string]ProviderOverride, len(cfg.ProviderOverrides)),
		OpenRouterAliases: make(map[string]string, len(cfg.OpenRouterAliases)),
		CustomProviders:   make(map[string]CustomProvider, len(cfg.CustomProviders)),
	}
	for key, value := range cfg.ProviderOverrides {
		out.ProviderOverrides[key] = value
	}
	for key, value := range cfg.OpenRouterAliases {
		out.OpenRouterAliases[key] = value
	}
	for key, value := range cfg.CustomProviders {
		out.CustomProviders[key] = value
	}
	return out
}

// SaveSecretsMerged persists current on top of whatever secrets.env holds right
// now, under an exclusive lock. baseline is the content this process read at
// startup; it may be nil, in which case current is written as a pure addition.
func SaveSecretsMerged(path string, baseline, current Secrets) error {
	if err := ensurePrivateDir(filepath.Dir(path)); err != nil {
		return err
	}
	return WithFileLock(path, func() error {
		disk, err := LoadSecrets(path)
		if err != nil {
			// Never write over a file we could not read: that is exactly how a
			// permission problem or a hand-edit turns into lost credentials.
			return fmt.Errorf("cannot read %s before saving it: %w", path, err)
		}
		return saveSecretsFile(path, mergeSecrets(disk, baseline, current))
	})
}

// SaveConfigMerged is SaveSecretsMerged for config.json.
func SaveConfigMerged(path string, baseline, current *File) error {
	if err := ensurePrivateDir(filepath.Dir(path)); err != nil {
		return err
	}
	return WithFileLock(path, func() error {
		disk, err := LoadConfig(path)
		if err != nil {
			return fmt.Errorf("cannot read %s before saving it: %w", path, err)
		}
		return saveConfigFile(path, mergeConfig(disk, baseline, current))
	})
}

func mergeSecrets(disk, baseline, current Secrets) Secrets {
	out := disk.Clone()
	mergeInto(out, baseline, current)
	return out
}

func mergeConfig(disk, baseline, current *File) *File {
	out := disk.Clone()
	if out == nil {
		out = &File{
			ProviderOverrides: map[string]ProviderOverride{},
			OpenRouterAliases: map[string]string{},
			CustomProviders:   map[string]CustomProvider{},
		}
	}
	out.Version = current.Version
	var base *File
	if baseline != nil {
		base = baseline
	} else {
		base = &File{}
	}
	mergeInto(out.ProviderOverrides, base.ProviderOverrides, current.ProviderOverrides)
	mergeInto(out.OpenRouterAliases, base.OpenRouterAliases, current.OpenRouterAliases)
	mergeInto(out.CustomProviders, base.CustomProviders, current.CustomProviders)
	return out
}

// mergeInto replays the baseline -> current diff onto out: an entry that changed
// in memory is written, an entry that disappeared from memory is deleted, and an
// entry nobody touched is left exactly as the file on disk has it.
func mergeInto[T comparable](out, baseline, current map[string]T) {
	for key, value := range current {
		if previous, ok := baseline[key]; !ok || previous != value {
			out[key] = value
		}
	}
	for key := range baseline {
		if _, ok := current[key]; !ok {
			delete(out, key)
		}
	}
}
