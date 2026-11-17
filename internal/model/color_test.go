package model_test

import (
	"testing"

	"github.com/jabiOP/clindar/internal/model"
)

func TestEffectiveColor(t *testing.T) {
	palette := map[string]string{"11": "#dc2127"}
	cal := model.Calendar{Color: "#4285f4"}
	tests := []struct {
		name    string
		colorID string
		want    string
	}{
		{"own color from the palette", "11", "#dc2127"},
		{"no own color: the calendar's", "", "#4285f4"},
		{"own color missing from the palette: the calendar's", "99", "#4285f4"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := model.EffectiveColor(model.Event{ColorID: tt.colorID}, cal, palette); got != tt.want {
				t.Errorf("EffectiveColor = %q, want %q", got, tt.want)
			}
		})
	}
}
