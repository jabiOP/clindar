package model

import "strings"

// Calendar is a calendar the account can see, from Google's calendar list.
type Calendar struct {
	AccountID  string `json:"account_id"`
	ID         string `json:"id"`
	Name       string `json:"name"`
	Color      string `json:"color,omitempty"`       // "#rrggbb"
	AccessRole string `json:"access_role,omitempty"` // owner | writer | reader | freeBusyReader
	Primary    bool   `json:"primary,omitempty"`
	Selected   bool   `json:"selected"` // shown by default (Google's "selected" flag)
	Visible    bool   `json:"visible"`  // shown in clindar: the local choice if any, else Selected
	TimeZone   string `json:"time_zone,omitempty"`
}

func (c Calendar) Writable() bool { return c.AccessRole == "owner" || c.AccessRole == "writer" }

// EffectiveColor is the color an event is drawn in, as Google Calendar's web
// UI draws it: its own color (colorId, through palette or else EventPalette)
// if it has one, else its calendar's.
func EffectiveColor(e Event, cal Calendar, palette map[string]string) string {
	if e.ColorID != "" {
		if c, ok := palette[e.ColorID]; ok {
			return WebColor(c)
		}
		if c, ok := EventPalette[e.ColorID]; ok {
			return c
		}
	}
	return WebColor(cal.Color)
}

// EventPalette is Google's event colors (colorId → "#rrggbb") as the web UI
// draws them: Lavender, Sage, Grape, Flamingo, Banana, Tangerine, Peacock,
// Graphite, Blueberry, Basil, Tomato.
var EventPalette = map[string]string{
	"1": "#7986cb", "2": "#33b679", "3": "#8e24aa", "4": "#e67c73", "5": "#f6bf26", "6": "#f4511e",
	"7": "#039be5", "8": "#616161", "9": "#3f51b5", "10": "#0b8043", "11": "#d50000",
}

// webColors maps the API's colors to the web UI's. The API (colors.get, and
// a calendar's backgroundColor) still reports Google's old, paler palette for
// the same choices; a custom color isn't in it and passes through.
var webColors = map[string]string{
	// event colors 1–11
	"#a4bdfc": "#7986cb", "#7ae7bf": "#33b679", "#dbadff": "#8e24aa", "#ff887c": "#e67c73", "#fbd75b": "#f6bf26",
	"#ffb878": "#f4511e", "#46d6db": "#039be5", "#e1e1e1": "#616161", "#5484ed": "#3f51b5", "#51b749": "#0b8043",
	"#dc2127": "#d50000",
	// calendar colors 1–24
	"#ac725e": "#795548", "#d06b64": "#e67c73", "#f83a22": "#d50000", "#fa573c": "#f4511e", "#ff7537": "#ef6c00",
	"#ffad46": "#f09300", "#42d692": "#009688", "#16a765": "#0b8043", "#7bd148": "#7cb342", "#b3dc6c": "#c0ca33",
	"#fbe983": "#e4c441", "#fad165": "#f6bf26", "#92e1c0": "#33b679", "#9fe1e7": "#039be5", "#9fc6e7": "#4285f4",
	"#4986e7": "#3f51b5", "#9a9cff": "#7986cb", "#b99aff": "#b39ddb", "#c2c2c2": "#616161", "#cabdbf": "#a79b8e",
	"#cca6ac": "#ad1457", "#f691b2": "#d81b60", "#cd74e6": "#8e24aa", "#a47ae2": "#9e69af",
}

// WebColor is hex as Google Calendar's web UI shows it.
func WebColor(hex string) string {
	if c, ok := webColors[strings.ToLower(hex)]; ok {
		return c
	}
	return hex
}
