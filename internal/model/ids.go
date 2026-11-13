package model

import (
	"crypto/rand"
	"encoding/base32"
	"strings"
	"time"
)

var idEncoding = base32.HexEncoding.WithPadding(base32.NoPadding)

// NewEventID returns a random 26-char ID using only [a-v0-9], which Google
// accepts for client-supplied event IDs. Generating IDs locally means an event
// created offline already has its final ID.
func NewEventID() string {
	b := make([]byte, 16)
	rand.Read(b) // never returns an error since Go 1.24
	return strings.ToLower(idEncoding.EncodeToString(b))
}

// InstanceID is the ID Google assigns to one occurrence of a recurring event.
func InstanceID(seriesID string, originalStart time.Time, allDay bool) string {
	if allDay {
		return seriesID + "_" + originalStart.UTC().Format("20060102")
	}
	return seriesID + "_" + originalStart.UTC().Format("20060102T150405Z")
}

// ParseInstanceID is the inverse of InstanceID. ok is false for IDs that
// aren't instance IDs (event IDs never contain "_": base32hex has no underscore).
func ParseInstanceID(id string) (seriesID string, originalStart time.Time, allDay, ok bool) {
	i := strings.LastIndexByte(id, '_')
	if i <= 0 {
		return "", time.Time{}, false, false
	}
	seriesID, stamp := id[:i], id[i+1:]
	if t, err := time.Parse("20060102T150405Z", stamp); err == nil {
		return seriesID, t, false, true
	}
	if t, err := time.Parse("20060102", stamp); err == nil {
		return seriesID, t, true, true
	}
	return "", time.Time{}, false, false
}
