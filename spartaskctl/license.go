package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
)

const licenseUsage = `usage: spartaskctl license show             this installation's key fingerprint and license
       spartaskctl license install <file>   install the license the license service issued for it
       spartaskctl license renew            renew the license with the license service now
                                            (SPARTASK_LICENSE_SERVER; the scheduler renews by itself)`

// maxLicenseFile bounds the license file handed to spartask-admin.
const maxLicenseFile = 64 << 10

// runLicense shows, installs or renews the installation license through spartask-admin in the
// api container. The license service binds the license to the key fingerprint "show" prints
// (SPARTASK_INSTALLATION_KEY); until one is installed no company can be created.
func runLicense(ctx context.Context, s stack, args []string) error {
	switch {
	case len(args) == 1 && (args[0] == "show" || args[0] == "renew"):
		return licenseAdmin(s.composeStream(ctx, nil, os.Stdout, "exec", "-T", "api", "/app/bin/spartask-admin", "license", args[0]))
	case len(args) == 2 && args[0] == "install":
		data, err := readLicenseFile(args[1])
		if err != nil {
			return err
		}
		return licenseAdmin(s.composeStream(ctx, bytes.NewReader(data), os.Stdout,
			"exec", "-T", "api", "/app/bin/spartask-admin", "license", "install", "-f", "-"))
	}
	return errors.New(licenseUsage)
}

// readLicenseFile reads a license file within maxLicenseFile.
func readLicenseFile(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.Size() > maxLicenseFile {
		return nil, fmt.Errorf("%s is larger than %d bytes: not a license file", path, maxLicenseFile)
	}
	return os.ReadFile(path)
}

// licenseAdmin explains the error of a release whose spartask-admin has no license command.
func licenseAdmin(err error) error {
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "exit status 2") {
		return fmt.Errorf("this Spartask release has no installation license; update to a release that includes it (%w)", err)
	}
	return err
}
