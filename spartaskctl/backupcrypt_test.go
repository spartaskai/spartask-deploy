package main

import (
	"bytes"
	"crypto/rand"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

var testBackupKey = []byte("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")

func sealBytes(t *testing.T, plain, key []byte) []byte {
	t.Helper()
	var out bytes.Buffer
	enc, err := newBackupEncrypter(&out, key)
	if err != nil {
		t.Fatal(err)
	}
	// odd write sizes cross chunk boundaries
	for rest := plain; len(rest) > 0; {
		n := min(len(rest), 7777)
		if _, err := enc.Write(rest[:n]); err != nil {
			t.Fatal(err)
		}
		rest = rest[n:]
	}
	if err := enc.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func openBytes(sealed, key []byte) ([]byte, error) {
	dec, err := newBackupDecrypter(bytes.NewReader(sealed), key)
	if err != nil {
		return nil, err
	}
	return io.ReadAll(dec)
}

func TestBackupCryptRoundTrip(t *testing.T) {
	for _, size := range []int{0, 1, backupChunkSize - 1, backupChunkSize, backupChunkSize + 1, 3*backupChunkSize + 123} {
		plain := make([]byte, size)
		rand.Read(plain)
		sealed := sealBytes(t, plain, testBackupKey)
		if size > 16 && bytes.Contains(sealed, plain[:16]) {
			t.Fatalf("size %d: plaintext visible in the encrypted file", size)
		}
		got, err := openBytes(sealed, testBackupKey)
		if err != nil || !bytes.Equal(got, plain) {
			t.Fatalf("size %d: round trip failed: %v", size, err)
		}
	}
}

func TestBackupCryptRejectsWrongKeyAndTampering(t *testing.T) {
	plain := make([]byte, 2*backupChunkSize+10)
	rand.Read(plain)
	sealed := sealBytes(t, plain, testBackupKey)

	if _, err := openBytes(sealed, []byte("another-key-0123456789abcdef-0123456789")); !errors.Is(err, errBackupKey) {
		t.Fatalf("wrong key: got %v", err)
	}
	flipped := bytes.Clone(sealed)
	flipped[len(flipped)/2] ^= 1
	if _, err := openBytes(flipped, testBackupKey); err == nil {
		t.Fatal("tampered file was accepted")
	}
	// Dropping the final chunk must not look like a complete (shorter) file.
	header := len(backupMagic) + backupSaltSize
	fullChunk := backupChunkSize + 16
	if _, err := openBytes(sealed[:header+2*fullChunk], testBackupKey); err == nil {
		t.Fatal("truncated file was accepted")
	}
	if _, err := openBytes(append(bytes.Clone(sealed), 0), testBackupKey); err == nil {
		t.Fatal("trailing data was accepted")
	}
}

func TestOpenBackupFilePlainAndEncrypted(t *testing.T) {
	dir := t.TempDir()
	if err := writeBackupFile(filepath.Join(dir, "spartask.dump"), testBackupKey, func(w io.Writer) error {
		_, err := io.WriteString(w, "dump-data")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "spartask.dump")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("plaintext dump written next to the encrypted one")
	}
	if !backupIsEncrypted(dir) {
		t.Fatal("encrypted backup not detected")
	}
	r, err := openBackupFile(dir, "spartask.dump", testBackupKey)
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(r)
	r.Close()
	if err != nil || string(data) != "dump-data" {
		t.Fatalf("got %q, %v", data, err)
	}
	if _, err := openBackupFile(dir, "spartask.dump", nil); err == nil {
		t.Fatal("encrypted file opened without a key")
	}
	if err := checkBackupKey(filepath.Join(dir, "spartask.dump.enc"), []byte("wrong-key-0123456789abcdef0123456789")); err == nil {
		t.Fatal("wrong key passed the check")
	}

	// Older, unencrypted backups still restore.
	if err := os.WriteFile(filepath.Join(dir, "storage.tar.gz"), []byte("plain"), 0o600); err != nil {
		t.Fatal(err)
	}
	r, err = openBackupFile(dir, "storage.tar.gz", testBackupKey)
	if err != nil {
		t.Fatal(err)
	}
	data, _ = io.ReadAll(r)
	r.Close()
	if string(data) != "plain" {
		t.Fatalf("plain file: got %q", data)
	}
	if _, err := openBackupFile(dir, "marketplace.dump", testBackupKey); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing file: got %v", err)
	}
}

func TestEncryptedTarRoundTrip(t *testing.T) {
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, ".env"), []byte("SECRET_KEY=x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := writeBackupFile(filepath.Join(dir, "config.tar.gz"), testBackupKey, func(w io.Writer) error {
		return writeTar(w, src, []string{".env", "certs"})
	}); err != nil {
		t.Fatal(err)
	}
	dst := t.TempDir()
	if err := untarBackup(dir, "config.tar.gz", testBackupKey, dst); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dst, ".env"))
	if err != nil || string(data) != "SECRET_KEY=x\n" {
		t.Fatalf("got %q, %v", data, err)
	}
}
