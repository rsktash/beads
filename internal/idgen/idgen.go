// Package idgen produces beads-style ids of the form "<prefix>-<hash>".
//
// The hash is the last N base36 characters of SHA-256 over a content seed
// plus a nonce; on collision the caller bumps the nonce and retries. The
// content seed ties the id to the bead's identity, which is what upstream
// gastownhall/beads does. Pure-random ids would also work for uniqueness but
// would lose stability under replay.
package idgen

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math/big"
	"strconv"
)

// DefaultLen is the default base36 hash length. 4 chars of base36 is
// 36^4 ≈ 1.7M ids per prefix — same as upstream's typical size.
const DefaultLen = 4

// MinLen / MaxLen mirror upstream's accepted range.
const (
	MinLen = 3
	MaxLen = 8
)

// New returns "<prefix>-<base36>" of the requested length. Caller-supplied
// `seed` should be a content-derived string (title|description|creator|ts);
// `nonce` is a counter the caller increments on collision retries.
func New(prefix, seed string, length int, nonce uint64) (string, error) {
	if prefix == "" {
		return "", fmt.Errorf("idgen: empty prefix")
	}
	if length < MinLen || length > MaxLen {
		return "", fmt.Errorf("idgen: length %d out of range [%d,%d]", length, MinLen, MaxLen)
	}
	var nb [8]byte
	binary.BigEndian.PutUint64(nb[:], nonce)
	h := sha256.Sum256(append([]byte(seed+"|"), nb[:]...))

	// Take enough leading bytes that base36(value) has >= length digits, then
	// keep the rightmost `length` chars (matches upstream "least significant
	// digits" strategy).
	bytes := bytesNeeded(length)
	z := new(big.Int).SetBytes(h[:bytes])
	s := z.Text(36)
	for len(s) < length {
		s = "0" + s
	}
	if len(s) > length {
		s = s[len(s)-length:]
	}
	return prefix + "-" + s, nil
}

// MustNew is a helper for callers who can't return an error (idgen errors are
// always programmer bugs).
func MustNew(prefix, seed string, length int, nonce uint64) string {
	id, err := New(prefix, seed, length, nonce)
	if err != nil {
		panic(err)
	}
	return id
}

// bytesNeeded returns how many bytes from the SHA-256 we need to encode N
// base36 digits without truncating the input below the desired width.
// 256-bit SHA outputs are way larger than 5 bytes; this function just
// matches upstream's mapping table for byte-width per length.
func bytesNeeded(length int) int {
	switch {
	case length <= 4:
		return 3
	case length <= 6:
		return 4
	default:
		return 5
	}
}

// ChildID composes a hierarchical id of the form "<parent>.<n>". Atomicity is
// the caller's responsibility (typically via a child_counters table).
func ChildID(parent string, n int) string {
	return parent + "." + strconv.Itoa(n)
}
