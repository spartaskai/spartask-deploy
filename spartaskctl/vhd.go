package main

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"io"
	"time"
)

// A fixed VHD is the raw disk followed by a 512-byte footer (Virtual Hard Disk Image Format
// Specification). Hyper-V's Convert-VHD turns it into the VHDX a generation 2 VM boots from.

const vhdFooterSize = 512

// vhdEpoch is the VHD timestamp origin.
var vhdEpoch = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)

// vhdFooter is the footer of a fixed VHD whose raw disk is size bytes (a multiple of 512).
func vhdFooter(size int64, now time.Time) ([]byte, error) {
	if size <= 0 || size%512 != 0 {
		return nil, fmt.Errorf("a VHD disk size must be a positive multiple of 512, got %d", size)
	}
	f := make([]byte, vhdFooterSize)
	be := binary.BigEndian
	copy(f[0:], "conectix")
	be.PutUint32(f[8:], 2)           // features: reserved bit
	be.PutUint32(f[12:], 0x00010000) // format version 1.0
	be.PutUint64(f[16:], ^uint64(0)) // data offset: none (fixed disk)
	be.PutUint32(f[24:], uint32(now.Sub(vhdEpoch)/time.Second))
	copy(f[28:], "sptk")             // creator application
	be.PutUint32(f[32:], 0x00010000) // creator version
	copy(f[36:], "Wi2k")             // creator host OS
	be.PutUint64(f[40:], uint64(size))
	be.PutUint64(f[48:], uint64(size))
	cylinders, heads, sectors := vhdGeometry(size)
	be.PutUint16(f[56:], cylinders)
	f[58], f[59] = heads, sectors
	be.PutUint32(f[60:], 2) // disk type: fixed
	if _, err := rand.Read(f[68:84]); err != nil {
		return nil, fmt.Errorf("vhd unique id: %w", err)
	}
	be.PutUint32(f[64:], vhdChecksum(f))
	return f, nil
}

// vhdChecksum is the one's complement of the byte sum of the footer, its checksum field ignored.
func vhdChecksum(footer []byte) uint32 {
	var sum uint32
	for i, b := range footer {
		if i < 64 || i >= 68 {
			sum += uint32(b)
		}
	}
	return ^sum
}

// vhdGeometry is the CHS geometry the VHD specification derives from the disk size.
func vhdGeometry(size int64) (cylinders uint16, heads, sectorsPerTrack uint8) {
	total := size / 512
	if total > 65535*16*255 {
		total = 65535 * 16 * 255
	}
	var spt, h, cth int64
	if total >= 65535*16*63 {
		spt, h = 255, 16
		cth = total / spt
	} else {
		spt = 17
		cth = total / spt
		h = (cth + 1023) / 1024
		if h < 4 {
			h = 4
		}
		if cth >= h*1024 || h > 16 {
			spt, h = 31, 16
			cth = total / spt
		}
		if cth >= h*1024 {
			spt, h = 63, 16
			cth = total / spt
		}
	}
	return uint16(cth / h), uint8(h), uint8(spt)
}

// writeFixedVHD makes the raw disk of size bytes at the start of w a fixed VHD: the size is
// rounded up to a whole MiB (Hyper-V's alignment), and the footer is written after it. It
// returns the disk size.
func writeFixedVHD(w io.WriterAt, size int64, now time.Time) (int64, error) {
	const mib = 1 << 20
	size = (size + mib - 1) / mib * mib
	footer, err := vhdFooter(size, now)
	if err != nil {
		return 0, err
	}
	if _, err := w.WriteAt(footer, size); err != nil {
		return 0, fmt.Errorf("write vhd footer: %w", err)
	}
	return size, nil
}
