package main

import (
	"fmt"
	"os"

	"nissan-nav-tools/anonymize"
	"nissan-nav-tools/internal/disk"
	"nissan-nav-tools/internal/humansize"
)

func logResetReport(rep anonymize.Report, log func(string)) {
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
}

func resetInPlaceFile(path string, log func(string)) (anonymize.Report, error) {
	var rep anonymize.Report
	f, err := os.OpenFile(path, os.O_RDWR, 0o644)
	if err != nil {
		return rep, err
	}
	defer f.Close()
	size, err := fileSize(f)
	if err != nil {
		return rep, err
	}
	log("Resetting...")
	return anonymize.Reset(f, size)
}

func toWindows(ws []anonymize.Window) []disk.Window {
	out := make([]disk.Window, len(ws))
	for i, w := range ws {
		out[i] = disk.Window{Offset: w.Offset, Size: w.Size}
	}
	return out
}

// resetCardInPlace unmounts the card, resets it directly through its raw device handle, and
// reads every write back through the same handle before returning. The write path refuses
// anything outside the front gap and the CWS exFAT partition.
func resetCardInPlace(d disk.Info, log func(string)) (anonymize.Report, error) {
	var rep anonymize.Report
	log(fmt.Sprintf("Unmounting %s...", d.Identifier))
	if err := unmountCard(d.Identifier); err != nil {
		return rep, fmt.Errorf("unmount: %w", err)
	}
	f, err := openCard(d, true)
	if err != nil {
		return rep, fmt.Errorf("opening %s: %w", d.Node(), err)
	}
	defer f.Close()

	windows, err := anonymize.Bounds(f, d.Size)
	if err != nil {
		return rep, err
	}

	aligned := disk.NewAlignedReadWriterAt(f)
	guarded := disk.NewRestrictedReadWriterAt(aligned, toWindows(windows))

	log("Resetting the card...")
	rep, err = anonymize.Reset(guarded, d.Size)
	if err != nil {
		return rep, fmt.Errorf("reset: %w", err)
	}

	log("Reading the written ranges back...")
	if err := aligned.Verify(); err != nil {
		return rep, fmt.Errorf("reading back: %w", err)
	}
	log("Read-back matches.")
	return rep, nil
}
