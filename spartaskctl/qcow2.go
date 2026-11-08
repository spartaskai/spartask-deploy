package main

import (
	"bytes"
	"compress/flate"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// The Ubuntu cloud image is a qcow2 file; Hyper-V boots VHDX. Windows has no tool that reads
// qcow2, so spartaskctl converts it: qcow2ToRaw copies the guest disk out of the image and
// writeFixedVHD turns the raw disk into a fixed VHD, which Convert-VHD makes a VHDX.

const qcow2Magic = 0x514649fb // "QFI\xfb"

// qcow2Image is the part of a qcow2 header the conversion needs.
type qcow2Image struct {
	clusterBits uint32
	size        uint64
	l1Size      uint32
	l1Offset    uint64
}

func (q qcow2Image) clusterSize() uint64 { return 1 << q.clusterBits }

// readQcow2Header reads and checks the header: version 2 or 3, no backing file, no encryption,
// no external data file or extended L2 entries, zlib compression.
func readQcow2Header(r io.ReaderAt) (qcow2Image, error) {
	var h [105]byte
	n, err := r.ReadAt(h[:], 0)
	if n < 72 {
		return qcow2Image{}, fmt.Errorf("read qcow2 header: %w", errors.Join(err, io.ErrUnexpectedEOF))
	}
	be := binary.BigEndian
	if be.Uint32(h[0:]) != qcow2Magic {
		return qcow2Image{}, errors.New("not a qcow2 image")
	}
	version := be.Uint32(h[4:])
	if version != 2 && version != 3 {
		return qcow2Image{}, fmt.Errorf("unsupported qcow2 version %d", version)
	}
	if be.Uint64(h[8:]) != 0 {
		return qcow2Image{}, errors.New("qcow2 images with a backing file are not supported")
	}
	if be.Uint32(h[32:]) != 0 {
		return qcow2Image{}, errors.New("encrypted qcow2 images are not supported")
	}
	q := qcow2Image{clusterBits: be.Uint32(h[20:]), size: be.Uint64(h[24:]), l1Size: be.Uint32(h[36:]), l1Offset: be.Uint64(h[40:])}
	if q.clusterBits < 9 || q.clusterBits > 21 {
		return qcow2Image{}, fmt.Errorf("invalid qcow2 cluster size 2^%d", q.clusterBits)
	}
	if version == 3 {
		if n < 104 {
			return qcow2Image{}, fmt.Errorf("read qcow2 header: %w", io.ErrUnexpectedEOF)
		}
		const (
			externalData = 1 << 2
			compression  = 1 << 3
			extendedL2   = 1 << 4
			known        = 1<<0 | externalData | compression | extendedL2
		)
		incompatible := be.Uint64(h[72:])
		if incompatible&^known != 0 || incompatible&(externalData|extendedL2) != 0 {
			return qcow2Image{}, fmt.Errorf("unsupported qcow2 features %#x", incompatible)
		}
		if incompatible&compression != 0 && (be.Uint32(h[100:]) <= 104 || n < 105 || h[104] != 0) {
			return qcow2Image{}, errors.New("only zlib-compressed qcow2 images are supported")
		}
	}
	return q, nil
}

const (
	qcow2OffsetMask = 0x00fffffffffffe00 // bits 9-55: the host offset of an L2 table or cluster
	qcow2Compressed = 1 << 62
	qcow2ZeroFlag   = 1 << 0
)

// qcow2ToRaw writes the guest disk of the qcow2 image src (srcSize bytes) to dst, which must
// read as zeros where nothing is written (a new or truncated file): unallocated and zero clusters
// are skipped. It returns the virtual size.
func qcow2ToRaw(src io.ReaderAt, srcSize int64, dst io.WriterAt) (int64, error) {
	q, err := readQcow2Header(src)
	if err != nil {
		return 0, err
	}
	cs := q.clusterSize()
	perL2 := cs / 8
	if want := (q.size + perL2*cs - 1) / (perL2 * cs); uint64(q.l1Size) < want {
		return 0, fmt.Errorf("qcow2 L1 table has %d entries, %d needed", q.l1Size, want)
	}
	l1 := make([]byte, uint64(q.l1Size)*8)
	if _, err := src.ReadAt(l1, int64(q.l1Offset)); err != nil {
		return 0, fmt.Errorf("read qcow2 L1 table: %w", err)
	}
	l2 := make([]byte, cs)
	cluster := make([]byte, cs)
	compressedBits := 62 - (q.clusterBits - 8)
	for i := uint64(0); i < uint64(q.l1Size); i++ {
		l2Offset := binary.BigEndian.Uint64(l1[i*8:]) & qcow2OffsetMask
		if l2Offset == 0 {
			continue
		}
		if _, err := src.ReadAt(l2, int64(l2Offset)); err != nil {
			return 0, fmt.Errorf("read qcow2 L2 table %d: %w", i, err)
		}
		for j := uint64(0); j < perL2; j++ {
			guest := (i*perL2 + j) * cs
			if guest >= q.size {
				break
			}
			entry := binary.BigEndian.Uint64(l2[j*8:])
			length := min(cs, q.size-guest)
			switch {
			case entry&qcow2Compressed != 0:
				offset := entry & (1<<compressedBits - 1)
				sectors := (entry>>compressedBits)&(1<<(q.clusterBits-8)-1) + 1
				size := int64(sectors*512 - offset%512)
				if rest := srcSize - int64(offset); size > rest {
					size = rest
				}
				if size <= 0 {
					return 0, fmt.Errorf("qcow2 cluster at %d: compressed data outside the file", guest)
				}
				packed := make([]byte, size)
				if _, err := src.ReadAt(packed, int64(offset)); err != nil && !errors.Is(err, io.EOF) {
					return 0, fmt.Errorf("read qcow2 cluster at %d: %w", guest, err)
				}
				if _, err := io.ReadFull(flate.NewReader(bytes.NewReader(packed)), cluster[:length]); err != nil {
					return 0, fmt.Errorf("decompress qcow2 cluster at %d: %w", guest, err)
				}
			case entry&qcow2ZeroFlag != 0 || entry&qcow2OffsetMask == 0:
				continue
			default:
				if _, err := src.ReadAt(cluster[:length], int64(entry&qcow2OffsetMask)); err != nil {
					return 0, fmt.Errorf("read qcow2 cluster at %d: %w", guest, err)
				}
			}
			if allZero(cluster[:length]) {
				continue
			}
			if _, err := dst.WriteAt(cluster[:length], int64(guest)); err != nil {
				return 0, fmt.Errorf("write disk at %d: %w", guest, err)
			}
		}
	}
	return int64(q.size), nil
}

func allZero(b []byte) bool {
	for _, c := range b {
		if c != 0 {
			return false
		}
	}
	return true
}
