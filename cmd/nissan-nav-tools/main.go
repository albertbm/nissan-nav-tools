// Command nissan-nav-tools resets and patches Clarion QY8 map card images: run with no arguments for the GUI, or "reset"/"consent" for the headless CLI.
package main

import (
	"fmt"
	"os"
)

func main() {
	args := os.Args[1:]
	if len(args) == 0 || args[0] == "gui" {
		if relaunchElevated() {
			return
		}
		runGUI()
		return
	}

	if args[0] == "-h" || args[0] == "-help" || args[0] == "--help" {
		usage()
		return
	}
	run, ok := cliCommand(args[0])
	if !ok {
		fmt.Fprintf(os.Stderr, "nissan-nav-tools: unknown command %q\n", args[0])
		usage()
		os.Exit(2)
	}
	os.Exit(run(args[1:]))
}

// cliCommand maps a subcommand name to its runner. "anonymize" is the old name
// for "reset", kept so existing scripts still work.
func cliCommand(name string) (run func([]string) int, ok bool) {
	switch name {
	case "reset", "anonymize":
		return runResetCLI, true
	case "consent":
		return runConsentCLI, true
	case "restore":
		return runRestoreCLI, true
	case "trim":
		return runTrimCLI, true
	default:
		return nil, false
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `usage:
  nissan-nav-tools [gui]                       open the GUI
  nissan-nav-tools reset -in X [-out Y]         remove the previous user's data from a copy of X
  nissan-nav-tools consent -in X [-out Y]       skip the consent screen on a copy of X
  nissan-nav-tools trim -in X [-out Y]          drop the dead space past X's last partition
  nissan-nav-tools restore -in X -disk diskN [-verify] [-yes]   write X onto a card, wiping it`)
}
