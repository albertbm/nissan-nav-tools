//go:build darwin

package disk

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

func platformNode(d Info) string { return "/dev/r" + d.Identifier }

func platformList() ([]Info, error) {
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

func platformUnmount(id string) error {
	out, err := exec.Command("diskutil", "unmountDisk", id).CombinedOutput()
	if err != nil {
		return fmt.Errorf("diskutil unmountDisk %s: %w: %s", id, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func platformEject(id string) error {
	out, err := exec.Command("diskutil", "eject", id).CombinedOutput()
	if err != nil {
		return fmt.Errorf("diskutil eject %s: %w: %s", id, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// platformOpen goes through authopen, so Touch ID or a watch can clear the prompt instead of a typed password; no root shell ever touches the file.
func platformOpen(id Info, write bool) (*os.File, error) {
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

func platformNotes() []string { return nil }
