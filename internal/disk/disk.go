// Package disk lists removable storage and reads it raw, macOS only for now.
package disk

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
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

// A multiple of sectorBytes: only the final, short chunk of a device write ever needs padding.
const chunkSize = 4 << 20

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
		if !isCard(info) {
			continue
		}
		out = append(out, info)
	}
	return out, nil
}

// Head unit cards top out at 64 GB; this keeps external drives off the list.
const maxCardSize = 130_000_000_000

func isCard(d Info) bool {
	return !d.Internal && d.Removable && d.Size > 0 && d.Size <= maxCardSize
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

func Eject(id string) error {
	if runtime.GOOS != "darwin" {
		return errUnsupported
	}
	out, err := exec.Command("diskutil", "eject", id).CombinedOutput()
	if err != nil {
		return fmt.Errorf("diskutil eject %s: %w: %s", id, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// OpenRaw goes through authopen, so Touch ID or a watch can clear the prompt instead of a typed password; no root shell ever touches the file.
func OpenRaw(id Info, write bool) (*os.File, error) {
	if runtime.GOOS != "darwin" {
		return nil, errUnsupported
	}
	if err := refuseUnlessCard(id); err != nil {
		return nil, err
	}

	args := []string{"-stdoutpipe"}
	if write {
		args = append(args, "-o", strconv.Itoa(os.O_RDWR))
	}
	args = append(args, id.Node())

	fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM, 0)
	if err != nil {
		return nil, fmt.Errorf("socketpair: %w", err)
	}
	defer unix.Close(fds[0])
	childEnd := os.NewFile(uintptr(fds[1]), "authopen-stdout")

	cmd := exec.Command("/usr/libexec/authopen", args...)
	cmd.Stdout = childEnd
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	startErr := cmd.Start()
	childEnd.Close()
	if startErr != nil {
		return nil, fmt.Errorf("starting authopen: %w", startErr)
	}

	f, recvErr := recvFD(fds[0], id.Node())
	waitErr := cmd.Wait()

	if recvErr != nil || f == nil {
		detail := strings.TrimSpace(stderr.String())
		switch {
		case waitErr != nil && detail != "":
			return nil, fmt.Errorf("authopen on %s: %w: %s", id.Node(), waitErr, detail)
		case waitErr != nil:
			return nil, fmt.Errorf("authopen on %s: %w (denied, cancelled, or this app needs Full Disk Access)", id.Node(), waitErr)
		default:
			return nil, fmt.Errorf("authopen on %s gave no file descriptor; check Full Disk Access for this app", id.Node())
		}
	}
	return f, nil
}

func recvFD(sock int, name string) (*os.File, error) {
	buf := make([]byte, 4)
	oob := make([]byte, unix.CmsgSpace(4))
	_, oobn, _, _, err := unix.Recvmsg(sock, buf, oob, 0)
	if err != nil {
		return nil, fmt.Errorf("receiving the file descriptor: %w", err)
	}
	if oobn == 0 {
		return nil, fmt.Errorf("authopen passed no file descriptor")
	}
	scms, err := unix.ParseSocketControlMessage(oob[:oobn])
	if err != nil {
		return nil, fmt.Errorf("parsing the control message: %w", err)
	}
	if len(scms) == 0 {
		return nil, fmt.Errorf("authopen's message carried no control data")
	}
	rights, err := unix.ParseUnixRights(&scms[0])
	if err != nil {
		return nil, fmt.Errorf("parsing the passed rights: %w", err)
	}
	if len(rights) == 0 {
		return nil, fmt.Errorf("authopen passed no rights")
	}
	return os.NewFile(uintptr(rights[0]), name), nil
}

func padToSector(buf []byte) []byte {
	r := len(buf) % sectorBytes
	if r == 0 {
		return buf
	}
	padded := make([]byte, len(buf)+sectorBytes-r)
	copy(padded, buf)
	return padded
}

func copyChunks(dst io.Writer, src io.Reader, onProgress func(int64), padWrites bool) error {
	buf := make([]byte, chunkSize)
	var total int64
	for {
		n, err := io.ReadFull(src, buf)
		if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
			return fmt.Errorf("reading: %w", err)
		}
		if n > 0 {
			chunk := buf[:n]
			if padWrites {
				chunk = padToSector(chunk)
			}
			if _, werr := dst.Write(chunk); werr != nil {
				return fmt.Errorf("writing: %w", werr)
			}
			total += int64(n)
			if onProgress != nil {
				onProgress(total)
			}
		}
		if err == io.ErrUnexpectedEOF || err == io.EOF {
			return nil
		}
	}
}

func ReadHeader(id Info) ([]byte, error) {
	f, err := OpenRaw(id, false)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data := make([]byte, 1024)
	if _, err := io.ReadFull(f, data); err != nil {
		return nil, fmt.Errorf("reading the partition table: %w", err)
	}
	return data, nil
}

func ReadRaw(id Info, outPath string, maxBytes int64, onProgress func(written int64)) error {
	src, err := OpenRaw(id, false)
	if err != nil {
		return err
	}
	defer src.Close()

	out, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer out.Close()

	var r io.Reader = src
	if maxBytes > 0 {
		r = io.LimitReader(src, maxBytes)
	}
	return copyChunks(out, r, onProgress, false)
}

func WriteRaw(id Info, srcPath string, onProgress func(written int64)) error {
	src, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer src.Close()

	dst, err := OpenRaw(id, true)
	if err != nil {
		return err
	}
	defer dst.Close()

	return copyChunks(dst, src, onProgress, true)
}

func checkRangesAligned(ranges []Range) error {
	for _, r := range ranges {
		if r.Offset%sectorBytes != 0 || int64(len(r.Data))%sectorBytes != 0 {
			return fmt.Errorf("range at 0x%x, %d bytes, is not sector-aligned", r.Offset, len(r.Data))
		}
	}
	return nil
}

func WriteRanges(id Info, ranges []Range) error {
	if len(ranges) == 0 {
		return nil
	}
	if err := checkRangesAligned(ranges); err != nil {
		return err
	}

	f, err := OpenRaw(id, true)
	if err != nil {
		return err
	}
	defer f.Close()

	for _, r := range ranges {
		if _, err := f.WriteAt(r.Data, r.Offset); err != nil {
			return fmt.Errorf("writing the card at 0x%x: %w", r.Offset, err)
		}
	}
	return nil
}

func ReadRanges(id Info, ranges []Range) ([][]byte, error) {
	if len(ranges) == 0 {
		return nil, nil
	}
	if err := checkRangesAligned(ranges); err != nil {
		return nil, err
	}

	f, err := OpenRaw(id, false)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	result := make([][]byte, len(ranges))
	for i, r := range ranges {
		buf := make([]byte, len(r.Data))
		if _, err := f.ReadAt(buf, r.Offset); err != nil {
			return nil, fmt.Errorf("reading the card back at 0x%x: %w", r.Offset, err)
		}
		result[i] = buf
	}
	return result, nil
}
