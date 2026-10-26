package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"nissan-nav-tools/anonymize"
	"nissan-nav-tools/consent"
	"nissan-nav-tools/internal/clone"
	"nissan-nav-tools/internal/humansize"
	"nissan-nav-tools/trim"
)

// prepareTarget is in itself if inPlace, else a clone of in at out; in is never opened for writing.
func prepareTarget(in, out, suffix string, inPlace, force bool) (path string, err error) {
	if inPlace {
		return in, nil
	}
	if out == "" {
		out = defaultOutput(in, suffix)
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

func runAnonymizeCLI(args []string) int {
	fs := flag.NewFlagSet("anonymize", flag.ExitOnError)
	in := fs.String("in", "", "card image to anonymize (required)")
	out := fs.String("out", "", "output image (default: <in>.anon<ext>)")
	force := fs.Bool("force", false, "replace -out if it already exists")
	inPlace := fs.Bool("in-place", false, "write to -in directly instead of a copy")
	fs.Parse(args)

	if *in == "" {
		fmt.Fprintln(os.Stderr, "anonymize: -in is required")
		return 2
	}

	target, err := prepareTarget(*in, *out, ".anon", *inPlace, *force)
	if err != nil {
		fmt.Fprintf(os.Stderr, "anonymize: %v\n", err)
		return 1
	}

	f, err := os.OpenFile(target, os.O_RDWR, 0o644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "anonymize: opening %s: %v\n", target, err)
		return 1
	}
	defer f.Close()
	size, err := fileSize(f)
	if err != nil {
		fmt.Fprintf(os.Stderr, "anonymize: %v\n", err)
		return 1
	}

	rep, err := anonymize.Reset(f, size)
	if err != nil {
		fmt.Fprintf(os.Stderr, "anonymize: %v\n", err)
		return 1
	}

	fmt.Printf("anonymized %s\n", target)
	fmt.Printf("%d exFAT partition(s) walked, %d item(s) zeroed, %s total\n",
		rep.CWSPartitions, len(rep.Items), humansize.Format(rep.BytesZeroed()))
	for _, it := range rep.Items {
		fmt.Printf("  %-55s %-45s %8s\n", it.Path, it.What, humansize.Format(it.Size))
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

	fmt.Printf("patched %s\n", target)
	fmt.Printf("build %s, chunk %d: %d -> %d bytes, %d pages written\n",
		res.Target.Build, res.Chunk, res.Stock, res.Packed, res.Pages)
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
	if err := copyTrimmed(*in, outPath, target); err != nil {
		fmt.Fprintf(os.Stderr, "trim: %v\n", err)
		return 1
	}
	fmt.Printf("%s written from %s: %s -> %s, %s skipped\n",
		outPath, *in, humansize.Format(fullSize), humansize.Format(target), humansize.Format(skipped))
	return 0
}

// copyTrimmed copies only the first n bytes, so it never reads the discarded tail.
func copyTrimmed(src, dst string, n int64) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.CopyN(out, in, n); err != nil {
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
