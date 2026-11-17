package model

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

// EffectiveColor is the color an event is drawn in: its own color from the
// palette (colorId → "#rrggbb") if it has one, else its calendar's.
func EffectiveColor(e Event, cal Calendar, palette map[string]string) string {
	if c, ok := palette[e.ColorID]; ok && e.ColorID != "" {
		return c
	}
	return cal.Color
}
