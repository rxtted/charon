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

// TestAllChannelIDsDedupes: the boot orphan sweep needs every
// distinct channel id, routed or fallback, exactly once.
func TestAllChannelIDsDedupes(t *testing.T) {
	env := map[string]string{"CHARON_DISCORD_TOKEN": "tok"}
	lookup := func(k string) (string, bool) { v, ok := env[k]; return v, ok }
	cfg, err := Load(lookup, "testdata/config.yml")
	if err != nil {
		t.Fatal(err)
	}
	ids := cfg.AllChannelIDs()
	want := map[string]bool{"111": true, "222": true}
	if len(ids) != len(want) {
		t.Fatalf("AllChannelIDs() = %v, want %d distinct ids", ids, len(want))
	}
	for _, id := range ids {
		if !want[id] {
			t.Fatalf("unexpected id %q in %v", id, ids)
		}
	}
}
