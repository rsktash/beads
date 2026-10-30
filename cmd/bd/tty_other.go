//go:build !darwin && !linux

package main

import "os"

// isInteractiveTerminal has no terminal probe on this platform, so it reports
// false and every owner-only terminal gate refuses.
func isInteractiveTerminal(f *os.File) bool { return false }
