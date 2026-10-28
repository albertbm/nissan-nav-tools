package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"nissan-nav-tools/anonymize"
	"nissan-nav-tools/consent"
	"nissan-nav-tools/internal/clone"
	"nissan-nav-tools/internal/disk"
	"nissan-nav-tools/internal/humansize"
	"nissan-nav-tools/internal/progress"
	"nissan-nav-tools/trim"
)

func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

func cliProgress(total int64) func(int64) {
	tr := progress.New(total)
	tty := isTerminal(os.Stdout)
	return func(done int64) {
		st := tr.Update(done)
		if tty {
			fmt.Printf("\r%s", st.Line())
		}
	}
}

func cliProgressDone(start time.Time, totalBytes int64) {
	if isTerminal(os.Stdout) {
		fmt.Println()
	}
	fmt.Println(progress.Summary(time.Since(start), totalBytes))
}

// prepareTarget is in itself if inPlace, else a clone of in at out; in is never opened for writing.
func prepareTarget(in, out, suffix string, inPlace, force bool) (path string, err error) {
	if inPlace {
		return in, nil
	}
	if out == "" {
		out = defaultOutput(in, suffix)
	}
	if same, err := clone.SameFile(in, out); err != nil {
		return "", err
	} else if same {
		return "", fmt.Errorf("-out is the same file as -in; pick a different path or use -in-place")
	}
	if _, err := os.Stat(out); err == nil {
		if !force {
			return "", fmt.Errorf("%s already exists; pick another -out or pass -force", out)
		}
		if err := os.Remove(out); err != nil {
			return "", err
		}
	}
	if err := clone.Copy(in, out); err != nil {
		return "", fmt.Errorf("copying %s to %s: %w", in, out, err)
	}
	return out, nil
}

func defaultOutput(in, suffix string) string {
	ext := ""
	if i := strings.LastIndexAny(in, "./\\"); i >= 0 && in[i] == '.' {
		ext = in[i:]
		in = in[:i]
	}
	return in + suffix + ext
}

func runResetCLI(args []string) int {
	fs := flag.NewFlagSet("reset", flag.ExitOnError)
	in := fs.String("in", "", "card image to reset (required)")
	out := fs.String("out", "", "output image (default: <in>.reset<ext>)")
	force := fs.Bool("force", false, "replace -out if it already exists")
	inPlace := fs.Bool("in-place", false, "write to -in directly instead of a copy")
	fs.Parse(args)

	if *in == "" {
		fmt.Fprintln(os.Stderr, "reset: -in is required")
		return 2
	}

	target, err := prepareTarget(*in, *out, ".reset", *inPlace, *force)
	if err != nil {
		fmt.Fprintf(os.Stderr, "reset: %v\n", err)
		return 1
	}
	done := false
	defer func() {
		if !done && !*inPlace {
			os.Remove(target)
		}
	}()

	f, err := os.OpenFile(target, os.O_RDWR, 0o644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "reset: opening %s: %v\n", target, err)
		return 1
	}
	defer f.Close()
	size, err := fileSize(f)
	if err != nil {
		fmt.Fprintf(os.Stderr, "reset: %v\n", err)
		return 1
	}

	rep, err := anonymize.Reset(f, size)
	if err != nil {
		fmt.Fprintf(os.Stderr, "reset: %v\n", err)
		return 1
	}
	done = true

	fmt.Printf("reset %s\n", target)
	fmt.Printf("%d exFAT partition(s) walked, %d item(s) zeroed, %s total\n",
		rep.CWSPartitions, len(rep.Items), humansize.Format(rep.BytesZeroed()))
	for _, it := range rep.Items {
		fmt.Printf("  %-55s %-45s %8s\n", it.Path, it.What, humansize.Format(it.Size))
	}
	if rep.CWSPartitions > 0 {
		fmt.Printf("  %-55s %-45s %8s (%d cluster(s))\n",
			"<free space on the CWS partition(s)>", "old deleted/rewritten file residue",
			humansize.Format(rep.FreeBytesZeroed), rep.FreeClusters)
	}
	if rep.FrontGapFound {
		fmt.Printf("  %-55s %-45s %8s (%d markers seen)\n",
			"<front gap settings area>", "unit-written state, layout undecoded",
			humansize.Format(rep.FrontGapSize), rep.FrontGapRecords)
	} else {
		fmt.Println("  front gap not present (image too small) - nothing zeroed there")
	}
	return 0
}

