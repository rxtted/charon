package event

import (
	"strings"
	"testing"
)

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

func TestBehaviorTable(t *testing.T) {
	a, n := Alert.Behavior(), Notify.Behavior()
	if !a.Dedups || !a.Renotifies || a.AckCloses || len(a.Actions) != 3 {
		t.Fatalf("alert behaviour wrong: %+v", a)
	}
	if n.Dedups || n.Renotifies || !n.AckCloses || len(n.Actions) != 1 || n.Actions[0] != Ack {
		t.Fatalf("notify behaviour wrong: %+v", n)
	}
	if Kind("nonsense").Behavior().AckCloses { // unknown falls back to alert
		t.Fatal("unknown kind should fall back to alert behaviour")
	}
}

func TestNormalizeDefaultsAlert(t *testing.T) {
	e := Event{Source: "s", Channel: "c", Title: "t"}
	e.Normalize()
	if e.Kind != Alert {
		t.Fatalf("kind = %q, want alert", e.Kind)
	}
	if !strings.HasPrefix(e.DedupKey, "derived:") {
		t.Fatalf("alert should get a derived key, got %q", e.DedupKey)
	}
}

func TestNormalizeNotifyGetsUniqueKey(t *testing.T) {
	mk := func() string {
		e := Event{Kind: Notify, Source: "s", Channel: "c", Title: "same title", DedupKey: "ignored"}
		e.Normalize()
		return e.DedupKey
	}
	k1, k2 := mk(), mk()
	if !strings.HasPrefix(k1, "notify:") {
		t.Fatalf("notify key should be charon-owned, got %q", k1)
	}
	if k1 == k2 {
		t.Fatal("two notifies with the same title must get different keys")
	}
}

func TestNormalizeCanonicalizesUnknownKind(t *testing.T) {
	e := Event{Kind: "alrt", Source: "s", Channel: "c", Title: "t"} // a typo, not a real kind
	e.Normalize()
	if e.Kind != Alert {
		t.Fatalf("unknown kind should resolve to alert, got %q", e.Kind)
	}
	if !strings.HasPrefix(e.DedupKey, "derived:") {
		t.Fatalf("a canonicalized alert should get a derived key, got %q", e.DedupKey)
	}
}
