package server

import (
	"strings"
	"unicode"
)

const maxNameLen = 20

// sender is the connection abstraction the hub writes through. Production
// wraps a WebSocket (client.go); tests use an in-process fake (hub_test.go).
// trySend must be non-blocking: it returns false if the buffer is full or the
// connection is closed, which the hub treats as a disconnect (server.md §4).
type sender interface {
	trySend(b []byte) bool
	close()
}

// Session is one player's slot-bound state. Identity is the token (sb_token
// cookie); Name is display-only. out is swapped on reconnect.
type Session struct {
	Token  string
	Slot   int
	Name   string
	Online bool
	out    sender
}

// sanitizeName trims, strips control characters, and caps to maxNameLen runes.
// Returns ok=false if nothing printable remains.
func sanitizeName(raw string) (string, bool) {
	var b strings.Builder
	for _, r := range strings.TrimSpace(raw) {
		if unicode.IsControl(r) {
			continue
		}
		b.WriteRune(r)
	}
	name := strings.TrimSpace(b.String())
	if name == "" {
		return "", false
	}
	rs := []rune(name)
	if len(rs) > maxNameLen {
		name = string(rs[:maxNameLen])
	}
	return name, true
}
