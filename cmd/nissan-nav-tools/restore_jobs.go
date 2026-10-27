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

func restoreJob(d disk.Info, imgPath string, onProgress func(int64), log func(string)) error {
	log(fmt.Sprintf("Unmounting %s...", d.Identifier))
	if err := disk.Unmount(d.Identifier); err != nil {
		return fmt.Errorf("unmount: %w", err)
	}
	log(fmt.Sprintf("Writing %s to %s...", imgPath, d.Node()))
	if err := disk.WriteRaw(d, imgPath, onProgress); err != nil {
		return fmt.Errorf("writing the card: %w", err)
	}
	return nil
}

// ejectCard is its own step: ejecting before a read-back verify can pull the device node that verify needs.
func ejectCard(id string, log func(string)) error {
	log(fmt.Sprintf("Ejecting %s...", id))
	if err := disk.Eject(id); err != nil {
		return fmt.Errorf("eject: %w", err)
	}
	return nil
}

// restoreSequence runs write, then verify if asked, then eject; a failed write or verify skips the eject.
func restoreSequence(write, verify func() error, verifyWanted bool, eject func() error) error {
	if err := write(); err != nil {
		return err
	}
	if verifyWanted {
		if err := verify(); err != nil {
			return err
		}
	}
	return eject()
}

func verifyRestore(d disk.Info, imgPath string, onProgress func(int64), log func(string)) error {
	fi, err := os.Stat(imgPath)
	if err != nil {
		return err
	}
	wantHash, err := fileSHA256(imgPath)
	if err != nil {
		return err
	}

	log("Reading back...")
	gotHash, err := disk.HashRaw(d, fi.Size(), onProgress)
	if err != nil {
		return fmt.Errorf("reading the card back: %w", err)
	}
	if gotHash != wantHash {
		return fmt.Errorf("verify failed: card sha256 %s, image sha256 %s", gotHash, wantHash)
	}
	log("Verified: card matches image, sha256 " + gotHash)
	return nil
}
