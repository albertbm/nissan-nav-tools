//go:build linux

package main

import (
	"os"
	"os/exec"
)

// Relaunches the whole GUI under pkexec; splitting out just the card I/O would need a platformOpen contract change.
func relaunchElevated() bool {
	if os.Geteuid() == 0 {
		return false
	}
	pkexecPath, err := exec.LookPath("pkexec")
	if err != nil {
		return false
	}
	exe, err := os.Executable()
	if err != nil {
		return false
	}

	cmd := exec.Command(pkexecPath, "env",
		"DISPLAY="+os.Getenv("DISPLAY"),
		"XAUTHORITY="+os.Getenv("XAUTHORITY"),
		"WAYLAND_DISPLAY="+os.Getenv("WAYLAND_DISPLAY"),
		"XDG_RUNTIME_DIR="+os.Getenv("XDG_RUNTIME_DIR"),
		exe, "gui")
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return false
	}
	return true
}
