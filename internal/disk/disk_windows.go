//go:build windows

package disk

import (
	"encoding/binary"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"

	"golang.org/x/sys/windows"
)

// IOCTL/FSCTL codes and struct offsets below come from winioctl.h; golang.org/x/sys/windows doesn't define them.
const (
	ioctlStorageQueryProperty       = 0x002D1400
	ioctlDiskGetLengthInfo          = 0x0007405C
	ioctlVolumeGetVolumeDiskExtents = 0x00560000
	ioctlStorageEjectMedia          = 0x002D4808
	fsctlLockVolume                 = 0x00090018
	fsctlDismountVolume             = 0x00090020
)

const (
	storageDevicePropertyID = 0
	propertyStandardQuery   = 0
)

const (
	busTypeUSB = 0x07
	busTypeSD  = 0x0C
	busTypeMMC = 0x0D
)

const maxPhysicalDrives = 64

func platformNode(id Info) string { return `\\.\` + id.Identifier }

func platformList() ([]Info, error) {
	var out []Info
	for n := 0; n < maxPhysicalDrives; n++ {
		path := fmt.Sprintf(`\\.\PhysicalDrive%d`, n)
		h, err := openDiskHandle(path, false)
		if err != nil {
			if err == windows.ERROR_FILE_NOT_FOUND {
				break
			}
			continue
		}
		info, err := diskInfo(h, n)
		windows.CloseHandle(h)
		if err != nil {
			continue
		}
		out = append(out, info)
	}
	return out, nil
}

func openDiskHandle(path string, write bool) (windows.Handle, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return windows.InvalidHandle, err
	}
	access := uint32(windows.GENERIC_READ)
	if write {
		access |= windows.GENERIC_WRITE
	}
	return windows.CreateFile(p, access, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, 0, 0)
}

func diskInfo(h windows.Handle, n int) (Info, error) {
	var qbuf [8]byte
	binary.LittleEndian.PutUint32(qbuf[0:4], storageDevicePropertyID)
	binary.LittleEndian.PutUint32(qbuf[4:8], propertyStandardQuery)

	descBuf := make([]byte, 1024)
	var got uint32
	if err := windows.DeviceIoControl(h, ioctlStorageQueryProperty, &qbuf[0], uint32(len(qbuf)), &descBuf[0], uint32(len(descBuf)), &got, nil); err != nil {
		return Info{}, fmt.Errorf("querying device properties: %w", err)
	}
	busType, removableMedia, name, err := parseStorageDeviceDescriptor(descBuf[:got])
	if err != nil {
		return Info{}, err
	}
	internal, removable := classifyBusType(busType, removableMedia)

	var lenBuf [8]byte
	var gotLen uint32
	if err := windows.DeviceIoControl(h, ioctlDiskGetLengthInfo, nil, 0, &lenBuf[0], uint32(len(lenBuf)), &gotLen, nil); err != nil {
		return Info{}, fmt.Errorf("querying disk length: %w", err)
	}

	return Info{
		Identifier: fmt.Sprintf("PhysicalDrive%d", n),
		Name:       name,
		Size:       int64(binary.LittleEndian.Uint64(lenBuf[:])),
		Internal:   internal,
		Removable:  removable,
	}, nil
}

// USB, SD and MMC count as a card reader even when Windows doesn't flag the media itself as removable.
func classifyBusType(busType byte, removableMedia bool) (internal, removable bool) {
	isCardBus := busType == busTypeUSB || busType == busTypeSD || busType == busTypeMMC
	return !isCardBus, removableMedia || isCardBus
}

func parseStorageDeviceDescriptor(buf []byte) (busType byte, removableMedia bool, name string, err error) {
	if len(buf) < 36 {
		return 0, false, "", fmt.Errorf("short STORAGE_DEVICE_DESCRIPTOR: %d bytes", len(buf))
	}
	removableMedia = buf[10] != 0
	busType = buf[28]
	vendor := readCString(buf, binary.LittleEndian.Uint32(buf[12:16]))
	product := readCString(buf, binary.LittleEndian.Uint32(buf[16:20]))
	name = strings.TrimSpace(strings.TrimSpace(vendor) + " " + strings.TrimSpace(product))
	return busType, removableMedia, name, nil
}

func readCString(buf []byte, offset uint32) string {
	if offset == 0 || int(offset) >= len(buf) {
		return ""
	}
	end := int(offset)
	for end < len(buf) && buf[end] != 0 {
		end++
	}
	return string(buf[offset:end])
}

// A volume can span more than one disk, so this returns every extent's disk number.
func parseVolumeDiskExtents(buf []byte) ([]uint32, error) {
	const extentsOffset = 8
	const extentSize = 24

	if len(buf) < extentsOffset {
		return nil, fmt.Errorf("short VOLUME_DISK_EXTENTS: %d bytes", len(buf))
	}
	count := binary.LittleEndian.Uint32(buf[0:4])
	out := make([]uint32, 0, count)
	for i := uint32(0); i < count; i++ {
		start := extentsOffset + i*extentSize
		if int(start+4) > len(buf) {
			return nil, fmt.Errorf("VOLUME_DISK_EXTENTS too short for %d extents", count)
		}
		out = append(out, binary.LittleEndian.Uint32(buf[start:start+4]))
	}
	return out, nil
}

func containsDisk(disks []uint32, want uint32) bool {
	for _, d := range disks {
		if d == want {
			return true
		}
	}
	return false
}

func diskNumberFromIdentifier(id string) (uint32, error) {
	s := strings.TrimPrefix(id, "PhysicalDrive")
	n, err := strconv.ParseUint(s, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("parsing disk number from %q: %w", id, err)
	}
	return uint32(n), nil
}

// locked holds handles until eject, the next unmount, or GC: *os.File has no Close hook to release them on.
var (
	lockedMu sync.Mutex
	locked   = map[uint32][]windows.Handle{}
)

func releaseLocks(diskNum uint32) {
	lockedMu.Lock()
	hs := locked[diskNum]
	delete(locked, diskNum)
	lockedMu.Unlock()
	for _, h := range hs {
		windows.CloseHandle(h)
	}
}

func platformUnmount(id string) error {
	diskNum, err := diskNumberFromIdentifier(id)
	if err != nil {
		return err
	}
	releaseLocks(diskNum)

	drives, err := windows.GetLogicalDrives()
	if err != nil {
		return fmt.Errorf("listing drive letters: %w", err)
	}

	var handles []windows.Handle
	for letter := 0; letter < 26; letter++ {
		if drives&(1<<uint(letter)) == 0 {
			continue
		}
		volPath := fmt.Sprintf(`\\.\%c:`, 'A'+letter)
		h, err := openVolumeHandle(volPath)
		if err != nil {
			continue
		}
		diskNums, err := queryVolumeDiskExtents(h)
		if err != nil || !containsDisk(diskNums, diskNum) {
			windows.CloseHandle(h)
			continue
		}
		if err := lockAndDismount(h); err != nil {
			windows.CloseHandle(h)
			for _, held := range handles {
				windows.CloseHandle(held)
			}
			return fmt.Errorf("locking %s: %w", volPath, err)
		}
		handles = append(handles, h)
	}

	lockedMu.Lock()
	locked[diskNum] = handles
	lockedMu.Unlock()
	return nil
}

func openVolumeHandle(path string) (windows.Handle, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return windows.InvalidHandle, err
	}
	return windows.CreateFile(p, windows.GENERIC_READ|windows.GENERIC_WRITE, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, 0, 0)
}

func queryVolumeDiskExtents(h windows.Handle) ([]uint32, error) {
	buf := make([]byte, 1024)
	var got uint32
	if err := windows.DeviceIoControl(h, ioctlVolumeGetVolumeDiskExtents, nil, 0, &buf[0], uint32(len(buf)), &got, nil); err != nil {
		return nil, err
	}
	return parseVolumeDiskExtents(buf[:got])
}

func lockAndDismount(h windows.Handle) error {
	var got uint32
	if err := windows.DeviceIoControl(h, fsctlLockVolume, nil, 0, nil, 0, &got, nil); err != nil {
		return fmt.Errorf("FSCTL_LOCK_VOLUME: %w", err)
	}
	if err := windows.DeviceIoControl(h, fsctlDismountVolume, nil, 0, nil, 0, &got, nil); err != nil {
		return fmt.Errorf("FSCTL_DISMOUNT_VOLUME: %w", err)
	}
	return nil
}

func platformEject(id string) error {
	diskNum, err := diskNumberFromIdentifier(id)
	if err != nil {
		return err
	}
	releaseLocks(diskNum)

	h, err := openDiskHandle(fmt.Sprintf(`\\.\PhysicalDrive%d`, diskNum), true)
	if err != nil {
		return nil
	}
	defer windows.CloseHandle(h)

	var got uint32
	// Ejecting often isn't supported on a reader slot; that's not treated as an error.
	_ = windows.DeviceIoControl(h, ioctlStorageEjectMedia, nil, 0, nil, 0, &got, nil)
	return nil
}

func platformOpen(id Info, write bool) (*os.File, error) {
	path := platformNode(id)
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, fmt.Errorf("encoding %s: %w", path, err)
	}
	access := uint32(windows.GENERIC_READ)
	if write {
		access |= windows.GENERIC_WRITE
	}

	// No FILE_FLAG_NO_BUFFERING: a Go slice isn't guaranteed to be sector-aligned in memory.
	h, err := windows.CreateFile(p, access, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_WRITE_THROUGH, 0)
	if err != nil {
		if err == windows.ERROR_ACCESS_DENIED {
			return nil, fmt.Errorf("opening %s: access denied; run nissan-nav-tools as administrator", path)
		}
		return nil, fmt.Errorf("opening %s: %w", path, err)
	}

	f := os.NewFile(uintptr(h), path)
	if diskNum, err := diskNumberFromIdentifier(id.Identifier); err == nil {
		runtime.SetFinalizer(f, func(*os.File) { releaseLocks(diskNum) })
	}
	return f, nil
}
