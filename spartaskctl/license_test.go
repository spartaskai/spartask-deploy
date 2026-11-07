package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLicenseUsage(t *testing.T) {
	for _, args := range [][]string{nil, {"show", "now"}, {"install"}, {"delete"}} {
		err := runLicense(context.Background(), stack{dir: t.TempDir()}, args)
		if err == nil || !strings.Contains(err.Error(), "usage: spartaskctl license show") {
			t.Errorf("%v: %v", args, err)
		}
	}
}

func TestLicenseInstallRefusesLargeFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "license.jws")
	if err := os.WriteFile(path, make([]byte, maxLicenseFile+1), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readLicenseFile(path); err == nil {
		t.Fatal("a file larger than a license was read")
	}
	if _, err := readLicenseFile(filepath.Join(t.TempDir(), "missing.jws")); err == nil {
		t.Fatal("a missing file was read")
	}
}
