package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSumFor(t *testing.T) {
	a, b := strings.Repeat("a", 64), strings.Repeat("B", 64)
	sums := []byte(a + " *ubuntu.img\n" + b + "  other.img\n")
	if got, err := sumFor(sums, "ubuntu.img"); err != nil || got != a {
		t.Fatalf("binary-mode line: %s %v", got, err)
	}
	if got, err := sumFor(sums, "other.img"); err != nil || got != strings.ToLower(b) {
		t.Fatalf("text-mode line: %s %v", got, err)
	}
	if _, err := sumFor(sums, "missing.img"); err == nil {
		t.Fatal("missing name accepted")
	}
	if _, err := sumFor([]byte("abc *ubuntu.img\n"), "ubuntu.img"); err == nil {
		t.Fatal("short sum accepted")
	}
}

func TestFetchImage(t *testing.T) {
	image := []byte(strings.Repeat("cloud image ", 1000))
	sum := sha256.Sum256(image)
	sums := hex.EncodeToString(sum[:]) + " *ubuntu.img\n"
	downloads := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/release/SHA256SUMS":
			w.Write([]byte(sums))
		case "/release/ubuntu.img":
			downloads++
			w.Write(image)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	dir := t.TempDir()
	ctx := context.Background()

	path, err := fetchImage(ctx, srv.Client(), srv.URL+"/release/", "ubuntu.img", dir)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != string(image) {
		t.Fatalf("downloaded %d bytes: %v", len(got), err)
	}
	if _, err := fetchImage(ctx, srv.Client(), srv.URL+"/release/", "ubuntu.img", dir); err != nil || downloads != 1 {
		t.Fatalf("a matching copy was downloaded again (%d downloads): %v", downloads, err)
	}

	// A newer point release: the kept copy no longer matches, and a corrupt download is refused.
	sums = strings.Repeat("0", 64) + " *ubuntu.img\n"
	if _, err := fetchImage(ctx, srv.Client(), srv.URL+"/release/", "ubuntu.img", dir); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("mismatch: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "ubuntu.img.part")); !os.IsNotExist(err) {
		t.Fatalf("partial download left behind: %v", err)
	}
	if _, err := fetchImage(ctx, srv.Client(), srv.URL+"/missing/", "ubuntu.img", dir); err == nil {
		t.Fatal("missing release accepted")
	}
}
