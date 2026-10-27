//go:build linux

package disk

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func fakeSysBlockDisk(t *testing.T, root, name string, sectors uint64, removable bool, usbSymlink string, vendor, model, mmcName string) {
	t.Helper()
	dir := filepath.Join(root, name)
	writeFile(t, filepath.Join(dir, "size"), itoa(sectors))
	r := "0"
	if removable {
		r = "1"
	}
	writeFile(t, filepath.Join(dir, "removable"), r)
	if usbSymlink != "" {
		target := filepath.Join(root, "sys", strings.TrimLeft(usbSymlink, "./"))
		if err := os.MkdirAll(target, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, filepath.Join(dir, "device")); err != nil {
			t.Fatal(err)
		}
	}
	if mmcName != "" {
		writeFile(t, filepath.Join(dir, "device", "name"), mmcName)
	} else if vendor != "" || model != "" {
		writeFile(t, filepath.Join(dir, "device", "vendor"), vendor)
		writeFile(t, filepath.Join(dir, "device", "model"), model)
	}
}

func itoa(v uint64) string {
	return strconv.FormatUint(v, 10)
}

func TestSysBlockInfoBuiltInDiskIsInternal(t *testing.T) {
	root := t.TempDir()
	fakeSysBlockDisk(t, root, "sda", 1_000_000_000, false, "", "ATA", "Samsung SSD 860", "")

	info, ok := sysBlockInfo(root, "sda")
	if !ok {
		t.Fatal("sysBlockInfo reported sda as unreadable")
	}
	if !info.Internal || info.Removable {
		t.Errorf("got Internal=%v Removable=%v, want an internal disk", info.Internal, info.Removable)
	}
	if info.Name != "ATA Samsung SSD 860" {
		t.Errorf("Name = %q, want %q", info.Name, "ATA Samsung SSD 860")
	}
	if info.Size != 1_000_000_000*sectorBytes {
		t.Errorf("Size = %d, want %d", info.Size, 1_000_000_000*sectorBytes)
	}
}

func TestSysBlockInfoRemovableFlagMakesACard(t *testing.T) {
	root := t.TempDir()
	fakeSysBlockDisk(t, root, "sdb", 125_045_424, true, "", "Generic", "SD Card Reader", "")

	info, ok := sysBlockInfo(root, "sdb")
	if !ok {
		t.Fatal("sysBlockInfo reported sdb as unreadable")
	}
	if info.Internal || !info.Removable {
		t.Errorf("got Internal=%v Removable=%v, want a removable card", info.Internal, info.Removable)
	}
}

func TestSysBlockInfoUSBBusMakesACardEvenWithoutTheRemovableFlag(t *testing.T) {
	root := t.TempDir()
	fakeSysBlockDisk(t, root, "sdc", 64_000_000, false,
		"../../devices/pci0000:00/0000:00:14.0/usb1/1-1/1-1:1.0/host3/target3:0:0/3:0:0:0/block/sdc",
		"Generic", "USB Reader", "")

	info, ok := sysBlockInfo(root, "sdc")
	if !ok {
		t.Fatal("sysBlockInfo reported sdc as unreadable")
	}
	if !info.Removable {
		t.Error("a device on the usb bus should count as removable even if removable=0")
	}
}

func TestSysBlockInfoMMCIsRemovableAndUsesDeviceName(t *testing.T) {
	root := t.TempDir()
	fakeSysBlockDisk(t, root, "mmcblk0", 30_000_000, false, "", "", "", "SD32G")

	info, ok := sysBlockInfo(root, "mmcblk0")
	if !ok {
		t.Fatal("sysBlockInfo reported mmcblk0 as unreadable")
	}
	if !info.Removable {
		t.Error("mmcblk* should always be treated as removable")
	}
	if info.Name != "SD32G" {
		t.Errorf("Name = %q, want %q", info.Name, "SD32G")
	}
}

func TestSysBlockInfoMissingSizeIsSkipped(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "sdz"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, ok := sysBlockInfo(root, "sdz"); ok {
		t.Error("a device with no size file should not parse")
	}
}

func TestParseSysBlockSkipsPseudoDevices(t *testing.T) {
	root := t.TempDir()
	fakeSysBlockDisk(t, root, "sda", 1_000_000, false, "", "ATA", "Disk", "")
	for _, name := range []string{"loop0", "ram0", "zram0", "dm-0", "md0"} {
		writeFile(t, filepath.Join(root, name, "size"), "1000")
	}

	infos, err := parseSysBlock(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(infos) != 1 || infos[0].Identifier != "sda" {
		t.Errorf("got %+v, want only sda", infos)
	}
}

func TestIsPartitionOf(t *testing.T) {
	cases := []struct {
		name, disk string
		want       bool
	}{
		{"sda", "sda", true},
		{"sda1", "sda", true},
		{"sda10", "sda", true},
		{"sdb1", "sda", false},
		{"sdaa1", "sda", false},
		{"mmcblk0", "mmcblk0", true},
		{"mmcblk0p1", "mmcblk0", true},
		{"mmcblk1p1", "mmcblk0", false},
	}
	for _, c := range cases {
		if got := isPartitionOf(c.name, c.disk); got != c.want {
			t.Errorf("isPartitionOf(%q, %q) = %v, want %v", c.name, c.disk, got, c.want)
		}
	}
}

func TestParseMountedPartitionsMatchesDiskAndItsPartitions(t *testing.T) {
	data := []byte(
		"/dev/sda1 / ext4 rw,relatime 0 0\n" +
			"/dev/sdb1 /media/user/CARD vfat rw,relatime 0 0\n" +
			"/dev/mmcblk0p1 /media/user/SD\\040CARD vfat rw 0 0\n" +
			"tmpfs /run tmpfs rw 0 0\n",
	)

	got := parseMountedPartitions(data, "sdb")
	if len(got) != 1 || got[0].device != "/dev/sdb1" || got[0].mountPoint != "/media/user/CARD" {
		t.Errorf("got %+v, want one mount of /dev/sdb1 at /media/user/CARD", got)
	}

	got = parseMountedPartitions(data, "mmcblk0")
	if len(got) != 1 || got[0].mountPoint != "/media/user/SD CARD" {
		t.Errorf("got %+v, want the escaped space unescaped", got)
	}
}

func TestParseMountedPartitionsIgnoresUnrelatedDisks(t *testing.T) {
	data := []byte("/dev/sda1 / ext4 rw 0 0\n")
	if got := parseMountedPartitions(data, "sdc"); len(got) != 0 {
		t.Errorf("got %+v, want no matches", got)
	}
}
