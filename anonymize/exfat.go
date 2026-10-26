package anonymize

import (
	"fmt"
	"regexp"
	"strings"
)

type exfatVol struct {
	f    ReadWriterAt
	base int64 // bytes, volume start on the image

	fatOffset  int64 // sectors from base
	fatLength  int64
	heapOffset int64 // sectors from base
	rootFirst  uint32

	sectorSize   int64
	spc          int64 // sectors per cluster
	clusterBytes int64
	activeFat    int64
}

func openExfat(f ReadWriterAt, lba int64) (*exfatVol, error) {
	base := lba * 512
	b := make([]byte, 512)
	if _, err := f.ReadAt(b, base); err != nil {
		return nil, err
	}
	if string(b[3:11]) != "EXFAT   " {
		return nil, fmt.Errorf("not exFAT")
	}
	v := &exfatVol{f: f, base: base}
	v.fatOffset = int64(le32(b[0x50:0x54]))
	v.fatLength = int64(le32(b[0x54:0x58]))
	v.heapOffset = int64(le32(b[0x58:0x5c]))
	v.rootFirst = le32(b[0x60:0x64])
	flags := le16(b[0x6a:0x6c])
	v.sectorSize = 1 << b[0x6c]
	v.spc = 1 << b[0x6d]
	v.clusterBytes = v.sectorSize * v.spc
	if flags&1 != 0 {
		v.activeFat = 1
	}
	return v, nil
}

func (v *exfatVol) fatSectorOffset() int64 {
	return v.base + (v.fatOffset+v.activeFat*v.fatLength)*v.sectorSize
}

func (v *exfatVol) fatNext(c uint32) uint32 {
	off := v.fatSectorOffset() + int64(c)*4
	b := make([]byte, 4)
	v.f.ReadAt(b, off)
	return le32(b)
}

// chain lists a file's clusters; noFat means they run contiguous from first instead of being FAT-linked.
func (v *exfatVol) chain(first uint32, noFat bool, size uint64) []uint32 {
	if first < 2 {
		return nil
	}
	if noFat {
		n := int64(1)
		if size > 0 {
			n = (int64(size) + v.clusterBytes - 1) / v.clusterBytes
			if n < 1 {
				n = 1
			}
		}
		out := make([]uint32, n)
		for i := range out {
			out[i] = first + uint32(i)
		}
		return out
	}
	var out []uint32
	c := first
	for c >= 2 && c < 0xfffffff7 && len(out) < 1_000_000 {
		out = append(out, c)
		c = v.fatNext(c)
	}
	return out
}

func (v *exfatVol) clusterOffset(c uint32) int64 {
	return v.base + (v.heapOffset+(int64(c)-2)*v.spc)*v.sectorSize
}

type matched struct {
	path   string
	what   string
	size   int64
	offset int64
	chain  []uint32
}

type rule struct {
	dir  string
	name *regexp.Regexp
	what string
}

type prefixRule struct {
	prefix string
	what   string
}

var rules = []rule{
	{"/PRIVATE/TEL/PB_OBJ", regexp.MustCompile(`^[0-9]+_TELBOOK\.BIN$`), "phonebook"},
	{"/PRIVATE/TEL/PB_OBJ", regexp.MustCompile(`^MGR_INFO\.BIN$`), "phone/BT pairing manager info"},
	{"/PRIVATE/CWS/DCME", regexp.MustCompile(`^DCME_TCUPIN$`), "TCU/BT pairing PIN"},
	{"/PRIVATE/CWS/DJ", regexp.MustCompile(`^CWS_DEST$`), "destinations and address book (includes Home)"},
	{"/PRIVATE/CWS/DJ", regexp.MustCompile(`^CWS_ROUTE$`), "route history"},
	{"/PRIVATE/CWS/DJ", regexp.MustCompile(`^CWS_DJHST_[0-9]+$`), "destination history slot"},
	{"/PRIVATE/CWS/AP", regexp.MustCompile(`^CWS_SYS$`), "CARWINGS connection profile"},
	{"/PRIVATE/CWS/AP", regexp.MustCompile(`^CWS_COM$`), "CARWINGS/communication state"},
	{"/PRIVATE/CWS/AP", regexp.MustCompile(`^CWS_CNF$`), "CARWINGS small config"},
	{"/PRIVATE/CWS/CP", regexp.MustCompile(`^CWS_CPMANUAL$`), "charging-station cache (manual)"},
	{"/PRIVATE/CWS/CP", regexp.MustCompile(`^CWS_CPAUTOHOME$`), "charging-station cache (home area)"},
}

