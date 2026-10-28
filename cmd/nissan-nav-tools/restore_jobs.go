package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"

	"nissan-nav-tools/internal/disk"
	"nissan-nav-tools/internal/humansize"
)

func refuseIfTooSmall(d disk.Info, imgSize int64) error {
	if d.Size < imgSize {
		return fmt.Errorf("%s (%s) is smaller than the image (%s)", d.Identifier, humansize.Format(d.Size), humansize.Format(imgSize))
	}
	return nil
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// restoreJob writes imgPath onto d and returns the still-open handle, so a verify
// right after can read the card back through it rather than through a fresh open
// (which on macOS would see whatever Spotlight just wrote to the newly-mounted
// partition, and on Windows would have lost the dismount locks).
func restoreJob(d disk.Info, imgPath string, onProgress func(int64), log func(string)) (disk.RawDevice, error) {
	log(fmt.Sprintf("Unmounting %s...", d.Identifier))
	if err := unmountCard(d.Identifier); err != nil {
		return nil, fmt.Errorf("unmount: %w", err)
	}

	src, err := os.Open(imgPath)
	if err != nil {
		return nil, err
	}
	defer src.Close()

	log(fmt.Sprintf("Writing %s to %s...", imgPath, d.Node()))
	f, err := openCard(d, true)
	if err != nil {
		return nil, fmt.Errorf("opening the card: %w", err)
	}
	if err := disk.WriteToHandle(f, src, onProgress); err != nil {
		f.Close()
		return nil, fmt.Errorf("writing the card: %w", err)
	}
	return f, nil
}

func restoreSequence(write, verify func() error, verifyWanted bool) error {
	if err := write(); err != nil {
		return err
	}
	if verifyWanted {
		return verify()
	}
	return nil
}

// verifyRestore reads back through f, the handle restoreJob wrote with and left open.
func verifyRestore(f disk.RawDevice, imgPath string, onProgress func(int64), log func(string)) error {
	fi, err := os.Stat(imgPath)
	if err != nil {
		return err
	}
	wantHash, err := fileSHA256(imgPath)
	if err != nil {
		return err
	}

	log("Reading back...")
	gotHash, err := disk.HashHandle(f, fi.Size(), onProgress)
	if err != nil {
		return fmt.Errorf("reading the card back: %w", err)
	}
	if gotHash != wantHash {
		return fmt.Errorf("verify failed: card sha256 %s, image sha256 %s", gotHash, wantHash)
	}
	log("Verified: card matches image, sha256 " + gotHash)
	return nil
}
