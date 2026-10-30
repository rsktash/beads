//go:build darwin || linux

package main

import (
	"os"
	"syscall"
	"unsafe"
)

// isInteractiveTerminal reports whether f is a terminal. A character-device
// test is not enough: /dev/null is a character device, and it is the stdin an
// agent's shell runs with.
func isInteractiveTerminal(f *os.File) bool {
	var t syscall.Termios
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), uintptr(ioctlGetTermios), uintptr(unsafe.Pointer(&t)))
	return errno == 0
}
