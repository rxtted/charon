package card

import (
	"testing"
	"time"

	"github.com/rxtted/charon/internal/store"
)

func TestBuildResolvesPlaceholders(t *testing.T) {
	desc := "{body}"
	st := Style{
		Icon:        "https://x/i.png",
		Title:       "{title}",
		Description: &desc,
		Glance:      []GlanceItem{{Value: "{labels.cond}", Code: true}},
		Data:        []DataItem{{Label: "Host", Value: "{host}"}, {Label: "Job", Value: "{labels.job}"}},
		Footer:      []string{"{host}", "{source}"},
		Links:       []Link{{Label: "Open", URL: "{link}"}},
	}
	in := &store.Incident{
		Source: "grafana", Severity: "critical", Title: "up down",
		Body: "the agent stopped", Host: "titan", Link: "https://g/1",
		Labels:    map[string]string{"cond": "up == 0"},
		CreatedAt: time.Unix(0, 0),
	}
	r := Build(in, st)

	if r.Title != "up down" || r.Description != "the agent stopped" {
		t.Fatalf("title/desc: %q / %q", r.Title, r.Description)
	}
	if r.Severity != "Critical" {
		t.Fatalf("severity lead not title-cased: %q", r.Severity)
	}
	if len(r.Glance) != 1 || r.Glance[0].Value != "up == 0" || !r.Glance[0].Code {
		t.Fatalf("glance: %+v", r.Glance)
	}
	if len(r.Data) != 1 || r.Data[0].Label != "Host" || r.Data[0].Value != "titan" {
		t.Fatalf("data not filtered on absent label: %+v", r.Data)
	}
	if len(r.Links) != 1 || r.Links[0].URL != "https://g/1" {
		t.Fatalf("link: %+v", r.Links)
	}
}

func TestBuildOmitsPartialMissingLabel(t *testing.T) {
	in := &store.Incident{Source: "s", Severity: "warning", Title: "t", CreatedAt: time.Unix(0, 0)}
	st := Style{
		Title:  "{title}",
		Data:   []DataItem{{Label: "Job", Value: "job {labels.job}"}}, // job absent on the event
		Footer: []string{"{source}"},
	}
	r := Build(in, st)
	if len(r.Data) != 0 {
		t.Fatalf("data item with an absent label should be omitted, got %+v", r.Data)
	}
}

func TestBuildDropsNonHTTPLink(t *testing.T) {
	in := &store.Incident{Source: "s", Severity: "warning", Title: "t", CreatedAt: time.Unix(0, 0),
		Labels: map[string]string{"u": "ftp://x/y"}}
	st := Style{Title: "{title}", Links: []Link{{Label: "Open", URL: "{labels.u}"}}}
	r := Build(in, st)
	if len(r.Links) != 0 {
		t.Fatalf("non-http link should be dropped, got %+v", r.Links)
	}
}
