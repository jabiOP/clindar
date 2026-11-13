package timeparse_test

import (
	"testing"
	"time"

	"github.com/jabiOP/clindar/internal/timeparse"
)

func TestParseDuration(t *testing.T) {
	tests := []struct {
		in      string
		want    time.Duration
		wantErr bool
	}{
		{in: "30m", want: 30 * time.Minute},
		{in: "1h30m", want: 90 * time.Minute},
		{in: "2d", want: 48 * time.Hour},
		{in: "1w", want: 7 * 24 * time.Hour},
		{in: " 1H ", want: time.Hour},
		{in: "", wantErr: true},
		{in: "h", wantErr: true},
		{in: "10", wantErr: true}, // unit is required
		{in: "5s", wantErr: true}, // seconds are not useful for calendars
		{in: "1h-5m", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := timeparse.ParseDuration(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseDuration(%q) error = %v, wantErr %v", tt.in, err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("ParseDuration(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}
