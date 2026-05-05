package idgen

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// New returns a hash-style id like "bd-a1b2".
// 16 bits of entropy is enough for the small per-repo issue space; on collision
// the storage layer retries.
func New() string {
	var b [2]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(fmt.Errorf("idgen: %w", err))
	}
	return "bd-" + hex.EncodeToString(b[:])
}

// Child returns the next hierarchical id given a parent id and the current
// number of existing children. e.g. Child("bd-a3f8", 0) -> "bd-a3f8.1".
func Child(parent string, existing int) string {
	return fmt.Sprintf("%s.%d", parent, existing+1)
}
