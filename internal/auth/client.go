// Package auth handles OAuth: the client credentials, the browser login flow,
// and storing/refreshing tokens.
package auth

import (
	"errors"
	"fmt"
	"io/fs"
	"os"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/calendar/v3"
)

// Set at build time for an official build, e.g.
//
//	go build -ldflags "-X github.com/jabiOP/clindar/internal/auth.BuiltinClientID=... -X ...BuiltinClientSecret=..."
//
// For a "Desktop app" OAuth client the secret is not confidential (Google says so).
var (
	BuiltinClientID     string
	BuiltinClientSecret string
)

var ErrNoClient = errors.New("no OAuth client configured: run `clindar auth setup path/to/client_secret.json` (see docs/setup-google-cloud.md)")

// LoadClient reads the user's client JSON (downloaded from Google Cloud
// Console). If the file doesn't exist, it falls back to the built-in client.
func LoadClient(path string) (*oauth2.Config, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		if BuiltinClientID == "" {
			return nil, ErrNoClient
		}
		return &oauth2.Config{
			ClientID: BuiltinClientID, ClientSecret: BuiltinClientSecret,
			Endpoint: google.Endpoint, Scopes: []string{calendar.CalendarScope},
		}, nil
	}
	if err != nil {
		return nil, err
	}
	cfg, err := google.ConfigFromJSON(data, calendar.CalendarScope)
	if err != nil {
		return nil, fmt.Errorf("%s: %w (expected the JSON for a \"Desktop app\" OAuth client)", path, err)
	}
	return cfg, nil
}
