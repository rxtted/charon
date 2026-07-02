package config

import (
	"testing"
	"time"
)

func TestLoadMergesEnvAndYAML(t *testing.T) {
	env := map[string]string{
		"CHARON_DISCORD_TOKEN": "tok",
		"CHARON_INGEST_TOKEN":  "secret",
	}
	lookup := func(k string) (string, bool) { v, ok := env[k]; return v, ok }
	cfg, err := Load(lookup, "testdata/config.yml")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DiscordToken != "tok" {
		t.Fatalf("token = %q", cfg.DiscordToken)
	}
	if cfg.ChannelFor("network") != cfg.ChannelFor("infra") {
		t.Fatal("network should share the infra channel")
	}
	if cfg.ChannelFor("unknown") != cfg.Fallback {
		t.Fatal("unknown tag should fall back")
	}
	if cfg.RenotifyEvery != 4*time.Hour {
		t.Fatalf("renotify = %s", cfg.RenotifyEvery)
	}
}
