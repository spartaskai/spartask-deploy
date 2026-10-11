package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Encrypted backup files carry the .enc suffix and this layout:
//
//	"SPARTASK-BACKUP-1\n" | 32-byte random salt | sealed chunks
//
// Each file has its own AES-256-GCM key, HMAC-SHA256(BACKUP_ENCRYPTION_KEY, label | salt). The
// plaintext is cut into 64 KiB chunks sealed in order; the nonce is the chunk counter plus a flag
// on the final chunk, so reordered, dropped or truncated chunks fail to decrypt (the STREAM
// construction also used by age).
const (
	backupMagic     = "SPARTASK-BACKUP-1\n"
	backupSaltSize  = 32
	backupChunkSize = 64 << 10
	encSuffix       = ".enc"
)

var (
	errBackupKey       = errors.New("cannot decrypt the backup: wrong BACKUP_ENCRYPTION_KEY or damaged file")
	errBackupTruncated = errors.New("the encrypted backup file is truncated")
)

func backupAEAD(key, salt []byte) (cipher.AEAD, error) {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte("spartask backup v1"))
	mac.Write(salt)
	block, err := aes.NewCipher(mac.Sum(nil))
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func chunkNonce(counter uint64, last bool) []byte {
	nonce := make([]byte, 12)
	binary.BigEndian.PutUint64(nonce[3:11], counter)
	if last {
		nonce[11] = 1
	}
	return nonce
}

// backupEncrypter seals everything written to it; Close writes the final chunk.
type backupEncrypter struct {
	w       io.Writer
	aead    cipher.AEAD
	buf     []byte
	counter uint64
	closed  bool
}

func newBackupEncrypter(w io.Writer, key []byte) (*backupEncrypter, error) {
	salt := make([]byte, backupSaltSize)
	if _, err := rand.Read(salt); err != nil {
		return nil, fmt.Errorf("generate backup salt: %w", err)
	}
	aead, err := backupAEAD(key, salt)
	if err != nil {
		return nil, err
	}
	if _, err := io.WriteString(w, backupMagic); err != nil {
		return nil, err
	}
	if _, err := w.Write(salt); err != nil {
		return nil, err
	}
	return &backupEncrypter{w: w, aead: aead, buf: make([]byte, 0, backupChunkSize)}, nil
}

func (e *backupEncrypter) Write(p []byte) (int, error) {
	if e.closed {
		return 0, errors.New("write to a closed backup file")
	}
	n := 0
	for len(p) > 0 {
		// A full chunk is sealed only once more data arrives: the last chunk must carry the flag.
		if len(e.buf) == backupChunkSize {
			if err := e.seal(false); err != nil {
				return n, err
			}
		}
		k := copy(e.buf[len(e.buf):backupChunkSize], p)
		e.buf = e.buf[:len(e.buf)+k]
		p = p[k:]
		n += k
	}
	return n, nil
}

func (e *backupEncrypter) seal(last bool) error {
	sealed := e.aead.Seal(nil, chunkNonce(e.counter, last), e.buf, nil)
	e.counter++
	e.buf = e.buf[:0]
	_, err := e.w.Write(sealed)
	return err
}

func (e *backupEncrypter) Close() error {
	if e.closed {
		return nil
	}
	e.closed = true
	return e.seal(true)
}

// backupDecrypter reads the plaintext of an encrypted backup file.
type backupDecrypter struct {
	r       io.Reader
	aead    cipher.AEAD
	chunk   []byte
	plain   []byte
	counter uint64
	done    bool
}

func newBackupDecrypter(r io.Reader, key []byte) (*backupDecrypter, error) {
	header := make([]byte, len(backupMagic)+backupSaltSize)
	if _, err := io.ReadFull(r, header); err != nil || string(header[:len(backupMagic)]) != backupMagic {
		return nil, errors.New("not an encrypted spartaskctl backup file")
	}
	aead, err := backupAEAD(key, header[len(backupMagic):])
	if err != nil {
		return nil, err
	}
	return &backupDecrypter{r: r, aead: aead, chunk: make([]byte, backupChunkSize+aead.Overhead())}, nil
}

func (d *backupDecrypter) Read(p []byte) (int, error) {
	for len(d.plain) == 0 {
		if d.done {
			return 0, io.EOF
		}
		if err := d.next(); err != nil {
			return 0, err
		}
	}
	n := copy(p, d.plain)
	d.plain = d.plain[n:]
	return n, nil
}

