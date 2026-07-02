package card

type GlanceItem struct {
	Value string `yaml:"value"`
	Code  bool   `yaml:"code"`
}

type DataItem struct {
	Label string `yaml:"label"`
	Value string `yaml:"value"`
}

type Link struct {
	Label string `yaml:"label"`
	URL   string `yaml:"url"`
}

type Style struct {
	Icon        string            `yaml:"icon"`
	Title       string            `yaml:"title"`
	Description *string           `yaml:"description"` // nil inherits; "" omits the line
	Glance      []GlanceItem      `yaml:"glance"`
	Data        []DataItem        `yaml:"data"`
	Footer      []string          `yaml:"footer"`
	Links       []Link            `yaml:"links"`
	Accent      map[string]string `yaml:"accent"` // severity to hex, e.g. "critical": "E74C3C"
	Wrap        *int              `yaml:"wrap"`   // nil inherits; body wrap column for this source
}

// builtin is the default styling a source with no config renders.
var builtin = Style{
	Title:       "{title}",
	Description: strptr("{body}"),
	Footer:      []string{"{host}", "{source}", "{time}"},
}

func strptr(s string) *string { return &s }

type Set struct {
	def   Style
	named map[string]Style
}

func NewSet(senders map[string]Style) Set {
	def := merge(builtin, senders["default"])
	named := make(map[string]Style, len(senders))
	for name, st := range senders {
		if name == "default" {
			continue
		}
		named[name] = merge(def, st)
	}
	return Set{def: def, named: named}
}

func (s Set) Resolve(source string) Style {
	if st, ok := s.named[source]; ok {
		return st
	}
	return s.def
}

// merge overlays over base: a set field on over wins, an unset field inherits base.
func merge(base, over Style) Style {
	out := base
	if over.Icon != "" {
		out.Icon = over.Icon
	}
	if over.Title != "" {
		out.Title = over.Title
	}
	if over.Description != nil {
		out.Description = over.Description
	}
	if over.Glance != nil {
		out.Glance = over.Glance
	}
	if over.Data != nil {
		out.Data = over.Data
	}
	if over.Footer != nil {
		out.Footer = over.Footer
	}
	if over.Links != nil {
		out.Links = over.Links
	}
	if over.Accent != nil {
		merged := make(map[string]string, len(out.Accent)+len(over.Accent))
		for sev, hex := range out.Accent {
			merged[sev] = hex
		}
		for sev, hex := range over.Accent {
			merged[sev] = hex
		}
		out.Accent = merged
	}
	if over.Wrap != nil {
		out.Wrap = over.Wrap
	}
	return out
}
