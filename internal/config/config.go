package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	DiscordToken  string
	ListenAddr    string // ingest listener inside the container, e.g. ":8000"; compose restricts host exposure
	MetricsAddr   string
	IngestToken   string // empty = trust the vlan
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

func envOr(lookup func(string) (string, bool), key, def string) string {
	if v, ok := lookup(key); ok && v != "" {
		return v
	}
	return def
}
