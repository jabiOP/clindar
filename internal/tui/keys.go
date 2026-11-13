package tui

import "github.com/charmbracelet/bubbles/key"

// keys is the single source of truth for bindings: Update matches on them
// and the help screen renders them.
var keys = struct {
	Left, Right, Up, Down, ScrollUp, ScrollDown, Today, Goto, Toggle, Open,
	Add, Edit, Delete, RSVP, Sync, Help, Quit, Back key.Binding
}{
	Left:       key.NewBinding(key.WithKeys("h", "left"), key.WithHelp("h/←", "previous day")),
	Right:      key.NewBinding(key.WithKeys("l", "right"), key.WithHelp("l/→", "next day")),
	Up:         key.NewBinding(key.WithKeys("k", "up"), key.WithHelp("k/↑", "up (week / event)")),
	Down:       key.NewBinding(key.WithKeys("j", "down"), key.WithHelp("j/↓", "down (week / event)")),
	ScrollUp:   key.NewBinding(key.WithKeys("K", "pgup"), key.WithHelp("K", "scroll a week back")),
	ScrollDown: key.NewBinding(key.WithKeys("J", "pgdown"), key.WithHelp("J", "scroll a week forward")),
	Today:      key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "today")),
	Goto:       key.NewBinding(key.WithKeys("g"), key.WithHelp("g", "go to date")),
	Toggle:     key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "grid ⇄ list")),
	Open:       key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open day / event")),
	Add:        key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "add event")),
	Edit:       key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "edit event (list)")),
	Delete:     key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "delete event (list)")),
	RSVP:       key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "RSVP (list)")),
	Sync:       key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "sync now")),
	Help:       key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
	Quit:       key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
	Back:       key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back / cancel")),
}

func helpText() string {
	s := "Keys\n\n"
	for _, b := range []key.Binding{keys.Left, keys.Right, keys.Up, keys.Down, keys.ScrollUp, keys.ScrollDown,
		keys.Today, keys.Goto, keys.Toggle, keys.Open, keys.Add, keys.Edit, keys.Delete, keys.RSVP,
		keys.Sync, keys.Back, keys.Quit} {
		s += "  " + fit(b.Help().Key, 8) + b.Help().Desc + "\n"
	}
	return s + "\nPress any key to close."
}