func (d *backupDecrypter) next() error {
	n, err := io.ReadFull(d.r, d.chunk)
	switch {
	case errors.Is(err, io.EOF):
		return errBackupTruncated // the final chunk never came
	case errors.Is(err, io.ErrUnexpectedEOF):
		return d.open(d.chunk[:n], true) // a short chunk can only be the last one
	case err != nil:
		return err
	}
	if d.open(d.chunk, false) == nil {
		return nil
	}
	return d.open(d.chunk, true) // a full chunk is the last one when the data ended on a boundary
}

func (d *backupDecrypter) open(chunk []byte, last bool) error {
	plain, err := d.aead.Open(nil, chunkNonce(d.counter, last), chunk, nil)
	if err != nil {
		return errBackupKey
	}
	d.counter++
	d.plain = plain
	if last {
		d.done = true
		var extra [1]byte
		if n, _ := io.ReadFull(d.r, extra[:]); n > 0 {
			return errors.New("the encrypted backup file has data after its final chunk")
		}
	}
	return nil
}

// writeBackupFile streams write's output to path; with a key it is encrypted and the file is
// named path.enc, so plaintext never touches the disk.
func writeBackupFile(path string, key []byte, write func(io.Writer) error) error {
	if key == nil {
		return streamToFile(path, write)
	}
	return streamToFile(path+encSuffix, func(w io.Writer) error {
		enc, err := newBackupEncrypter(w, key)
		if err != nil {
			return err
		}
		if err := write(enc); err != nil {
			return err
		}
		return enc.Close()
	})
}

type readCloser struct {
	io.Reader
	io.Closer
}

// openBackupFile opens name in a backup directory, decrypting name.enc when present. A backup
// without either file returns an error matching os.ErrNotExist.
func openBackupFile(dir, name string, key []byte) (io.ReadCloser, error) {
	f, err := os.Open(filepath.Join(dir, name+encSuffix))
	if errors.Is(err, os.ErrNotExist) {
		return os.Open(filepath.Join(dir, name))
	}
	if err != nil {
		return nil, err
	}
	if key == nil {
		f.Close()
		return nil, fmt.Errorf("%s is encrypted: BACKUP_ENCRYPTION_KEY is required", name)
	}
	dec, err := newBackupDecrypter(f, key)
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	return readCloser{dec, f}, nil
}

func backupIsEncrypted(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, "spartask.dump"+encSuffix))
	return err == nil
}

// checkBackupKey decrypts the first chunk of an encrypted file to tell a wrong key early.
func checkBackupKey(path string, key []byte) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	dec, err := newBackupDecrypter(f, key)
	if err != nil {
		return err
	}
	if _, err := dec.Read(make([]byte, 1)); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}

// restoreKey finds the key of an encrypted backup: BACKUP_ENCRYPTION_KEY in the environment,
// then in the installed .env, then (interactive restores) a prompt. Nil for a plain backup.
func restoreKey(s stack, dir string, interactive bool) ([]byte, error) {
	if !backupIsEncrypted(dir) {
		return nil, nil
	}
	probe := filepath.Join(dir, "spartask.dump"+encSuffix)
	var candidates []string
	if value := os.Getenv("BACKUP_ENCRYPTION_KEY"); value != "" {
		candidates = append(candidates, value)
	}
	if env, err := readEnvFile(s.path(".env")); err == nil && env.Get("BACKUP_ENCRYPTION_KEY") != "" {
		candidates = append(candidates, env.Get("BACKUP_ENCRYPTION_KEY"))
	}
	for _, candidate := range candidates {
		if checkBackupKey(probe, []byte(candidate)) == nil {
			return []byte(candidate), nil
		}
	}
	if interactive {
		value, err := newPrompter(false).askSecret("BACKUP_ENCRYPTION_KEY of this backup")
		if err != nil {
			return nil, err
		}
		if value != "" {
			if err := checkBackupKey(probe, []byte(value)); err != nil {
				return nil, err
			}
			return []byte(value), nil
		}
	}
	return nil, errors.New("the backup is encrypted and no matching BACKUP_ENCRYPTION_KEY was found: " +
		"set it in the environment (BACKUP_ENCRYPTION_KEY=... spartaskctl restore ...) from your password manager")
}
