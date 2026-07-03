package truenas

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rxtted/charon/internal/event"
)

func match(t *testing.T, body string) []event.Event {
	t.Helper()
	r := httptest.NewRequest("POST", "/truenas", strings.NewReader(body))
	evs, err := New().Match(r)
	if err != nil {
		t.Fatalf("Match: %v", err)
	}
	return evs
}

const firingBody = `{"text": "TrueNAS @ tank  \n  \nNew alert:\n\n  * Pool HDD state is DEGRADED: One or more devices are faulted.\n\nCurrent alerts:\n\n  * Pool HDD state is DEGRADED: One or more devices are faulted."}`

const clearedBody = `{"text": "TrueNAS @ tank  \n  \nThe following alert has been cleared:\n\n  * Pool HDD state is DEGRADED: One or more devices are faulted."}`

const testAlertBody = `{"text": "TrueNAS @ tank  \n  \nThis is a test alert"}`

func TestFiringFromNew(t *testing.T) {
	evs := match(t, firingBody)
	if len(evs) != 1 {
		t.Fatalf("want 1 firing, got %d: %+v", len(evs), evs)
	}
	e := evs[0]
	if e.Source != "truenas" || e.Channel != "infra" || e.Status != event.Firing || e.Kind != event.Alert {
		t.Fatalf("bad routing: %+v", e)
	}
	if e.Severity != event.Critical { // "DEGRADED" is a fault token
		t.Fatalf("severity = %q, want critical", e.Severity)
	}
	if e.Host != "tank" || !strings.HasPrefix(e.Title, "Pool HDD state is DEGRADED") {
		t.Fatalf("host/title: %+v", e)
	}
	if !strings.HasPrefix(e.DedupKey, "truenas:") {
		t.Fatalf("dedup = %q", e.DedupKey)
	}
}

func TestClearedResolves(t *testing.T) {
	evs := match(t, clearedBody)
	if len(evs) != 1 || evs[0].Status != event.Resolved {
		t.Fatalf("want one resolved, got %+v", evs)
	}
}

func TestFireAndClearShareKey(t *testing.T) {
	fire := match(t, firingBody)[0]
	clear := match(t, clearedBody)[0]
	if fire.DedupKey != clear.DedupKey {
		t.Fatalf("fire %q != clear %q; a cleared alert must resolve its firing card", fire.DedupKey, clear.DedupKey)
	}
}

func TestTestAlertDropped(t *testing.T) {
	if evs := match(t, testAlertBody); evs != nil {
		t.Fatalf("a test alert has no section, want drop, got %+v", evs)
	}
}

func TestSeverityDefaultsWarning(t *testing.T) {
	body := `{"text":"TrueNAS @ tank  \n  \nNew alert:\n\n  * New feature update 25.04 is available."}`
	if evs := match(t, body); evs[0].Severity != event.Warning {
		t.Fatalf("non-fault alert should be warning, got %q", evs[0].Severity)
	}
}

func TestDistinctDisksDistinctKeys(t *testing.T) {
	k := func(disk string) string {
		body := `{"text":"TrueNAS @ tank  \n  \nNew alert:\n\n  * ` + disk + ` is OFFLINE."}`
		return match(t, body)[0].DedupKey
	}
	if k("Disk 1") == k("Disk 2") {
		t.Fatal("two different disks must not collapse onto one dedup key")
	}
}

func TestUnrecognizedRejected(t *testing.T) {
	// valid JSON but not a truenas payload (no "TrueNAS @ <host>" line): reject, don't drop.
	r := httptest.NewRequest("POST", "/truenas", strings.NewReader(`{"text":"some unrelated slack message"}`))
	if _, err := New().Match(r); err == nil {
		t.Fatal("a body with no host line should be rejected, not silently accepted")
	}
}

func TestRejectsGarbage(t *testing.T) {
	r := httptest.NewRequest("POST", "/truenas", strings.NewReader("not json"))
	if _, err := New().Match(r); err == nil {
		t.Fatal("expected error on malformed body")
	}
}
