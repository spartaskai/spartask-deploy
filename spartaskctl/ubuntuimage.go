package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// The VM boots Ubuntu's cloud image; the release directory always holds the newest point release
// with its SHA256SUMS.
const (
	ubuntuImageBase = "https://cloud-images.ubuntu.com/releases/24.04/release/"
	ubuntuImageName = "ubuntu-24.04-server-cloudimg-amd64.img"
)

// sumFor returns the SHA-256 that a SHA256SUMS file lists for name.
func sumFor(sums []byte, name string) (string, error) {
	scanner := bufio.NewScanner(bytes.NewReader(sums))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 2 && strings.TrimPrefix(fields[1], "*") == name {
			if _, err := hex.DecodeString(fields[0]); err != nil || len(fields[0]) != 64 {
				return "", fmt.Errorf("SHA256SUMS: %q is not a SHA-256 for %s", fields[0], name)
			}
			return strings.ToLower(fields[0]), nil
		}
	}
	return "", fmt.Errorf("SHA256SUMS lists no %s", name)
}

// fetchImage downloads name from base into dir (a copy from an earlier run is kept when it still
// matches) and returns its path after checking it against the release's SHA256SUMS.
func fetchImage(ctx context.Context, client *http.Client, base, name, dir string) (string, error) {
	sums, err := httpGet(ctx, client, base+"SHA256SUMS", 1<<20)
	if err != nil {
		return "", err
	}
	want, err := sumFor(sums, name)
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, name)
	if got, err := fileSHA256(path); err == nil && got == want {
		return path, nil
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	part := path + ".part"
	f, err := os.Create(part)
	if err != nil {
		return "", err
	}
	hash := sha256.New()
	fmt.Printf("Downloading %s\n", base+name)
	getErr := httpDownload(ctx, client, base+name, io.MultiWriter(f, hash), 0)
	closeErr := f.Close()
	if err := errors.Join(getErr, closeErr); err != nil {
		return "", errors.Join(err, removeIfExists(part))
	}
	if got := hex.EncodeToString(hash.Sum(nil)); got != want {
		return "", errors.Join(fmt.Errorf("%s does not match the release's SHA256SUMS (got %s)", name, got), removeIfExists(part))
	}
	if err := os.Rename(part, path); err != nil {
		return "", err
	}
	return path, nil
}

// httpGet returns the body of url, at most limit bytes.
func httpGet(ctx context.Context, client *http.Client, url string, limit int64) ([]byte, error) {
	var body bytes.Buffer
	if err := httpDownload(ctx, client, url, &body, limit); err != nil {
		return nil, err
	}
	return body.Bytes(), nil
}

// httpDownload writes the body of url to w, refusing more than limit bytes (0: no limit), and
// shows the progress of a body of known size.
func httpDownload(ctx context.Context, client *http.Client, url string, w io.Writer, limit int64) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	body := io.Reader(resp.Body)
	if limit > 0 {
		body = io.LimitReader(resp.Body, limit+1)
	}
	p := &progress{total: resp.ContentLength}
	n, err := io.Copy(io.MultiWriter(w, p), body)
	if err != nil {
		return fmt.Errorf("GET %s: %w", url, err)
	}
	if limit > 0 && n > limit {
		return fmt.Errorf("GET %s: larger than %d bytes", url, limit)
	}
	return nil
}

// progress prints how much of a large download (over 64 MB) has arrived, every 10 percent.
type progress struct {
	total, written int64
	shown          int64
}

func (p *progress) Write(b []byte) (int, error) {
	p.written += int64(len(b))
	if p.total > 64<<20 {
		if step := p.written * 10 / p.total; step > p.shown {
			p.shown = step
			fmt.Printf("  %d%% of %d MB\n", step*10, p.total>>20)
		}
	}
	return len(b), nil
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// removeIfExists removes path; a missing file is not an error.
func removeIfExists(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
