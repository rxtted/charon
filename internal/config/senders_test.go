package config

import (
	"strings"
	"testing"

	"github.com/rxtted/charon/internal/card"
)

func intptr(n int) *int { return &n }

func TestValidateSenders(t *testing.T) {
	bad := []struct {
		name string
		st   card.Style
		want string
	}{
		{"unknown token", card.Style{Title: "{nope}"}, "placeholder"},
		{"capitalised token", card.Style{Title: "{Title}"}, "placeholder"},
		{"empty label key", card.Style{Title: "{labels.}"}, "placeholder"},
		{"hyphen label key", card.Style{Data: []card.DataItem{{Value: "{labels.job-name}"}}}, "placeholder"},
		{"duration outside footer", card.Style{Title: "{duration}"}, "placeholder"},
		{"unmatched brace", card.Style{Title: "{title"}, "brace"},
		{"nested brace", card.Style{Title: "{{title}}"}, "brace"},
		{"empty link label", card.Style{Links: []card.Link{{URL: "{link}"}}}, "label is required"},
		{"placeholder link label", card.Style{Links: []card.Link{{Label: "{title}", URL: "{link}"}}}, "static text"},
		{"bad icon", card.Style{Icon: "ftp://x"}, "icon url"},
		{"bad accent", card.Style{Accent: map[string]string{"critical": "zzz"}}, "accent"},
		{"bad wrap", card.Style{Wrap: intptr(0)}, "wrap"},
		{"too many links", card.Style{Links: []card.Link{{URL: "{link}"}, {URL: "{link}"}, {URL: "{link}"}}}, "at most two"},
		{"long label", card.Style{Links: []card.Link{{Label: strings.Repeat("x", 81), URL: "{link}"}}}, "label"},
	}
	for _, tc := range bad {
		if err := validateStyle("s", tc.st); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s: err = %v, want contains %q", tc.name, err, tc.want)
		}
	}
	ok := card.Style{
		Title:  "{title}",
		Icon:   "https://x/i.png",
		Glance: []card.GlanceItem{{Value: "{labels.c}"}},
		Footer: []string{"{duration}"}, // duration is allowed in the footer
		Wrap:   intptr(48),
	}
	if err := validateStyle("s", ok); err != nil {
		t.Fatalf("valid style rejected: %v", err)
	}
}
