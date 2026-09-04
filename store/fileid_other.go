//go:build !darwin

package store

import (
	"fmt"
	"os"
	"syscall"
)

func sqliteFileIdentity(path string) (string, bool) {
	info, err := os.Stat(path)
	if err != nil {
		return "", false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return "", false
	}
	return fmt.Sprintf("%d", stat.Ino), true
}
