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

func TestALabelColorWins(t *testing.T) {
	cal := model.Calendar{Color: "#4285f4", Labels: []model.Label{{ID: "l1", Color: "#d81b60"}}}
	palette := map[string]string{"11": "#dc2127"}
	if got := model.EffectiveColor(model.Event{LabelID: "l1", ColorID: "11"}, cal, palette); got != "#d81b60" {
		t.Errorf("got %s, want the label's #d81b60", got)
	}
	if got := model.EffectiveColor(model.Event{LabelID: "gone", ColorID: "11"}, cal, palette); got != "#dc2127" {
		t.Errorf("unknown label: got %s, want the colorId's", got)
	}
}

func TestLabelDisplayName(t *testing.T) {
	for l, want := range map[model.Label]string{
		{Color: "#616161"}:              "Graphite",
		{Color: "#E4C441"}:              "Citron",
		{Name: "Gym", Color: "#616161"}: "Gym",
		{Color: "#123456"}:              "#123456",
	} {
		if got := l.DisplayName(); got != want {
			t.Errorf("%+v: %q, want %q", l, got, want)
		}
	}
}
