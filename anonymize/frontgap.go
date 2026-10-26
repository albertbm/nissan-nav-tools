package anonymize

import "bytes"

// Offsets are specific to the ZE0/ZE1/ME0 cards this project has.
const (
	frontGapOffset = 413196288
	frontGapSize   = 415088640 - 413196288
)

// resetFrontGap zeros the front gap wholesale; its layout past the SRST/SRED header isn't decoded.
func resetFrontGap(f ReadWriterAt, imageSize int64, rep *Report) error {
	if imageSize < frontGapOffset+frontGapSize {
		return nil
	}
	gap := make([]byte, frontGapSize)
	if _, err := f.ReadAt(gap, frontGapOffset); err != nil {
		return err
	}
	rep.FrontGapFound = true
	rep.FrontGapOffset = frontGapOffset
	rep.FrontGapSize = frontGapSize
	rep.FrontGapRecords = bytes.Count(gap, []byte("SRED"))

	blank := make([]byte, frontGapSize)
	if _, err := f.WriteAt(blank, frontGapOffset); err != nil {
		return err
	}
	return nil
}
