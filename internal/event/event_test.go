package event

import "testing"

func TestNormalizeDefaults(t *testing.T) {
	e := Event{Source: "prometheus", Channel: "alerts", Title: "disk usage high"}
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

func TestDeriveKeyStable(t *testing.T) {
	a := DeriveKey("prometheus", "alerts", "disk usage high")
	if a != DeriveKey("prometheus", "alerts", "disk usage high") {
		t.Fatal("derive is not stable across calls")
	}
	if a == DeriveKey("prometheus", "alerts", "other title") {
		t.Fatal("different titles derived the same key")
	}
}
