// Package disk lists removable storage and reads it raw, macOS only for now.
package disk

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Info is one whole disk diskutil reported as external and physical.
type Info struct {
	Identifier string // e.g. "disk4"
	Name       string // diskutil's MediaName, blank if it didn't give one
	Size       int64
	Internal   bool
	Removable  bool // diskutil's RemovableMedia; false for most external enclosures, true for a card reader's slot
}

const sectorBytes = 512

type Range struct {
	Offset int64
	Data   []byte
}

// Node is the raw character device to read the disk through on macOS.
func (d Info) Node() string { return "/dev/r" + d.Identifier }

var errUnsupported = fmt.Errorf("reading a card by device is not supported on %s yet", runtime.GOOS)

// List drops any internal disk even if diskutil's own filter let it through.
func List() ([]Info, error) {
	if runtime.GOOS != "darwin" {
		return nil, errUnsupported
	}
	xml, err := exec.Command("diskutil", "list", "-plist", "external", "physical").Output()
	if err != nil {
		return nil, fmt.Errorf("diskutil list: %w", err)
	}
	js, err := toJSON(xml)
	if err != nil {
		return nil, err
	}
	ids, err := parseWholeDisks(js)
	if err != nil {
		return nil, err
	}

	var out []Info
	for _, id := range ids {
		info, err := infoFor(id)
		if err != nil {
			continue // a disk that vanished between the two calls, not an error worth failing the list over
		}
		if info.Internal {
			continue
		}
		out = append(out, info)
	}
	return out, nil
}

func infoFor(id string) (Info, error) {
	xml, err := exec.Command("diskutil", "info", "-plist", id).Output()
	if err != nil {
		return Info{}, fmt.Errorf("diskutil info %s: %w", id, err)
	}
	js, err := toJSON(xml)
	if err != nil {
		return Info{}, err
	}
	return parseInfo(js)
}

// toJSON runs an XML plist through plutil, so this needs no plist library.
func toJSON(xmlPlist []byte) ([]byte, error) {
	cmd := exec.Command("plutil", "-convert", "json", "-o", "-", "-")
	cmd.Stdin = bytes.NewReader(xmlPlist)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("plutil: %w", err)
	}
	return out, nil
}

func parseWholeDisks(js []byte) ([]string, error) {
	var v struct {
		WholeDisks []string `json:"WholeDisks"`
	}
	if err := json.Unmarshal(js, &v); err != nil {
		return nil, fmt.Errorf("parsing diskutil list output: %w", err)
	}
	return v.WholeDisks, nil
}

func parseInfo(js []byte) (Info, error) {
	var v struct {
		DeviceIdentifier string `json:"DeviceIdentifier"`
		MediaName        string `json:"MediaName"`
		Size             int64  `json:"Size"`
		Internal         bool   `json:"Internal"`
		RemovableMedia   bool   `json:"RemovableMedia"`
	}
	if err := json.Unmarshal(js, &v); err != nil {
		return Info{}, fmt.Errorf("parsing diskutil info output: %w", err)
	}
	if v.DeviceIdentifier == "" {
		return Info{}, fmt.Errorf("diskutil info did not name a device")
	}
	return Info{Identifier: v.DeviceIdentifier, Name: v.MediaName, Size: v.Size, Internal: v.Internal, Removable: v.RemovableMedia}, nil
}

// refuseUnlessCard rejects any disk that is not external and removable.
func refuseUnlessCard(id Info) error {
	if id.Internal {
		return fmt.Errorf("refusing %s: it is an internal disk", id.Identifier)
	}
	if !id.Removable {
		return fmt.Errorf("refusing %s: it is not removable media", id.Identifier)
	}
	return nil
}