func runConsentCLI(args []string) int {
	fs := flag.NewFlagSet("consent", flag.ExitOnError)
	in := fs.String("in", "", "card image to patch (required)")
	out := fs.String("out", "", "output image (default: <in>.consent<ext>)")
	force := fs.Bool("force", false, "replace -out if it already exists")
	inPlace := fs.Bool("in-place", false, "write to -in directly instead of a copy")
	fs.Parse(args)

	if *in == "" {
		fmt.Fprintln(os.Stderr, "consent: -in is required")
		return 2
	}

	target, err := prepareTarget(*in, *out, ".consent", *inPlace, *force)
	if err != nil {
		fmt.Fprintf(os.Stderr, "consent: %v\n", err)
		return 1
	}
	done := false
	defer func() {
		if !done && !*inPlace {
			os.Remove(target)
		}
	}()

	f, err := os.OpenFile(target, os.O_RDWR, 0o644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "consent: opening %s: %v\n", target, err)
		return 1
	}
	defer f.Close()
	size, err := fileSize(f)
	if err != nil {
		fmt.Fprintf(os.Stderr, "consent: %v\n", err)
		return 1
	}

	res, err := consent.ApplyConsentSkip(f, size, func(s string) { fmt.Println(s) })
	if err != nil {
		fmt.Fprintf(os.Stderr, "consent: %v\n", err)
		return 1
	}
	done = true

	fmt.Printf("patched %s\n", target)
	fmt.Printf("build %s, chunk %d: %d -> %d bytes, %d pages written\n",
		res.Target.Build, res.Chunk, res.Stock, res.Packed, res.Pages)
	return 0
}

func runRestoreCLI(args []string) int {
	fs := flag.NewFlagSet("restore", flag.ExitOnError)
	in := fs.String("in", "", "image file to write to the card (required)")
	diskID := fs.String("disk", "", "disk identifier to write onto, e.g. disk4 (required)")
	verify := fs.Bool("verify", false, "read the card back and compare its sha256 with the image")
	yes := fs.Bool("yes", false, "skip the typed confirmation")
	fs.Parse(args)

	if *in == "" || *diskID == "" {
		fmt.Fprintln(os.Stderr, "restore: -in and -disk are required")
		return 2
	}

	fi, err := os.Stat(*in)
	if err != nil {
		fmt.Fprintf(os.Stderr, "restore: %v\n", err)
		return 1
	}

	disks, err := disk.List()
	if err != nil {
		fmt.Fprintf(os.Stderr, "restore: %v\n", err)
		return 1
	}
	var d disk.Info
	found := false
	for _, c := range disks {
		if c.Identifier == *diskID {
			d, found = c, true
			break
		}
	}
	if !found {
		fmt.Fprintf(os.Stderr, "restore: %s is not an external, removable disk, or isn't attached\n", *diskID)
		return 1
	}
	if err := refuseUnlessCard(d); err != nil {
		fmt.Fprintf(os.Stderr, "restore: %v\n", err)
		return 1
	}
	if err := refuseIfTooSmall(d, fi.Size()); err != nil {
		fmt.Fprintf(os.Stderr, "restore: %v\n", err)
		return 1
	}

	fmt.Printf("image: %s, card: %s\n", humansize.Format(fi.Size()), humansize.Format(d.Size))
	if av := detectAppVersion(*in); av.err == nil {
		fmt.Printf("image app version: %s\n", av.name)
	}

	if !*yes {
		fmt.Printf("This erases everything on %s and writes %s to it. There is no undo.\n", d.Identifier, *in)
		fmt.Printf("Type %s to continue: ", d.Identifier)
		line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		if strings.TrimSpace(line) != d.Identifier {
			fmt.Fprintln(os.Stderr, "restore: confirmation didn't match, nothing written")
			return 1
		}
	}

	log := func(s string) { fmt.Println(s) }
	var card disk.RawDevice
	err = restoreSequence(
		func() error {
			start := time.Now()
			var err error
			card, err = restoreJob(d, *in, cliProgress(fi.Size()), log)
			if err != nil {
				return err
			}
			cliProgressDone(start, fi.Size())
			return nil
		},
		func() error {
			start := time.Now()
			if err := verifyRestore(card, *in, cliProgress(fi.Size()), log); err != nil {
				return err
			}
			cliProgressDone(start, fi.Size())
			return nil
		},
		*verify,
	)
	if card != nil {
		card.Close()
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "restore: %v\n", err)
		return 1
	}
	fmt.Printf("restored %s onto %s\n", *in, d.Identifier)
	return 0
}

