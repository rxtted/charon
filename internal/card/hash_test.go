package card

import "testing"

func baseRendered() Rendered {
	return Rendered{
		Title: "t", Description: "d", Severity: "Critical",
		Glance: []GlanceItem{{Value: "up == 0", Code: true}},
		Data:   []DataItem{{Label: "Host", Value: "titan"}},
		Footer: []string{"titan", "grafana"},
		Links:  []Link{{Label: "Open", URL: "https://g/1"}},
		Icon:   "https://x/i.png", Accent: 0xE74C3C,
	}
}

func TestContentHashCoversContentNotPresentation(t *testing.T) {
	h0 := baseRendered().ContentHash()

	content := []func(*Rendered){
		func(r *Rendered) { r.Title = "x" },
		func(r *Rendered) { r.Description = "x" },
		func(r *Rendered) { r.Severity = "Warning" },
		func(r *Rendered) { r.Glance[0].Value = "x" },
		func(r *Rendered) { r.Data[0].Value = "x" },
		func(r *Rendered) { r.Footer[0] = "x" },
		func(r *Rendered) { r.Note = "✓ Acknowledged by noah" },
		func(r *Rendered) { r.Links[0].URL = "https://g/2" },
		func(r *Rendered) { r.Links[0].Label = "View" },
	}
	for i, m := range content {
		c := baseRendered()
		m(&c)
		if c.ContentHash() == h0 {
			t.Fatalf("content mutator %d did not change the hash", i)
		}
	}

	presentation := []func(*Rendered){
		func(r *Rendered) { r.Icon = "https://x/other.png" },
		func(r *Rendered) { r.Accent = 0x000000 },
		func(r *Rendered) { r.Wrap = 40 },
	}
	for i, m := range presentation {
		c := baseRendered()
		m(&c)
		if c.ContentHash() != h0 {
			t.Fatalf("presentation mutator %d changed the hash", i)
		}
	}
}
