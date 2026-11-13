package config

import (
	"errors"
	"fmt"
	"io/fs"
	"time"

	"github.com/BurntSushi/toml"
)

// Duration lets TOML values like "45s" decode into a time.Duration.
type Duration struct{ time.Duration }

func (d *Duration) UnmarshalText(b []byte) error {
	v, err := time.ParseDuration(string(b))
	if err != nil {
		return err
	}
	d.Duration = v
	return nil
}

type Config struct {
	SyncInterval    Duration `toml:"sync_interval"`    // daemon/TUI poll period
	StaleAfter      Duration `toml:"stale_after"`      // CLI reads sync first if older than this
	SyncTimeout     Duration `toml:"sync_timeout"`     // max time a CLI read waits for that sync
	DefaultCalendar string   `toml:"default_calendar"` // name or ID; "" = primary
	WeekStart       string   `toml:"week_start"`       // "monday" | "sunday"
	SendUpdates     string   `toml:"send_updates"`     // "all" | "none": email attendees on changes
}

func Default() Config {
	return Config{
		SyncInterval: Duration{45 * time.Second},
		StaleAfter:   Duration{2 * time.Minute},
		SyncTimeout:  Duration{5 * time.Second},
		WeekStart:    "monday",
		SendUpdates:  "all",
	}
}

// Load reads path over the defaults. A missing file is not an error.
func Load(path string) (Config, error) {
	c := Default()
	md, err := toml.DecodeFile(path, &c)
	if errors.Is(err, fs.ErrNotExist) {
		return Default(), nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("%s: %w", path, err)
	}
	if und := md.Undecoded(); len(und) > 0 {
		return Config{}, fmt.Errorf("%s: unknown key %q", path, und[0].String())
	}
	if err := c.validate(); err != nil {
		return Config{}, fmt.Errorf("%s: %w", path, err)
	}
	return c, nil
}

func (c Config) validate() error {
	if c.WeekStart != "monday" && c.WeekStart != "sunday" {
		return fmt.Errorf("week_start must be \"monday\" or \"sunday\", got %q", c.WeekStart)
	}
	if c.SendUpdates != "all" && c.SendUpdates != "none" {
		return fmt.Errorf("send_updates must be \"all\" or \"none\", got %q", c.SendUpdates)
	}
	if c.SyncInterval.Duration < 10*time.Second {
		return fmt.Errorf("sync_interval must be at least 10s (Google rate limits), got %v", c.SyncInterval)
	}
	return nil
}

func (c Config) FirstWeekday() time.Weekday {
	if c.WeekStart == "sunday" {
		return time.Sunday
	}
	return time.Monday
}
