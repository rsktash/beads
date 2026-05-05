// Package idgen produces beads-style ids matching upstream's
// internal/idgen/hash.go: <prefix>-<base36hash> where the hash is derived
// from SHA-256 over (title|description|creator|timestamp_ns|nonce).
package idgen

import (
	"crypto/sha256"
	"fmt"
	"math"
	"math/big"
	"strings"
	"time"
)

const base36Alphabet = "0123456789abcdefghijklmnopqrstuvwxyz"

const (
	MinLen     = 3
	MaxLen     = 8
	DefaultLen = 6 // upstream default
)

// EncodeBase36 converts data to a base36 string, padded/truncated to length
// (keeps least-significant digits on truncation). Mirrors upstream.
func EncodeBase36(data []byte, length int) string {
	num := new(big.Int).SetBytes(data)
	base := big.NewInt(36)
	zero := big.NewInt(0)
	mod := new(big.Int)

	chars := make([]byte, 0, length)
	for num.Cmp(zero) > 0 {
		num.DivMod(num, base, mod)
		chars = append(chars, base36Alphabet[mod.Int64()])
	}
	var rev strings.Builder
	for i := len(chars) - 1; i >= 0; i-- {
		rev.WriteByte(chars[i])
	}
	s := rev.String()
	if len(s) < length {
		s = strings.Repeat("0", length-len(s)) + s
	}
	if len(s) > length {
		s = s[len(s)-length:]
	}
	return s
}

// GenerateHashID returns "<prefix>-<base36hash>". Direct port of upstream.
//
// Byte width per length: 3->2, 4->3, 5->4, 6->4, 7->5, 8->5. Outside that
// range we default to 2 bytes (3-char output).
func GenerateHashID(prefix, title, description, creator string, timestamp time.Time, length, nonce int) string {
	content := fmt.Sprintf("%s|%s|%s|%d|%d",
		title, description, creator, timestamp.UnixNano(), nonce)
	hash := sha256.Sum256([]byte(content))

	var nb int
	switch length {
	case 3:
		nb = 2
	case 4:
		nb = 3
	case 5, 6:
		nb = 4
	case 7, 8:
		nb = 5
	default:
		nb = 2
	}
	return prefix + "-" + EncodeBase36(hash[:nb], length)
}

// AdaptiveConfig parameterises hash-length growth. Defaults match upstream.
type AdaptiveConfig struct {
	MaxCollisionProbability float64 // default 0.25
	MinLength               int     // default 3
	MaxLength               int     // default 8
}

// DefaultAdaptive returns the upstream defaults.
func DefaultAdaptive() AdaptiveConfig {
	return AdaptiveConfig{MaxCollisionProbability: 0.25, MinLength: 3, MaxLength: 8}
}

// AdaptiveLength picks the smallest length whose birthday-paradox collision
// probability for `numIssues` items stays below the configured threshold.
func AdaptiveLength(numIssues int, cfg AdaptiveConfig) int {
	if cfg.MinLength <= 0 {
		cfg.MinLength = MinLen
	}
	if cfg.MaxLength <= 0 {
		cfg.MaxLength = MaxLen
	}
	if cfg.MaxCollisionProbability <= 0 {
		cfg.MaxCollisionProbability = 0.25
	}
	const base = 36.0
	for length := cfg.MinLength; length <= cfg.MaxLength; length++ {
		total := math.Pow(base, float64(length))
		expo := -float64(numIssues*numIssues) / (2.0 * total)
		prob := 1.0 - math.Exp(expo)
		if prob <= cfg.MaxCollisionProbability {
			return length
		}
	}
	return cfg.MaxLength
}

// ChildID composes a hierarchical id ("parent.N"). Atomic counter allocation
// is the storage layer's responsibility.
func ChildID(parent string, n int) string {
	return fmt.Sprintf("%s.%d", parent, n)
}

// MaxHierarchyDepth is the cap upstream imposes (3 levels: bd-X.1.2.3 max).
const MaxHierarchyDepth = 3

// HierarchyDepth counts dots in id (depth 0 = root, 1 = child, 2 = grandchild).
func HierarchyDepth(id string) int {
	n := 0
	for _, r := range id {
		if r == '.' {
			n++
		}
	}
	return n
}
