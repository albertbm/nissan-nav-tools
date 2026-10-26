//go:build !windows && !linux

package main

// macOS asks per card operation through authopen instead.
func relaunchElevated() bool { return false }
