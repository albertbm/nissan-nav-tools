// Command nissan-nav-tools anonymizes and patches Clarion QY8 map card images.
//
// Run with no arguments for the GUI, or "anonymize"/"consent" for the
// headless CLI; each also accepts "-h" for its own flags.
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

	switch args[0] {
	case "anonymize":
		os.Exit(runAnonymizeCLI(args[1:]))
	case "consent":
		os.Exit(runConsentCLI(args[1:]))
	case "restore":
		os.Exit(runRestoreCLI(args[1:]))
	case "trim":
		os.Exit(runTrimCLI(args[1:]))
	case "-h", "-help", "--help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "nissan-nav-tools: unknown command %q\n", args[0])
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `usage:
  nissan-nav-tools [gui]                       open the GUI
  nissan-nav-tools anonymize -in X [-out Y]     zero personal data on a copy of X
  nissan-nav-tools consent -in X [-out Y]       skip the consent screen on a copy of X
  nissan-nav-tools trim -in X [-out Y]          drop the dead space past X's last partition
  nissan-nav-tools restore -in X -disk diskN [-verify] [-yes]   write X onto a card, wiping it`)
}