func runTrimCLI(args []string) int {
	fs := flag.NewFlagSet("trim", flag.ExitOnError)
	in := fs.String("in", "", "card image to trim (required)")
	out := fs.String("out", "", "output image (default: <in>.trim<ext>)")
	force := fs.Bool("force", false, "replace -out if it already exists")
	inPlace := fs.Bool("in-place", false, "truncate -in directly instead of writing a copy")
	fs.Parse(args)

	if *in == "" {
		fmt.Fprintln(os.Stderr, "trim: -in is required")
		return 2
	}

	f, err := os.Open(*in)
	if err != nil {
		fmt.Fprintf(os.Stderr, "trim: %v\n", err)
		return 1
	}
	fi, err := f.Stat()
	if err != nil {
		f.Close()
		fmt.Fprintf(os.Stderr, "trim: %v\n", err)
		return 1
	}
	fullSize := fi.Size()
	target, err := trim.Size(f)
	f.Close()
	if err != nil {
		fmt.Fprintf(os.Stderr, "trim: %v\n", err)
		return 1
	}
	if target >= fullSize {
		fmt.Printf("%s is already %s, at or below the %s trim target; nothing to do\n",
			*in, humansize.Format(fullSize), humansize.Format(target))
		return 0
	}
	skipped := fullSize - target

	if *inPlace {
		if err := os.Truncate(*in, target); err != nil {
			fmt.Fprintf(os.Stderr, "trim: %v\n", err)
			return 1
		}
		fmt.Printf("%s: %s -> %s, %s skipped\n", *in, humansize.Format(fullSize), humansize.Format(target), humansize.Format(skipped))
		return 0
	}

	outPath := *out
	if outPath == "" {
		outPath = defaultOutput(*in, ".trim")
	}
	if same, err := clone.SameFile(*in, outPath); err != nil {
		fmt.Fprintf(os.Stderr, "trim: %v\n", err)
		return 1
	} else if same {
		fmt.Fprintln(os.Stderr, "trim: -out is the same file as -in; pick a different path or use -in-place")
		return 1
	}
	if _, err := os.Stat(outPath); err == nil {
		if !*force {
			fmt.Fprintf(os.Stderr, "trim: %s already exists; pick another -out or pass -force\n", outPath)
			return 1
		}
		if err := os.Remove(outPath); err != nil {
			fmt.Fprintf(os.Stderr, "trim: %v\n", err)
			return 1
		}
	}
	start := time.Now()
	if err := copyTrimmed(*in, outPath, target, cliProgress(target)); err != nil {
		fmt.Fprintf(os.Stderr, "trim: %v\n", err)
		return 1
	}
	cliProgressDone(start, target)
	fmt.Printf("%s written from %s: %s -> %s, %s skipped\n",
		outPath, *in, humansize.Format(fullSize), humansize.Format(target), humansize.Format(skipped))
	return 0
}

// copyTrimmed copies only the first n bytes, so it never reads the discarded tail.
func copyTrimmed(src, dst string, n int64, onProgress func(int64)) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	var r io.Reader = in
	if onProgress != nil {
		r = progress.NewCountingReader(in, onProgress)
	}
	if _, err := io.CopyN(out, r, n); err != nil {
		out.Close()
		os.Remove(dst)
		return err
	}
	return out.Close()
}

func fileSize(f *os.File) (int64, error) {
	fi, err := f.Stat()
	if err != nil {
		return 0, err
	}
	return fi.Size(), nil
}
