package model_test

import (
	"regexp"
	"testing"
	"time"

	"github.com/jabiOP/clindar/internal/model"
)

func TestNewEventIDIsGoogleCompatibleAndUnique(t *testing.T) {
	valid := regexp.MustCompile(`^[a-v0-9]+$`) // Google: chars a-v and 0-9, length 5–1024
	seen := map[string]bool{}
	for range 1000 {
		id := model.NewEventID()
		if !valid.MatchString(id) || len(id) < 5 || len(id) > 1024 {
			t.Fatalf("NewEventID() = %q, not valid base32hex lowercase", id)
		}
		if seen[id] {
			t.Fatalf("NewEventID() returned duplicate %q", id)
		}
		seen[id] = true
	}
}

func TestInstanceID(t *testing.T) {
	ny := mustLoc(t, "America/New_York")
	tests := []struct {
		name   string
		start  time.Time
		allDay bool
		want   string
	}{
		{"timed uses UTC basic format", time.Date(2026, 11, 16, 9, 0, 0, 0, ny), false, "abc_20261116T140000Z"},
		{"all-day uses date only", time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC), true, "abc_20261116"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := model.InstanceID("abc", tt.start, tt.allDay); got != tt.want {
				t.Errorf("InstanceID = %q, want %q", got, tt.want)
			}
		})
	}
}
