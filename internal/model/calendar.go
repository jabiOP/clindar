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
