package card

import "testing"

func TestResolveLayers(t *testing.T) {
	def := "d body"
	set := NewSet(map[string]Style{
		"default": {Title: "{title}", Description: &def, Footer: []string{"{source}"}},
		"grafana": {Icon: "https://x/i.png", Footer: []string{"{host}", "{source}"}},
	})

	g := set.Resolve("grafana")
	if g.Icon != "https://x/i.png" {
		t.Fatalf("icon not taken from named block: %q", g.Icon)
	}
	if g.Title != "{title}" {
		t.Fatalf("title not inherited from default: %q", g.Title)
	}
	if len(g.Footer) != 2 || g.Footer[0] != "{host}" {
		t.Fatalf("footer not overridden by named block: %v", g.Footer)
	}

	u := set.Resolve("unknown")
	if u.Title != "{title}" || len(u.Footer) != 1 || u.Footer[0] != "{source}" {
		t.Fatalf("unknown source did not fall to default: %+v", u)
	}
}

func TestResolveMergesAccentPerSeverity(t *testing.T) {
	set := NewSet(map[string]Style{
		"default": {Accent: map[string]string{"critical": "AAAAAA"}},
		"grafana": {Accent: map[string]string{"warning": "BBBBBB"}},
	})
	g := set.Resolve("grafana")
	if g.Accent["critical"] != "AAAAAA" {
		t.Fatalf("default critical accent lost after a sender overrode only warning: %v", g.Accent)
	}
	if g.Accent["warning"] != "BBBBBB" {
		t.Fatalf("sender warning accent not applied: %v", g.Accent)
	}
}
