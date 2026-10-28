package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"time"

	"nissan-nav-tools/anonymize"
	"nissan-nav-tools/consent"
	"nissan-nav-tools/internal/clone"
	"nissan-nav-tools/internal/disk"
	"nissan-nav-tools/internal/humansize"
	"nissan-nav-tools/internal/progress"
	"nissan-nav-tools/trim"
)

type patchRow struct {
	title string
	apply func(f consent.ReadWriterAt, size int64, log func(string)) (string, error)
	plan  func(f io.ReaderAt, size int64, log func(string)) (consent.Result, []consent.PageWrite, error)
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
		plan: consent.PlanConsentSkip,
	},
}

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

func resetCopy(in, out string, log func(string)) error {
	if same, err := clone.SameFile(in, out); err != nil {
		return err
	} else if same {
		return fmt.Errorf("output is the same file as the input; pick a different path")
	}
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

	log("Resetting the copy...")
	rep, err := anonymize.Reset(f, size)
	if err != nil {
		return fmt.Errorf("reset: %w", err)
	}
	log(fmt.Sprintf("%d exFAT partition(s) walked, %d item(s) zeroed, %s total",
		rep.CWSPartitions, len(rep.Items), humansize.Format(rep.BytesZeroed())))
	for _, it := range rep.Items {
		log(fmt.Sprintf("  %-45s %-40s %8s", it.Path, it.What, humansize.Format(it.Size)))
	}
	if rep.CWSPartitions > 0 {
		log(fmt.Sprintf("  %-45s %-40s %8s (%d cluster(s))",
			"<free space on the CWS partition(s)>", "old deleted/rewritten file residue",
			humansize.Format(rep.FreeBytesZeroed), rep.FreeClusters))
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

// openCard and unmountCard are the only points jobs.go touches a card through; tests
// replace them with fakes so each job can be checked for how many times it opens.
var (
	openCard    = disk.OpenRaw
	unmountCard = disk.Unmount
)

func trimTarget(f disk.RawDevice, d disk.Info, wantTrim bool, log func(string)) int64 {
	if !wantTrim {
		return 0
	}
	header, err := disk.ReadHeaderFromHandle(f)
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

// backupJob opens the card once: the same handle supplies the header used to
// size the trim and the bytes that get copied out. onTarget, if not nil, is
// told the real byte count as soon as it's known, so a progress bar sized
// against the whole card up front can be rescaled to the trimmed total.
func backupJob(d disk.Info, out string, wantTrim bool, onTarget func(int64), onProgress func(int64), log func(string)) (int64, error) {
	log(fmt.Sprintf("Unmounting %s...", d.Identifier))
	if err := unmountCard(d.Identifier); err != nil {
		return 0, fmt.Errorf("unmount: %w", err)
	}
	f, err := openCard(d, false)
	if err != nil {
		return 0, fmt.Errorf("opening %s: %w", d.Node(), err)
	}
	defer f.Close()

	target := trimTarget(f, d, wantTrim, log)
	if onTarget != nil {
		if target > 0 {
			onTarget(target)
		} else {
			onTarget(d.Size)
		}
	}
	if target > 0 {
		log(fmt.Sprintf("Card is %s; trimming to %s, %s skipped",
			humansize.Format(d.Size), humansize.Format(target), humansize.Format(d.Size-target)))
	} else {
		log(fmt.Sprintf("Card is %s; copying all of it", humansize.Format(d.Size)))
	}
	log(fmt.Sprintf("Reading %s from %s...", d.Identifier, d.Node()))
	if err := disk.ReadRawFromHandle(f, out, target, onProgress); err != nil {
		return 0, fmt.Errorf("reading the card: %w", err)
	}
	fi, err := os.Stat(out)
	if err != nil {
		return 0, err
	}
	return fi.Size(), nil
}

func logHash(path string, want bool, onProgress func(int64), log func(string)) error {
	if !want {
		return nil
	}
	log("Computing SHA-256...")
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	start := time.Now()
	size, err := fileSize(f)
	if err != nil {
		return err
	}
	h := sha256.New()
	var r io.Reader = f
	if onProgress != nil {
		r = progress.NewCountingReader(f, onProgress)
	}
	if _, err := io.Copy(h, r); err != nil {
		return err
	}
	log("SHA-256: " + hex.EncodeToString(h.Sum(nil)))
	log(progress.Summary(time.Since(start), size))
	return nil
}

func patchInPlaceFile(path string, selected []int, log func(string)) error {
	f, err := os.OpenFile(path, os.O_RDWR, 0o644)
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
	return nil
}

func backupCopy(src, dst string, log func(string)) error {
	if same, err := clone.SameFile(src, dst); err != nil {
		return err
	} else if same {
		return fmt.Errorf("output is the same file as the input; pick a different path")
	}
	log(fmt.Sprintf("Backing up to %s...", dst))
	if _, err := os.Stat(dst); err == nil {
		if err := os.Remove(dst); err != nil {
			return err
		}
	}
	if err := clone.Copy(src, dst); err != nil {
		return fmt.Errorf("copying %s to %s: %w", src, dst, err)
	}
	log("Backup written.")
	return nil
}

func refuseUnlessCard(d disk.Info) error {
	if d.Internal {
		return fmt.Errorf("%s is an internal disk; refusing to patch it", d.Identifier)
	}
	if !d.Removable {
		return fmt.Errorf("%s is not removable media; refusing to patch it", d.Identifier)
	}
	return nil
}

func planCardPatch(r io.ReaderAt, limit int64, selected []int, log func(string)) ([]disk.Range, error) {
	var ranges []disk.Range
	for _, i := range selected {
		row := patchRows[i]
		if row.plan == nil {
			return nil, fmt.Errorf("%s: no in-place card plan for this patch", row.title)
		}
		log("Planning: " + row.title)
		res, writes, err := row.plan(r, limit, log)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", row.title, err)
		}
		lo, hi := res.Target.Offset, res.Target.Offset+int64(res.Target.Size)
		for _, w := range writes {
			if w.Offset < lo || w.Offset+int64(len(w.Data)) > hi {
				return nil, fmt.Errorf("planned write at 0x%x falls outside the nav image (0x%x-0x%x)", w.Offset, lo, hi)
			}
			ranges = append(ranges, disk.Range{Offset: w.Offset, Data: w.Data})
		}
		log(fmt.Sprintf("%s: %d page(s) planned", row.title, len(writes)))
	}
	return ranges, nil
}

func planCardPatchDevice(d disk.Info, selected []int, onProgress func(int64), log func(string)) ([]disk.Range, error) {
	f, err := openCard(d, false)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	limit, err := trim.FirstPartitionStart(f)
	if err != nil {
		return nil, fmt.Errorf("reading the partition table: %w", err)
	}
	log(fmt.Sprintf("Nav image area is the first %s of the card", humansize.Format(limit)))

	start := time.Now()
	var r io.ReaderAt = disk.NewBufferedReaderAt(f)
	if onProgress != nil {
		r = progress.NewCountingReaderAt(r, onProgress)
	}
	ranges, err := planCardPatch(r, limit, selected, log)
	if err != nil {
		return nil, err
	}
	log(progress.Summary(time.Since(start), limit))
	return ranges, nil
}

// Unmounts again right before writing: the card could have remounted while the confirmation dialog was up.
// Opens once: the write and the read-back that verifies it share the same handle.
func applyCardPatch(d disk.Info, ranges []disk.Range, log func(string)) error {
	var total int64
	for _, r := range ranges {
		total += int64(len(r.Data))
	}
	log(fmt.Sprintf("Unmounting %s...", d.Identifier))
	if err := unmountCard(d.Identifier); err != nil {
		return fmt.Errorf("unmount: %w", err)
	}
	f, err := openCard(d, true)
	if err != nil {
		return fmt.Errorf("opening %s: %w", d.Node(), err)
	}
	defer f.Close()

	log(fmt.Sprintf("Writing %d page range(s), %s, to %s...", len(ranges), humansize.Format(total), d.Node()))
	if err := disk.WriteRangesToHandle(f, ranges); err != nil {
		return fmt.Errorf("writing the card: %w", err)
	}
	log("Reading the written ranges back...")
	got, err := disk.ReadRangesFromHandle(f, ranges)
	if err != nil {
		return fmt.Errorf("reading back: %w", err)
	}
	for i, r := range ranges {
		if !bytes.Equal(got[i], r.Data) {
			return fmt.Errorf("read-back mismatch at 0x%x", r.Offset)
		}
	}
	log(fmt.Sprintf("Read-back matches: %d page range(s), %s", len(ranges), humansize.Format(total)))
	return nil
}

func patchJob(in, out string, selected []int, onCopyProgress func(int64), log func(string)) error {
	if len(selected) == 0 {
		return fmt.Errorf("no patch selected")
	}
	if same, err := clone.SameFile(in, out); err != nil {
		return err
	} else if same {
		return fmt.Errorf("output is the same file as the input; pick a different path")
	}
	if _, err := os.Stat(out); err == nil {
		if err := os.Remove(out); err != nil {
			return err
		}
	}
	copyStart := time.Now()
	if err := clone.CopyWithProgress(in, out, onCopyProgress); err != nil {
		return fmt.Errorf("copying %s to %s: %w", in, out, err)
	}
	if fi, err := os.Stat(out); err == nil {
		log(progress.Summary(time.Since(copyStart), fi.Size()))
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
