package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"

	"nissan-nav-tools/anonymize"
	"nissan-nav-tools/consent"
	"nissan-nav-tools/internal/clone"
	"nissan-nav-tools/internal/disk"
	"nissan-nav-tools/internal/humansize"
	"nissan-nav-tools/trim"
)

// patchRow is one listed patch; a new one is just a new entry here.
type patchRow struct {
	title string
	apply func(f consent.ReadWriterAt, size int64, log func(string)) (string, error)
}

var patchRows = []patchRow{
	{
		title: "Skip the telematics consent screen",
		apply: func(f consent.ReadWriterAt, size int64, log func(string)) (string, error) {
			res, err := consent.ApplyConsentSkip(f, size, log)
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("build %s, chunk %d: %d -> %d bytes, %d pages written",
				res.Target.Build, res.Chunk, res.Stock, res.Packed, res.Pages), nil
		},
	},
}

// appVersion is cached so Run can refuse without touching the disk again.
type appVersion struct {
	name      string
	supported bool
	err       error
}

func detectAppVersion(in string) appVersion {
	f, err := os.Open(in)
	if err != nil {
		return appVersion{err: err}
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return appVersion{err: err}
	}
	t, err := consent.Find(f, fi.Size())
	if err != nil {
		return appVersion{err: err}
	}
	return appVersion{name: t.Build, supported: consent.Supported(t.Build)}
}

// anonymizeCopy clones in to out and anonymizes the copy; in is never opened for writing.
func anonymizeCopy(in, out string, log func(string)) error {
	if _, err := os.Stat(out); err == nil {
		if err := os.Remove(out); err != nil {
			return err
		}
	}
	if err := clone.Copy(in, out); err != nil {
		return fmt.Errorf("copying %s to %s: %w", in, out, err)
	}

	f, err := os.OpenFile(out, os.O_RDWR, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	size, err := fileSize(f)
	if err != nil {
		return err
	}

	log("Anonymizing the copy...")
	rep, err := anonymize.Reset(f, size)
	if err != nil {
		return fmt.Errorf("anonymize: %w", err)
	}
	log(fmt.Sprintf("%d exFAT partition(s) walked, %d item(s) zeroed, %s total",
		rep.CWSPartitions, len(rep.Items), humansize.Format(rep.BytesZeroed())))
	for _, it := range rep.Items {
		log(fmt.Sprintf("  %-45s %-40s %8s", it.Path, it.What, humansize.Format(it.Size)))
	}
	if rep.FrontGapFound {
		log(fmt.Sprintf("  %-45s %-40s %8s (%d markers seen)",
			"<front gap settings area>", "unit-written state, layout undecoded",
			humansize.Format(rep.FrontGapSize), rep.FrontGapRecords))
	} else {
		log("  front gap not present (image too small) - nothing zeroed there")
	}
	return nil
}

// trimTarget reads the card's partition table and asks trim.Size where the
// data ends. 0 means copy the whole card: trimming is off, or the table
// couldn't be read or parsed.
func trimTarget(d disk.Info, wantTrim bool, log func(string)) int64 {
	if !wantTrim {
		return 0
	}
	header, err := disk.ReadHeader(d)
	if err != nil {
		log("Could not read the partition table, copying the whole card: " + err.Error())
		return 0
	}
	size, err := trim.Size(bytes.NewReader(header))
	if err != nil {
		log("Could not parse the partition table, copying the whole card: " + err.Error())
		return 0
	}
	if size >= d.Size {
		return 0
	}
	return size
}

// backupJob reads d raw into out. target is how much to read; 0 means all of it.
func backupJob(d disk.Info, out string, target int64, onProgress func(int64), log func(string)) (int64, error) {
	log(fmt.Sprintf("Unmounting %s...", d.Identifier))
	if err := disk.Unmount(d.Identifier); err != nil {
		return 0, fmt.Errorf("unmount: %w", err)
	}
	if target > 0 {
		log(fmt.Sprintf("Card is %s; trimming to %s, %s skipped",
			humansize.Format(d.Size), humansize.Format(target), humansize.Format(d.Size-target)))
	} else {
		log(fmt.Sprintf("Card is %s; copying all of it", humansize.Format(d.Size)))
	}
	log(fmt.Sprintf("Reading %s from %s...", d.Identifier, d.Node()))
	if err := disk.ReadRaw(d, out, target, onProgress); err != nil {
		return 0, fmt.Errorf("reading the card: %w", err)
	}
	fi, err := os.Stat(out)
	if err != nil {
		return 0, err
	}
	return fi.Size(), nil
}

func logHash(path string, want bool, log func(string)) error {
	if !want {
		return nil
	}
	log("Computing SHA-256...")
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	log("SHA-256: " + hex.EncodeToString(h.Sum(nil)))
	return nil
}

// patchJob clones in to out and runs the chosen rows in order; out is
// removed again if any row fails, so a half-patched file never sits there.
func patchJob(in, out string, selected []int, log func(string)) error {
	if len(selected) == 0 {
		return fmt.Errorf("no patch selected")
	}
	if _, err := os.Stat(out); err == nil {
		if err := os.Remove(out); err != nil {
			return err
		}
	}
	if err := clone.Copy(in, out); err != nil {
		return fmt.Errorf("copying %s to %s: %w", in, out, err)
	}
	done := false
	defer func() {
		if !done {
			os.Remove(out)
		}
	}()

	f, err := os.OpenFile(out, os.O_RDWR, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	size, err := fileSize(f)
	if err != nil {
		return err
	}

	for _, i := range selected {
		row := patchRows[i]
		log("Applying: " + row.title)
		summary, err := row.apply(f, size, log)
		if err != nil {
			return fmt.Errorf("%s: %w", row.title, err)
		}
		log(summary)
	}
	done = true
	return nil
}
