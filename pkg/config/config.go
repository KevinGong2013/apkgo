package config

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/KevinGong2013/apkgo/pkg/store"
)

// Config is the top-level YAML configuration.
type Config struct {
	Stores map[string]map[string]string `yaml:"stores"`
}

// Load reads a YAML config file and merges environment variable overrides.
//
// Environment variables follow the pattern: APKGO_<STORE>_<KEY>
// For example: APKGO_HUAWEI_CLIENT_ID, APKGO_XIAOMI_EMAIL
//
// If the config file does not exist but environment variables define at least
// one store, the config is built entirely from env vars.
func Load(path string) (*Config, error) {
	cfg := &Config{Stores: map[string]map[string]string{}}

	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("read config: %w", err)
		}
		// File doesn't exist — that's OK if env vars provide stores
	} else {
		if err := yaml.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("parse config: %w", err)
		}
		if cfg.Stores == nil {
			cfg.Stores = map[string]map[string]string{}
		}
	}

	// Merge environment variables: APKGO_<STORE>_<KEY>=value
	mergeEnv(cfg)

	if len(cfg.Stores) == 0 {
		return nil, fmt.Errorf("no stores configured (check %s or APKGO_* env vars)", path)
	}

	return cfg, nil
}

// mergeEnv scans environment for APKGO_<STORE>_<KEY> variables and
// merges them into the config. Env vars override file values.
func mergeEnv(cfg *Config) {
	const prefix = "APKGO_"
	for _, env := range os.Environ() {
		if !strings.HasPrefix(env, prefix) {
			continue
		}
		parts := strings.SplitN(env, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := parts[0][len(prefix):] // e.g. "HUAWEI_CLIENT_ID"
		value := parts[1]

		// Split into store name and field: HUAWEI + CLIENT_ID
		idx := strings.Index(key, "_")
		if idx <= 0 {
			continue
		}
		storeName := strings.ToLower(key[:idx])
		fieldKey := strings.ToLower(key[idx+1:])

		if cfg.Stores[storeName] == nil {
			cfg.Stores[storeName] = map[string]string{}
		}
		cfg.Stores[storeName][fieldKey] = value
	}
}

// CreateStores instantiates Store implementations from config.
// If filter is non-empty, only those stores are created.
func (c *Config) CreateStores(filter []string) ([]store.Store, error) {
	wanted := make(map[string]bool)
	for _, name := range filter {
		wanted[name] = true
	}

	var stores []store.Store
	for name, cfg := range c.Stores {
		if len(wanted) > 0 && !wanted[name] {
			continue
		}
		s, err := store.Create(name, cfg)
		if err != nil {
			return nil, fmt.Errorf("store %q: %w", name, err)
		}
		stores = append(stores, s)
	}

	if len(stores) == 0 {
		return nil, fmt.Errorf("no stores configured (or all filtered out)")
	}
	return stores, nil
}
