package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	DiscordToken  string
	ListenAddr    string // address the ingest server listens on, e.g. ":8000"
	MetricsAddr   string
	IngestToken   string // empty = ingest is unauthenticated
	MaxBodyBytes  int64
	DBPath        string
	Channels      map[string]string
	Fallback      string
	RenotifyEvery time.Duration
	ReaperGrace   time.Duration
	SnoozeOptions []time.Duration
}

type fileConfig struct {
	Channels      map[string]string `yaml:"channels"`
	Fallback      string            `yaml:"fallback"`
	RenotifyEvery time.Duration     `yaml:"renotify_every"`
	ReaperGrace   time.Duration     `yaml:"reaper_grace"`
	SnoozeOptions []time.Duration   `yaml:"snooze_options"`
}

func Load(lookup func(string) (string, bool), ymlPath string) (Config, error) {
	raw, err := os.ReadFile(ymlPath)
	if err != nil {
		return Config{}, fmt.Errorf("read config %s: %w", ymlPath, err)
	}
	var fc fileConfig
	if err := yaml.Unmarshal(raw, &fc); err != nil {
		return Config{}, fmt.Errorf("parse config %s: %w", ymlPath, err)
	}
	token, ok := lookup("CHARON_DISCORD_TOKEN")
	if !ok || token == "" {
		return Config{}, fmt.Errorf("CHARON_DISCORD_TOKEN is required")
	}
	cfg := Config{
		DiscordToken:  token,
		ListenAddr:    envOr(lookup, "CHARON_LISTEN_ADDR", ":8000"),
		MetricsAddr:   envOr(lookup, "CHARON_METRICS_ADDR", ":9095"),
		DBPath:        envOr(lookup, "CHARON_DB_PATH", "/data/charon.db"),
		Channels:      fc.Channels,
		Fallback:      fc.Fallback,
		RenotifyEvery: fc.RenotifyEvery,
		ReaperGrace:   fc.ReaperGrace,
		SnoozeOptions: fc.SnoozeOptions,
		MaxBodyBytes:  64 << 10,
	}
	if v, ok := lookup("CHARON_INGEST_TOKEN"); ok {
		cfg.IngestToken = v
	}
	if cfg.Fallback == "" {
		return Config{}, fmt.Errorf("config: fallback channel is required")
	}
	return cfg, nil
}

func (c Config) ChannelFor(tag string) string {
	if id, ok := c.Channels[tag]; ok {
		return id
	}
	return c.Fallback
}

// AllChannelIDs returns every configured discord channel id (every routed
// channel plus the fallback), deduplicated, for boot-time sweeps that need to
// touch every channel the bot might have posted to.
func (c Config) AllChannelIDs() []string {
	seen := make(map[string]bool, len(c.Channels)+1)
	var ids []string
	add := func(id string) {
		if id == "" || seen[id] {
			return
		}
		seen[id] = true
		ids = append(ids, id)
	}
	add(c.Fallback)
	for _, id := range c.Channels {
		add(id)
	}
	return ids
}

func envOr(lookup func(string) (string, bool), key, def string) string {
	if v, ok := lookup(key); ok && v != "" {
		return v
	}
	return def
}
