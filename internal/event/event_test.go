package event

import "testing"

func TestNormalizeDefaults(t *testing.T) {
	e := Event{Source: "sabnzbd", Channel: "downloads", Title: "download failed"}
	e.Normalize()
	if e.Severity != Warning {
		t.Fatalf("severity = %q, want warning", e.Severity)
	}
	if e.DedupKey == "" {
		t.Fatal("dedup key was not derived")
	}
	if e.Time.IsZero() {
		t.Fatal("time was not defaulted")
	}
}

func TestDeriveKeyStableAndDistinct(t *testing.T) {
	a := DeriveKey("sabnzbd", "downloads", "download failed")
	if a != DeriveKey("sabnzbd", "downloads", "download failed") {
		t.Fatal("derive is not stable across calls")
	}
	if a == DeriveKey("sabnzbd", "downloads", "other title") {
		t.Fatal("different titles derived the same key")
	}
}