// Unmount clears the buffer cache so nothing else writes under a raw read.
func Unmount(id string) error {
	if runtime.GOOS != "darwin" {
		return errUnsupported
	}
	out, err := exec.Command("diskutil", "unmountDisk", id).CombinedOutput()
	if err != nil {
		return fmt.Errorf("diskutil unmountDisk %s: %w: %s", id, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// ReadHeader reads the raw device's first two sectors, all a trim size needs.
func ReadHeader(id Info) ([]byte, error) {
	if runtime.GOOS != "darwin" {
		return nil, errUnsupported
	}
	if id.Internal {
		return nil, fmt.Errorf("refusing to read %s: it is an internal disk", id.Identifier)
	}
	script := fmt.Sprintf(
		`do shell script "/bin/dd if=%s bs=512 count=2 2>/dev/null | /usr/bin/base64" with administrator privileges`,
		appleScriptQuote(id.Node()))
	out, err := exec.Command("osascript", "-e", script).Output()
	if err != nil {
		return nil, fmt.Errorf("reading the partition table: %w", err)
	}
	data, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(out)))
	if err != nil {
		return nil, fmt.Errorf("decoding the partition table: %w", err)
	}
	if len(data) < 1024 {
		return nil, fmt.Errorf("got %d bytes off the device, want 1024", len(data))
	}
	return data, nil
}

// bestBlockSize picks the largest block size that divides n evenly, so a
// size-limited read still moves in big chunks.
func bestBlockSize(n int64) int64 {
	for _, bs := range []int64{16 << 20, 1 << 20, 64 << 10, 4096, 512} {
		if n%bs == 0 {
			return bs
		}
	}
	return 512
}

// ReadRaw copies id's raw device to outPath with one privileged, read-only
// dd. maxBytes, if positive, stops the copy early. onProgress, if set, is
// polled off outPath's growing size from another goroutine.
func ReadRaw(id Info, outPath string, maxBytes int64, onProgress func(written int64)) error {
	if runtime.GOOS != "darwin" {
		return errUnsupported
	}
	if id.Internal {
		return fmt.Errorf("refusing to read %s: it is an internal disk", id.Identifier)
	}

	ddArgs := "bs=16m"
	if maxBytes > 0 {
		bs := bestBlockSize(maxBytes)
		ddArgs = fmt.Sprintf("bs=%d count=%d", bs, maxBytes/bs)
	}
	script := fmt.Sprintf(
		`do shell script "/bin/dd if=%s of=%s %s" with administrator privileges`,
		appleScriptQuote(id.Node()), appleScriptQuote(outPath), ddArgs)
	cmd := exec.Command("osascript", "-e", script)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting the privileged read: %w", err)
	}

	stop := make(chan struct{})
	if onProgress != nil {
		go func() {
			t := time.NewTicker(500 * time.Millisecond)
			defer t.Stop()
			for {
				select {
				case <-t.C:
					if fi, err := os.Stat(outPath); err == nil {
						onProgress(fi.Size())
					}
				case <-stop:
					return
				}
			}
		}()
	}
	err := cmd.Wait()
	close(stop)
	if err != nil {
		return fmt.Errorf("dd: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// appleScriptQuote applies the shell's own quoting, then escapes that again for AppleScript's string literal.
func appleScriptQuote(s string) string {
	shq := "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
	return strings.ReplaceAll(strings.ReplaceAll(shq, `\`, `\\`), `"`, `\"`)
}

// WriteRanges writes to id's raw device in one privileged step, via one admin-authorized script.
func WriteRanges(id Info, ranges []Range) error {
	if runtime.GOOS != "darwin" {
		return errUnsupported
	}
	if err := refuseUnlessCard(id); err != nil {
		return err
	}
	if len(ranges) == 0 {
		return nil
	}

	dir, err := os.MkdirTemp("", "nnt-patch-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)

	dataPath := filepath.Join(dir, "data.bin")
	df, err := os.Create(dataPath)
	if err != nil {
		return err
	}
	var script strings.Builder
	script.WriteString("set -e\n")
	var tmpOff int64
	for _, r := range ranges {
		if r.Offset%sectorBytes != 0 || int64(len(r.Data))%sectorBytes != 0 {
			df.Close()
			return fmt.Errorf("range at 0x%x, %d bytes, is not sector-aligned", r.Offset, len(r.Data))
		}
		if _, err := df.Write(r.Data); err != nil {
			df.Close()
			return err
		}
		fmt.Fprintf(&script, "/bin/dd if=%s of=%s bs=%d skip=%d seek=%d count=%d conv=notrunc\n",
			dataPath, id.Node(), sectorBytes, tmpOff/sectorBytes, r.Offset/sectorBytes, int64(len(r.Data))/sectorBytes)
		tmpOff += int64(len(r.Data))
	}
	if err := df.Close(); err != nil {
		return err
	}
	scriptPath := filepath.Join(dir, "write.sh")
	if err := os.WriteFile(scriptPath, []byte(script.String()), 0o700); err != nil {
		return err
	}

	ascript := fmt.Sprintf(`do shell script "/bin/bash %s" with administrator privileges`, appleScriptQuote(scriptPath))
	out, err := exec.Command("osascript", "-e", ascript).CombinedOutput()
	if err != nil {
		return fmt.Errorf("writing the card: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// ReadRanges reads the same ranges back in one privileged step, for comparison.
func ReadRanges(id Info, ranges []Range) ([][]byte, error) {
	if runtime.GOOS != "darwin" {
		return nil, errUnsupported
	}
	if err := refuseUnlessCard(id); err != nil {
		return nil, err
	}
	if len(ranges) == 0 {
		return nil, nil
	}

	dir, err := os.MkdirTemp("", "nnt-readback-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	outPath := filepath.Join(dir, "readback.bin")

	var script strings.Builder
	script.WriteString("set -e\n")
	for _, r := range ranges {
		if r.Offset%sectorBytes != 0 || int64(len(r.Data))%sectorBytes != 0 {
			return nil, fmt.Errorf("range at 0x%x, %d bytes, is not sector-aligned", r.Offset, len(r.Data))
		}
		fmt.Fprintf(&script, "/bin/dd if=%s bs=%d skip=%d count=%d >> %s\n",
			id.Node(), sectorBytes, r.Offset/sectorBytes, int64(len(r.Data))/sectorBytes, outPath)
	}
	scriptPath := filepath.Join(dir, "read.sh")
	if err := os.WriteFile(scriptPath, []byte(script.String()), 0o700); err != nil {
		return nil, err
	}

	ascript := fmt.Sprintf(`do shell script "/bin/bash %s" with administrator privileges`, appleScriptQuote(scriptPath))
	out, err := exec.Command("osascript", "-e", ascript).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("reading the card back: %w: %s", err, strings.TrimSpace(string(out)))
	}

	got, err := os.ReadFile(outPath)
	if err != nil {
		return nil, fmt.Errorf("reading the read-back file: %w", err)
	}
	result := make([][]byte, len(ranges))
	var pos int
	for i, r := range ranges {
		n := len(r.Data)
		if pos+n > len(got) {
			return nil, fmt.Errorf("read-back is short: got %d bytes, wanted at least %d", len(got), pos+n)
		}
		result[i] = got[pos : pos+n]
		pos += n
	}
	return result, nil
}
