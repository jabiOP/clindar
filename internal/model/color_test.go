package model_test

import (
	"testing"

	"github.com/jabiOP/clindar/internal/model"
)

func TestEffectiveColor(t *testing.T) {
	palette := map[string]string{"11": "#dc2127", "99": "#123456"}
	cal := model.Calendar{Color: "#4285f4"}
	tests := []struct {
		name    string
		colorID string
		want    string
	}{
		{"own color, as the web UI draws it (Tomato)", "11", "#d50000"},
		{"own color not in the cached palette: the built-in one", "4", "#e67c73"},
		{"an unknown palette color passes through", "99", "#123456"},
		{"no own color: the calendar's", "", "#4285f4"},
		{"unknown color id: the calendar's", "42", "#4285f4"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := model.EffectiveColor(model.Event{ColorID: tt.colorID}, cal, palette); got != tt.want {
				t.Errorf("EffectiveColor = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestWebColorMapsTheOldPalette(t *testing.T) {
	for old, want := range map[string]string{"#9FE1E7": "#039be5", "#16a765": "#0b8043", "#123abc": "#123abc"} {
		if got := model.WebColor(old); got != want {
			t.Errorf("WebColor(%s) = %s, want %s", old, got, want)
		}
	}
}
