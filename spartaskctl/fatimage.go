package main

import (
	"encoding/binary"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf16"
)

// cloud-init reads the VM's settings from a "NoCloud" seed: a vfat or iso9660 volume labelled
// CIDATA holding user-data, meta-data and network-config. Windows cannot write either without
// extra tools, so spartaskctl builds a small FAT12 volume itself: one root directory, long file
// names (VFAT) for the names cloud-init expects.

const (
	fatSectorSize   = 512
	fatTotalSectors = 2048 // 1 MiB
	fatRootEntries  = 224
	fatSectorsFAT   = 6 // FAT12 for up to 2048 clusters
	fatFirstData    = 1 + 2*fatSectorsFAT + fatRootEntries*32/fatSectorSize
	fatClusters     = fatTotalSectors - fatFirstData
)

// fatImage returns a 1 MiB FAT12 volume labelled label (at most 11 characters) holding files in
// its root directory. Names may be long; contents must fit the volume.
func fatImage(label string, files map[string][]byte, now time.Time) ([]byte, error) {
	if len(label) == 0 || len(label) > 11 || strings.ToUpper(label) != label {
		return nil, fmt.Errorf("fat label must be 1-11 upper-case characters, got %q", label)
	}
	img := make([]byte, fatTotalSectors*fatSectorSize)
	le := binary.LittleEndian

	// Boot sector and BIOS parameter block.
	boot := img[:fatSectorSize]
	copy(boot, []byte{0xEB, 0x3C, 0x90})
	copy(boot[3:], "SPARTASK")
	le.PutUint16(boot[11:], fatSectorSize)
	boot[13] = 1               // sectors per cluster
	le.PutUint16(boot[14:], 1) // reserved sectors
	boot[16] = 2               // FATs
	le.PutUint16(boot[17:], fatRootEntries)
	le.PutUint16(boot[19:], fatTotalSectors)
	boot[21] = 0xF8 // fixed disk
	le.PutUint16(boot[22:], fatSectorsFAT)
	le.PutUint16(boot[24:], 32) // sectors per track
	le.PutUint16(boot[26:], 2)  // heads
	boot[36] = 0x80
	boot[38] = 0x29 // extended boot signature
	le.PutUint32(boot[39:], uint32(now.Unix()))
	copy(boot[43:], fmt.Sprintf("%-11s", label))
	copy(boot[54:], "FAT12   ")
	boot[510], boot[511] = 0x55, 0xAA

	fat := make([]uint16, fatClusters+2)
	fat[0], fat[1] = 0xFF8, 0xFFF
	root := img[(1+2*fatSectorsFAT)*fatSectorSize : fatFirstData*fatSectorSize]
	entries := [][]byte{fatEntry([]byte(fmt.Sprintf("%-11s", label)), 0x08, 0, 0, now)}

	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	next := uint16(2)
	for i, name := range names {
		data := files[name]
		clusters := (len(data) + fatSectorSize - 1) / fatSectorSize
		if int(next)+clusters > fatClusters+2 {
			return nil, fmt.Errorf("fat volume too small for %s", name)
		}
		first := uint16(0)
		if clusters > 0 {
			first = next
			for c := 0; c < clusters; c++ {
				cluster := next + uint16(c)
				fat[cluster] = cluster + 1
				if c == clusters-1 {
					fat[cluster] = 0xFFF
				}
				offset := (fatFirstData + int(cluster) - 2) * fatSectorSize
				copy(img[offset:offset+fatSectorSize], data[c*fatSectorSize:])
			}
			next += uint16(clusters)
		}
		short := fatShortName(name, i+1)
		lfn, err := fatLongName(name, short)
		if err != nil {
			return nil, err
		}
		entries = append(entries, lfn...)
		entries = append(entries, fatEntry(short, 0x20, first, uint32(len(data)), now))
	}
	if len(entries) > fatRootEntries {
		return nil, fmt.Errorf("fat root directory holds %d entries, %d needed", fatRootEntries, len(entries))
	}
	for i, e := range entries {
		copy(root[i*32:], e)
	}

	// Both FAT copies, 12 bits per cluster.
	packed := make([]byte, fatSectorsFAT*fatSectorSize)
	for n, v := range fat {
		at := n * 3 / 2
		if n%2 == 0 {
			packed[at] = byte(v)
			packed[at+1] = packed[at+1]&0xF0 | byte(v>>8)&0x0F
		} else {
			packed[at] = packed[at]&0x0F | byte(v<<4)
			packed[at+1] = byte(v >> 4)
		}
	}
	for copyIndex := 0; copyIndex < 2; copyIndex++ {
		copy(img[(1+copyIndex*fatSectorsFAT)*fatSectorSize:], packed)
	}
	return img, nil
}

// fatShortName is a unique 8.3 name for the long name: its first valid characters, "~n".
func fatShortName(name string, n int) []byte {
	tail := fmt.Sprintf("~%d", n)
	var base strings.Builder
	for _, r := range strings.ToUpper(name) {
		if base.Len() == 8-len(tail) {
			break
		}
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			base.WriteRune(r)
		}
	}
	return []byte(fmt.Sprintf("%-11s", base.String()+tail))
}

// fatLongName is the VFAT long-name entries of name, in the order they precede the short entry.
func fatLongName(name string, short []byte) ([][]byte, error) {
	chars := utf16.Encode([]rune(name))
	if len(chars) == 0 || len(chars) > 255 {
		return nil, fmt.Errorf("invalid file name %q", name)
	}
	var sum byte
	for _, c := range short {
		sum = (sum&1)<<7 + sum>>1 + c
	}
	count := (len(chars) + 12) / 13
	padded := make([]uint16, count*13)
	for i := range padded {
		switch {
		case i < len(chars):
			padded[i] = chars[i]
		case i == len(chars):
			padded[i] = 0
		default:
			padded[i] = 0xFFFF
		}
	}
	out := make([][]byte, 0, count)
	for seq := count; seq >= 1; seq-- {
		e := make([]byte, 32)
		e[0] = byte(seq)
		if seq == count {
			e[0] |= 0x40
		}
		e[11] = 0x0F
		e[13] = sum
		part := padded[(seq-1)*13 : seq*13]
		for i, at := range []int{1, 3, 5, 7, 9, 14, 16, 18, 20, 22, 24, 28, 30} {
			binary.LittleEndian.PutUint16(e[at:], part[i])
		}
		out = append(out, e)
	}
	return out, nil
}

// fatEntry is a directory entry with the 8.3 name short.
func fatEntry(short []byte, attr byte, cluster uint16, size uint32, now time.Time) []byte {
	e := make([]byte, 32)
	copy(e, short)
	e[11] = attr
	t := uint16(now.Hour()<<11 | now.Minute()<<5 | now.Second()/2)
	d := uint16((now.Year()-1980)<<9 | int(now.Month())<<5 | now.Day())
	le := binary.LittleEndian
	for _, at := range []int{14, 22} {
		le.PutUint16(e[at:], t)
	}
	for _, at := range []int{16, 18, 24} {
		le.PutUint16(e[at:], d)
	}
	le.PutUint16(e[26:], cluster)
	le.PutUint32(e[28:], size)
	return e
}
