package main

import (
	"bytes"
	"encoding/binary"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf16"
)

// readFATRoot reads the files of a FAT12 root directory the way an operating system does: long
// names from the VFAT entries, contents from the cluster chains.
func readFATRoot(t *testing.T, img []byte) (label string, files map[string][]byte) {
	t.Helper()
	le := binary.LittleEndian
	bps := int(le.Uint16(img[11:]))
	reserved := int(le.Uint16(img[14:]))
	fats := int(img[16])
	rootEntries := int(le.Uint16(img[17:]))
	perFAT := int(le.Uint16(img[22:]))
	fat := img[reserved*bps:]
	rootStart := (reserved + fats*perFAT) * bps
	dataStart := rootStart + rootEntries*32
	next := func(c int) int {
		v := int(le.Uint16(fat[c*3/2:]))
		if c%2 == 1 {
			return v >> 4
		}
		return v & 0xFFF
	}
	files = map[string][]byte{}
	var long []uint16
	for i := 0; i < rootEntries; i++ {
		e := img[rootStart+i*32 : rootStart+i*32+32]
		switch {
		case e[0] == 0:
			return label, files
		case e[11] == 0x0F:
			var part []uint16
			for _, at := range []int{1, 3, 5, 7, 9, 14, 16, 18, 20, 22, 24, 28, 30} {
				part = append(part, le.Uint16(e[at:]))
			}
			long = append(part, long...)
		case e[11]&0x08 != 0:
			label = strings.TrimSpace(string(e[:11]))
		default:
			if end := indexOf(long, 0); end >= 0 {
				long = long[:end]
			}
			name := string(utf16.Decode(long))
			long = nil
			size := int(le.Uint32(e[28:]))
			var data []byte
			for c := int(le.Uint16(e[26:])); c >= 2 && c < 0xFF8; c = next(c) {
				data = append(data, img[dataStart+(c-2)*bps:dataStart+(c-1)*bps]...)
			}
			files[name] = data[:size]
		}
	}
	return label, files
}

func indexOf(s []uint16, v uint16) int {
	for i, c := range s {
		if c == v {
			return i
		}
	}
	return -1
}

func TestFATImage(t *testing.T) {
	files := map[string][]byte{
		"user-data":      bytes.Repeat([]byte("#cloud-config\n"), 120), // several clusters
		"meta-data":      []byte("instance-id: spartask\n"),
		"network-config": []byte("version: 2\n"),
	}
	img, err := fatImage("CIDATA", files, time.Date(2026, 11, 8, 10, 30, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if len(img) != 1<<20 || img[510] != 0x55 || img[511] != 0xAA || string(img[43:54]) != "CIDATA     " || string(img[54:62]) != "FAT12   " {
		t.Fatal("boot sector")
	}
	label, got := readFATRoot(t, img)
	if label != "CIDATA" || len(got) != len(files) {
		t.Fatalf("label %q, files %v", label, len(got))
	}
	for name, want := range files {
		if !bytes.Equal(got[name], want) {
			t.Errorf("%s differs", name)
		}
	}
	// The second FAT is a copy of the first.
	fat := 6 * 512
	if !bytes.Equal(img[512:512+fat], img[512+fat:512+2*fat]) {
		t.Fatal("FAT copies differ")
	}

	// Where blkid exists (Linux), it must see what cloud-init looks for.
	if blkid, err := exec.LookPath("blkid"); err == nil {
		path := filepath.Join(t.TempDir(), "seed.img")
		if err := os.WriteFile(path, img, 0o600); err != nil {
			t.Fatal(err)
		}
		out, err := exec.Command(blkid, "-p", "-o", "export", path).Output()
		if err != nil {
			t.Fatalf("blkid: %v", err)
		}
		if !strings.Contains(string(out), "LABEL=CIDATA") || !strings.Contains(string(out), "TYPE=vfat") {
			t.Fatalf("blkid: %s", out)
		}
	}
}

func TestFATImageLimits(t *testing.T) {
	now := time.Now()
	if _, err := fatImage("cidata", nil, now); err == nil {
		t.Error("lower-case label accepted")
	}
	if _, err := fatImage("CIDATA", map[string][]byte{"big": make([]byte, 2<<20)}, now); err == nil {
		t.Error("oversized file accepted")
	}
	if got := string(fatShortName("network-config", 12)); got != "NETWO~12   " {
		t.Errorf("short name %q", got)
	}
}
