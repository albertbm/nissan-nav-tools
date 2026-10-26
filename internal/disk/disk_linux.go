//go:build linux

package disk

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

const sysBlockDir = "/sys/block"

func platformNode(d Info) string { return "/dev/" + d.Identifier }

func platformList() ([]Info, error) {
	return parseSysBlock(sysBlockDir)
}

func skipBlockDevice(name string) bool {
	switch {
	case strings.HasPrefix(name, "loop"), strings.HasPrefix(name, "ram"),
		strings.HasPrefix(name, "zram"), strings.HasPrefix(name, "dm-"), strings.HasPrefix(name, "md"):
		return true
	}
	return false
}

func parseSysBlock(root string) ([]Info, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", root, err)
	}
	var out []Info
	for _, e := range entries {
		if skipBlockDevice(e.Name()) {
			continue
		}
		if info, ok := sysBlockInfo(root, e.Name()); ok {
			out = append(out, info)
		}
	}
	return out, nil
}

func sysBlockInfo(root, name string) (Info, bool) {
	dir := filepath.Join(root, name)
	sectors, err := readUintFile(filepath.Join(dir, "size"))
	if err != nil {
		return Info{}, false
	}

	// mmcblk card readers never set the removable sysfs flag, so treat the prefix as always removable.
	isMMC := strings.HasPrefix(name, "mmcblk")
	removable := readUintFileDefault(filepath.Join(dir, "removable"), 0) == 1
	usb := isUSBDevice(dir)

	var deviceName string
	if isMMC {
		deviceName = readTrimmed(filepath.Join(dir, "device", "name"))
	} else {
		vendor := readTrimmed(filepath.Join(dir, "device", "vendor"))
		model := readTrimmed(filepath.Join(dir, "device", "model"))
		deviceName = strings.TrimSpace(vendor + " " + model)
	}

	external := removable || usb || isMMC
	return Info{
		Identifier: name,
		Name:       deviceName,
		Size:       int64(sectors) * sectorBytes,
		Internal:   !external,
		Removable:  external,
	}, true
}

// The device symlink target contains "/usb" only when the block device hangs off a USB bus.
func isUSBDevice(dir string) bool {
	target, err := os.Readlink(filepath.Join(dir, "device"))
	if err != nil {
		return false
	}
	return strings.Contains(target, "/usb")
}

func readUintFile(path string) (uint64, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64)
}

func readUintFileDefault(path string, def uint64) uint64 {
	v, err := readUintFile(path)
	if err != nil {
		return def
	}
	return v
}

func readTrimmed(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

type mountedPartition struct {
	device     string
	mountPoint string
}

func isPartitionOf(name, disk string) bool {
	if name == disk {
		return true
	}
	if !strings.HasPrefix(name, disk) {
		return false
	}
	rest := strings.TrimPrefix(name[len(disk):], "p")
	if rest == "" {
		return false
	}
	for _, r := range rest {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// /proc/mounts octal-escapes spaces, tabs, newlines and backslashes in paths; this undoes that.
var mountEscaper = strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`)

func parseMountedPartitions(data []byte, disk string) []mountedPartition {
	var out []mountedPartition
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 2 {
			continue
		}
		dev := fields[0]
		if isPartitionOf(filepath.Base(dev), disk) {
			out = append(out, mountedPartition{device: dev, mountPoint: mountEscaper.Replace(fields[1])})
		}
	}
	return out
}

func platformUnmount(id string) error {
	data, err := os.ReadFile("/proc/mounts")
	if err != nil {
		return fmt.Errorf("reading /proc/mounts: %w", err)
	}

	var failed []string
	for _, m := range parseMountedPartitions(data, id) {
		if err := unix.Unmount(m.mountPoint, 0); err == nil {
			continue
		}
		// unix.Unmount needs root; udisksctl unmounts the same device unprivileged, via polkit.
		if out, err := exec.Command("udisksctl", "unmount", "-b", m.device).CombinedOutput(); err != nil {
			failed = append(failed, fmt.Sprintf("%s: %s", m.device, strings.TrimSpace(string(out))))
		}
	}
	if len(failed) > 0 {
		return fmt.Errorf("unmounting %s: %s", id, strings.Join(failed, "; "))
	}
	return nil
}

func platformEject(id string) error {
	node := "/dev/" + id
	if _, err := exec.LookPath("udisksctl"); err == nil {
		exec.Command("udisksctl", "power-off", "-b", node).Run()
		return nil
	}
	exec.Command("eject", node).Run()
	return nil
}

func platformOpen(id Info, write bool) (*os.File, error) {
	flags := os.O_RDONLY
	if write {
		flags = os.O_RDWR | os.O_SYNC
	}
	f, err := os.OpenFile(id.Node(), flags, 0)
	if err != nil {
		if os.IsPermission(err) {
			return nil, fmt.Errorf("opening %s: permission denied; run as root, or let this app relaunch via pkexec", id.Node())
		}
		return nil, fmt.Errorf("opening %s: %w", id.Node(), err)
	}
	return f, nil
}
