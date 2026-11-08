package main

import (
	"encoding/binary"
	"testing"
	"time"
)

func TestVHDFooter(t *testing.T) {
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	const size = 3758096384 // the Ubuntu 24.04 cloud image disk (3.5 GiB)
	f, err := vhdFooter(size, now)
	if err != nil {
		t.Fatal(err)
	}
	be := binary.BigEndian
	if string(f[:8]) != "conectix" || be.Uint32(f[12:]) != 0x00010000 || be.Uint64(f[16:]) != ^uint64(0) ||
		be.Uint64(f[40:]) != size || be.Uint64(f[48:]) != size || be.Uint32(f[60:]) != 2 {
		t.Fatalf("footer fields: % x", f[:64])
	}
	if got := be.Uint32(f[24:]); got != uint32(now.Sub(vhdEpoch).Seconds()) {
		t.Fatalf("timestamp %d", got)
	}
	if be.Uint32(f[64:]) != vhdChecksum(f) {
		t.Fatal("checksum does not match")
	}
	// The specification's geometry for this size: 7281 cylinders, 16 heads, 63 sectors per
	// track (the 63-sector branch), which covers no more than the disk.
	c, h, s := vhdGeometry(size)
	if int64(c)*int64(h)*int64(s)*512 > size || h != 16 || s != 63 {
		t.Fatalf("geometry %d/%d/%d", c, h, s)
	}
	for _, bad := range []int64{0, 1000, -512} {
		if _, err := vhdFooter(bad, now); err == nil {
			t.Errorf("size %d accepted", bad)
		}
	}
}

func TestVHDGeometryLargeDisk(t *testing.T) {
	c, h, s := vhdGeometry(200 << 30)
	if c != 65535 || h != 16 || s != 255 {
		t.Fatalf("geometry %d/%d/%d", c, h, s)
	}
}

func TestWriteFixedVHD(t *testing.T) {
	var disk memDisk
	size, err := writeFixedVHD(&disk, 3<<20+1, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if size != 4<<20 || len(disk.data) != 4<<20+vhdFooterSize || string(disk.data[size:size+8]) != "conectix" {
		t.Fatalf("size %d, file %d", size, len(disk.data))
	}
}
