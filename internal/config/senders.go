package config

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/rxtted/charon/internal/card"
)

var allowedTokens = map[string]bool{
	"title": true, "body": true, "source": true, "host": true,
	"severity": true, "time": true, "link": true,
}

var (
	braceRe    = regexp.MustCompile(`\{[^{}]*\}`)
	labelKeyRe = regexp.MustCompile(`^labels\.[A-Za-z0-9_]+$`)
)

func validateStyle(name string, st card.Style) error {
	// {duration} is a footer-only value, so non-footer strings validate without it.
	for _, s := range nonFooterStrings(st) {
		if err := checkTokens(name, s, false); err != nil {
			return err
		}
	}
	for _, s := range st.Footer {
		if err := checkTokens(name, s, true); err != nil {
			return err
		}
	}
	if st.Icon != "" && !strings.HasPrefix(st.Icon, "http://") && !strings.HasPrefix(st.Icon, "https://") {
		return fmt.Errorf("sender %s: icon url must be http(s): %q", name, st.Icon)
	}
	for sev, hex := range st.Accent {
		if !isHex6(hex) {
			return fmt.Errorf("sender %s: accent[%s] must be 6 hex digits: %q", name, sev, hex)
		}
	}
	if st.Wrap != nil && *st.Wrap <= 0 {
		return fmt.Errorf("sender %s: wrap must be positive: %d", name, *st.Wrap)
	}
	if len(st.Links) > 2 {
		return fmt.Errorf("sender %s: at most two links per card (the action row holds five, three are lifecycle buttons)", name)
	}
	for _, l := range st.Links {
		if len(l.Label) > 80 {
			return fmt.Errorf("sender %s: link label over discord's 80-char button limit: %q", name, l.Label)
		}
	}
	return nil
}

// checkTokens rejects any brace-delimited run that isn't an allowed token or a
// labels.<key>, so a typo like {Title}, {labels.}, or {labels.job-name} fails at
// boot instead of rendering literally.
func checkTokens(name, s string, allowDuration bool) error {
	for _, m := range braceRe.FindAllString(s, -1) {
		key := m[1 : len(m)-1]
		if allowedTokens[key] || labelKeyRe.MatchString(key) {
			continue
		}
		if allowDuration && key == "duration" {
			continue
		}
		return fmt.Errorf("sender %s: unknown or malformed placeholder %s", name, m)
	}
	return nil
}

func nonFooterStrings(st card.Style) []string {
	out := []string{st.Title}
	if st.Description != nil {
		out = append(out, *st.Description)
	}
	for _, g := range st.Glance {
		out = append(out, g.Value)
	}
	for _, d := range st.Data {
		out = append(out, d.Value)
	}
	for _, l := range st.Links {
		out = append(out, l.URL)
	}
	return out
}

func isHex6(s string) bool {
	if len(s) != 6 {
		return false
	}
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}
