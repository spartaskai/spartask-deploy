package main

import (
	"bytes"
	"compress/flate"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// qcow2Cluster is how a test image stores one guest cluster.
type qcow2Cluster int

const (
	clusterUnallocated qcow2Cluster = iota
	clusterPlain
	clusterCompressed
	clusterZeroFlag
)

// buildQcow2 writes a version 3 qcow2 image (cluster size 2^bits) of a guest disk of size bytes,
// storing guest cluster i as kinds[i] (clusters past kinds are unallocated). It follows the qcow2
// specification the way qemu-img writes it: raw deflate for compressed clusters, packed at
// unaligned offsets.
func buildQcow2(t *testing.T, bits uint32, size uint64, disk []byte, kinds []qcow2Cluster) []byte {
	t.Helper()
	cs := uint64(1) << bits
	perL2 := cs / 8
	l1Size := (size + perL2*cs - 1) / (perL2 * cs)
	img := make([]byte, cs) // header cluster
	be := binary.BigEndian
	be.PutUint32(img[0:], qcow2Magic)
	be.PutUint32(img[4:], 3)
	be.PutUint32(img[20:], bits)
	be.PutUint64(img[24:], size)
	be.PutUint32(img[36:], uint32(l1Size))
	be.PutUint64(img[40:], cs) // L1 table in cluster 1
	be.PutUint32(img[96:], 4)  // refcount order
	be.PutUint32(img[100:], 104)
	l1 := make([]byte, cs)
	img = append(img, l1...)
	l2Offsets := make([]uint64, l1Size)
	for i := range l2Offsets {
		l2Offsets[i] = uint64(len(img))
		be.PutUint64(img[cs+uint64(i)*8:], l2Offsets[i]|1<<63)
		img = append(img, make([]byte, cs)...)
	}
	compressedBits := 62 - (bits - 8)
	for i, kind := range kinds {
		guest := uint64(i) * cs
		end := min(guest+cs, size)
		data := disk[guest:end]
		var entry uint64
		switch kind {
		case clusterPlain:
			img = append(img, make([]byte, (cs-uint64(len(img))%cs)%cs)...) // clusters are cluster-aligned
			entry = uint64(len(img)) | 1<<63
			img = append(img, data...)
			img = append(img, make([]byte, cs-uint64(len(data)))...)
		case clusterCompressed:
			var buf bytes.Buffer
			w, err := flate.NewWriter(&buf, flate.BestCompression)
			if err != nil {
				t.Fatal(err)
			}
			full := make([]byte, cs)
			copy(full, data)
			if _, err := w.Write(full); err != nil {
				t.Fatal(err)
			}
			if err := w.Close(); err != nil {
				t.Fatal(err)
			}
			img = append(img, 0, 0, 0) // unaligned start, as qemu-img packs compressed clusters
			offset := uint64(len(img))
			img = append(img, buf.Bytes()...)
			sectors := (offset%512 + uint64(buf.Len()) + 511) / 512
			entry = qcow2Compressed | (sectors-1)<<compressedBits | offset
		case clusterZeroFlag:
			entry = qcow2ZeroFlag
		}
		l2 := l2Offsets[uint64(i)/perL2]
		be.PutUint64(img[l2+(uint64(i)%perL2)*8:], entry)
	}
	return img
}

func TestQcow2ToRaw(t *testing.T) {
	const bits = 12 // 4 KiB clusters keep the test small; Ubuntu uses 64 KiB
	cs := uint64(1) << bits
	size := 7*cs - 100 // the last cluster is partial
	disk := make([]byte, size)
	for i := range disk {
		disk[i] = byte(i*7 + i/4096)
	}
	copy(disk[2*cs:3*cs], make([]byte, cs)) // zero-flag cluster: zeros in the guest
	copy(disk[3*cs:4*cs], make([]byte, cs)) // unallocated: zeros
	copy(disk[4*cs:5*cs], make([]byte, cs)) // plain cluster holding zeros
	kinds := []qcow2Cluster{clusterPlain, clusterCompressed, clusterZeroFlag, clusterUnallocated, clusterPlain, clusterCompressed, clusterCompressed}
	img := buildQcow2(t, bits, size, disk, kinds)

	path := filepath.Join(t.TempDir(), "disk.raw")
	out, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	got, err := qcow2ToRaw(bytes.NewReader(img), int64(len(img)), out)
	if err != nil {
		t.Fatal(err)
	}
	if got != int64(size) {
		t.Fatalf("size %d, want %d", got, size)
	}
	if err := out.Truncate(got); err != nil {
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(raw, disk) {
		for i := range disk {
			if raw[i] != disk[i] {
				t.Fatalf("disk differs first at byte %d (cluster %d)", i, uint64(i)/cs)
			}
		}
	}
}

func TestQcow2ToRawSpansL2Tables(t *testing.T) {
	const bits = 9 // 512-byte clusters: one L2 table covers 64 clusters
	cs := uint64(1) << bits
	size := 130 * cs
	disk := bytes.Repeat([]byte("spartask"), int(size/8))
	kinds := make([]qcow2Cluster, 130)
	for i := range kinds {
		kinds[i] = clusterPlain
		if i%3 == 0 {
			kinds[i] = clusterCompressed
		}
	}
	img := buildQcow2(t, bits, size, disk, kinds)
	var dst memDisk
	if _, err := qcow2ToRaw(bytes.NewReader(img), int64(len(img)), &dst); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(dst.bytes(int(size)), disk) {
		t.Fatal("disk differs")
	}
}

func TestQcow2HeaderChecks(t *testing.T) {
	good := buildQcow2(t, 12, 4096, make([]byte, 4096), nil)
	cases := map[string]func([]byte){
		"not qcow2":    func(b []byte) { copy(b, "VHDX") },
		"version 4":    func(b []byte) { binary.BigEndian.PutUint32(b[4:], 4) },
		"backing file": func(b []byte) { binary.BigEndian.PutUint64(b[8:], 512) },
		"encrypted":    func(b []byte) { binary.BigEndian.PutUint32(b[32:], 2) },
		"extended L2":  func(b []byte) { binary.BigEndian.PutUint64(b[72:], 1<<4) },
		"zstd": func(b []byte) {
			binary.BigEndian.PutUint64(b[72:], 1<<3)
			binary.BigEndian.PutUint32(b[100:], 112)
			b[104] = 1
		},
	}
	for name, change := range cases {
		img := bytes.Clone(good)
		change(img)
		if _, err := qcow2ToRaw(bytes.NewReader(img), int64(len(img)), &memDisk{}); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	if _, err := qcow2ToRaw(bytes.NewReader(good[:40]), 40, &memDisk{}); err == nil || !strings.Contains(err.Error(), "header") {
		t.Errorf("short header: %v", err)
	}
}

// memDisk is an in-memory io.WriterAt that reads as zeros where nothing was written.
type memDisk struct{ data []byte }

func (m *memDisk) WriteAt(p []byte, off int64) (int, error) {
	if end := int(off) + len(p); end > len(m.data) {
		m.data = append(m.data, make([]byte, end-len(m.data))...)
	}
	return copy(m.data[off:], p), nil
}

func (m *memDisk) bytes(size int) []byte {
	out := make([]byte, size)
	copy(out, m.data)
	return out
}