var prefixRules = []prefixRule{
	{"/PRIVATE/CWS/PIC/", "picture tied to a destination-history slot"},
}

func wants(dir, name string) string {
	dir = strings.ToUpper(dir)
	name = strings.ToUpper(name)
	for _, r := range rules {
		if r.dir == dir && r.name.MatchString(name) {
			return r.what
		}
	}
	full := dir + "/" + name
	for _, pr := range prefixRules {
		if strings.HasPrefix(full, pr.prefix) {
			return pr.what
		}
	}
	return ""
}

type dirEnt struct {
	name     string
	isDir    bool
	noFat    bool
	first    uint32
	dataSize uint64
}

// readDir parses 0x85 (file), 0xc0 (stream extension) and 0xc1 (file name) directory entries.
func (v *exfatVol) readDir(first uint32, noFat bool, size uint64) ([]dirEnt, error) {
	var out []dirEnt
	var cur *dirEnt
	for _, c := range v.chain(first, noFat, size) {
		buf := make([]byte, v.clusterBytes)
		if _, err := v.f.ReadAt(buf, v.clusterOffset(c)); err != nil {
			return nil, err
		}
		for off := 0; off+32 <= len(buf); off += 32 {
			e := buf[off : off+32]
			switch e[0] {
			case 0x00:
				goto done
			case 0x85:
				attr := le16(e[4:6])
				out = append(out, dirEnt{isDir: attr&0x10 != 0})
				cur = &out[len(out)-1]
			case 0xc0:
				if cur != nil {
					cur.noFat = e[1]&2 != 0 // exFAT no-FAT-chain flag
					cur.first = le32(e[0x14:0x18])
					cur.dataSize = le64(e[0x18:0x20])
				}
			case 0xc1:
				if cur != nil {
					cur.name += decodeUTF16LE(e[2:32])
				}
			}
		}
	}
done:
	for i := range out {
		out[i].name = strings.TrimRight(out[i].name, "\x00")
	}
	return out, nil
}

func decodeUTF16LE(b []byte) string {
	var sb strings.Builder
	for i := 0; i+1 < len(b); i += 2 {
		u := uint16(b[i]) | uint16(b[i+1])<<8
		if u == 0 {
			break
		}
		sb.WriteRune(rune(u))
	}
	return sb.String()
}

func (v *exfatVol) findTargets() ([]matched, error) {
	var out []matched
	var walk func(first uint32, noFat bool, size uint64, path string) error
	walk = func(first uint32, noFat bool, size uint64, path string) error {
		ents, err := v.readDir(first, noFat, size)
		if err != nil {
			return err
		}
		for _, e := range ents {
			if e.first == 0 {
				continue
			}
			full := path + "/" + e.name
			if e.isDir {
				if err := walk(e.first, e.noFat, e.dataSize, full); err != nil {
					return err
				}
				continue
			}
			what := wants(path, e.name)
			if what == "" {
				continue
			}
			chain := v.chain(e.first, e.noFat, e.dataSize)
			out = append(out, matched{
				path:   full,
				what:   what,
				size:   int64(e.dataSize),
				offset: v.clusterOffset(e.first),
				chain:  chain,
			})
		}
		return nil
	}
	if err := walk(v.rootFirst, false, 0, ""); err != nil {
		return nil, err
	}
	return out, nil
}

func (v *exfatVol) zero(f ReadWriterAt, m matched) error {
	if len(m.chain) == 0 {
		return nil
	}
	blank := make([]byte, v.clusterBytes)
	for _, c := range m.chain {
		if _, err := f.WriteAt(blank, v.clusterOffset(c)); err != nil {
			return err
		}
	}
	return nil
}
