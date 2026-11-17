package model

import "strings"

// Calendar is a calendar the account can see, from Google's calendar list.
type Calendar struct {
	AccountID  string  `json:"account_id"`
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	Color      string  `json:"color,omitempty"`       // "#rrggbb"
	AccessRole string  `json:"access_role,omitempty"` // owner | writer | reader | freeBusyReader
	Primary    bool    `json:"primary,omitempty"`
	Selected   bool    `json:"selected"` // shown by default (Google's "selected" flag)
	Visible    bool    `json:"visible"`  // shown in clindar: the local choice if any, else Selected
	TimeZone   string  `json:"time_zone,omitempty"`
	Labels     []Label `json:"labels,omitempty"` // event color labels, as in Google's color menu
}

// Label is one of a calendar's event colors (Google's "event label"): the 24
// defaults and any custom ones.
type Label struct {
	ID    string `json:"id"`
	Name  string `json:"name,omitempty"`
	Color string `json:"color"` // "#rrggbb"
}

// defaultLabelNames are the names Google Calendar's web UI gives its 24
// default event colors. The API returns those labels without a name.
var defaultLabelNames = map[string]string{
	"#795548": "Cocoa", "#e67c73": "Flamingo", "#d50000": "Tomato", "#f4511e": "Tangerine",
	"#ef6c00": "Pumpkin", "#f09300": "Mango", "#009688": "Eucalyptus", "#0b8043": "Basil",
	"#7cb342": "Pistachio", "#c0ca33": "Avocado", "#e4c441": "Citron", "#f6bf26": "Banana",
	"#33b679": "Sage", "#039be5": "Peacock", "#4285f4": "Cobalt", "#3f51b5": "Blueberry",
	"#7986cb": "Lavender", "#b39ddb": "Wisteria", "#616161": "Graphite", "#a79b8e": "Birch",
	"#ad1457": "Radicchio", "#d81b60": "Cherry blossom", "#8e24aa": "Grape", "#9e69af": "Amethyst",
}

// DisplayName is the label's name: its own, else the web UI's name for a
// default color, else its hex code.
func (l Label) DisplayName() string {
	if l.Name != "" {
		return l.Name
	}
	if n, ok := defaultLabelNames[strings.ToLower(l.Color)]; ok {
		return n
	}
	return l.Color
}

// Label returns the calendar's label with this ID.
func (c Calendar) Label(id string) (Label, bool) {
	for _, l := range c.Labels {
		if l.ID == id {
			return l, true
		}
	}
	return Label{}, false
}

func (c Calendar) Writable() bool { return c.AccessRole == "owner" || c.AccessRole == "writer" }

// EffectiveColor is the color an event is drawn in: its label's, else its own
// color from the palette (colorId → "#rrggbb"), else its calendar's.
func EffectiveColor(e Event, cal Calendar, palette map[string]string) string {
	if l, ok := cal.Label(e.LabelID); ok && e.LabelID != "" {
		return l.Color
	}
	if c, ok := palette[e.ColorID]; ok && e.ColorID != "" {
		return c
	}
	return cal.Color
}
