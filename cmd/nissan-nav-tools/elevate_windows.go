//go:build windows

package main

import (
	"os"
	"strings"

	"golang.org/x/sys/windows"
)

// Declining the UAC prompt runs on unelevated, where card ops fail with their own access error.
func relaunchElevated() bool {
	if windows.GetCurrentProcessToken().IsElevated() {
		return false
	}

	exe, err := os.Executable()
	if err != nil {
		return false
	}
	dir, err := os.Getwd()
	if err != nil {
		dir = ""
	}

	verb, err1 := windows.UTF16PtrFromString("runas")
	file, err2 := windows.UTF16PtrFromString(exe)
	args, err3 := windows.UTF16PtrFromString(quoteArgs(os.Args[1:]))
	cwd, err4 := windows.UTF16PtrFromString(dir)
	if err1 != nil || err2 != nil || err3 != nil || err4 != nil {
		return false
	}

	err = windows.ShellExecute(0, verb, file, args, cwd, windows.SW_SHOWNORMAL)
	if err != nil {
		return false
	}
	return true
}

func quoteArgs(args []string) string {
	quoted := make([]string, len(args))
	for i, a := range args {
		if a == "" || strings.ContainsAny(a, " \t\"") {
			quoted[i] = `"` + strings.ReplaceAll(a, `"`, `\"`) + `"`
		} else {
			quoted[i] = a
		}
	}
	return strings.Join(quoted, " ")
}
