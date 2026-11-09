package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net/netip"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"
)

// fakeDocker answers the docker commands an installation and an update run and records them.
// Files named fail-pull and fail-up make the next `docker compose pull` or `up` fail once; the
// code EXPIRED is refused.
const fakeDocker = `#!/bin/sh
d="$FAKE_DOCKER_DIR"
echo "$*" >> "$d/calls.log"
case "$*" in
"compose version --short") echo 2.29.0 ;;
"run --rm -e SPARTASK_INSTALLATION_KEY "*)
	echo "$SPARTASK_INSTALLATION_KEY" >> "$d/keys.log"
	case "$*" in *"-code EXPIRED "*) echo "activation code expired" >&2; exit 1 ;; esac
	printf '%s' '{"license":"lisans","installation_id":"inst-1","organization_id":"org-1","organization_name":"Firma A.Ş.","base_url":"https://spartask.firma.example"}'
	;;
"compose pull")
	echo "pulled SPARTASK_VERSION=$SPARTASK_VERSION" >> "$d/calls.log"
	if [ -e "$d/fail-pull" ]; then rm "$d/fail-pull"; echo "network is unreachable" >&2; exit 1; fi
	;;
"compose up -d --remove-orphans")
	if [ -e "$d/fail-up" ]; then rm "$d/fail-up"; echo "migration failed" >&2; exit 1; fi
	;;
"compose ps -q api") echo cid ;;
"inspect "*) echo "running healthy" ;;
*" license install -f -") cat > "$d/license.in" ;;
*" company create "*) cat > "$d/company.in" ;;
esac
exit 0
`

// installFixture is an extracted spartask-deploy directory and a fake docker on PATH.
func installFixture(t *testing.T) (stack, string) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("the fake docker is a shell script")
	}
	dir := t.TempDir()
	for _, name := range []string{"compose.yml", ".env.example", "marketplace.env.example", filepath.Join("caddy", "Caddyfile")} {
		data, err := os.ReadFile(filepath.Join("..", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "docker"), []byte(fakeDocker), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKE_DOCKER_DIR", bin)
	t.Setenv(adminPasswordVariable, "yeterince-uzun-sifre")
	return stack{dir: dir}, bin
}

// readyServer is a server that passes every preflight check.
func readyServer() preflightProbes {
	return preflightProbes{
		memory:     func() (uint64, error) { return 8 << 30, nil },
		freeDisk:   func(string) (uint64, error) { return 100 << 30, nil },
		dockerRoot: func(context.Context) (string, error) { return "/var/lib/docker", nil },
		listen:     func(int) error { return nil },
		fetch:      func(context.Context, string) (time.Time, error) { return time.Now(), nil },
		lookup: func(context.Context, string) ([]netip.Addr, error) {
			return []netip.Addr{netip.MustParseAddr("203.0.113.10")}, nil
		},
		tags: releaseTags,
		now:  time.Now,
	}
}

// releaseTags are the SparTask image's tags in the tests: 1.4.1 is the latest stable release.
func releaseTags(context.Context, string) ([]string, error) {
	return []string{"1.4.0", "v1.3.9", "1.4.1", "1.5.0-rc1", "latest", "sha-0f3c2a1"}, nil
}

func activationArgs(code string) []string {
	return []string{"-y", "-version", "1.2.3", "-activation", code, "-license-server", "https://accounts.example",
		"-admin-email", "bt@firma.example", "-admin-name", "BT", "-backup-job=false"}
}

func readLines(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Fields(string(data))
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// The images cannot be pulled after the activation: the license is kept in the install state, the
// same command continues without a second activation and creates the company, and afterwards the
// directory counts as installed.
func TestInstallContinuesAfterFailure(t *testing.T) {
	s, bin := installFixture(t)
	if err := os.WriteFile(filepath.Join(bin, "fail-pull"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	err := install(ctx, s, activationArgs("KOD-1"), readyServer())
	if err == nil || !strings.Contains(err.Error(), "continues where it stopped") {
		t.Fatalf("first run: %v", err)
	}
	state, err := readInstallState(s)
	if err != nil {
		t.Fatal(err)
	}
	if state.Activation != nil || state.Company == nil || state.Company.License != "lisans" || state.Company.LicenseInstalled {
		t.Fatalf("state after the failed pull: %+v %+v", state.Activation, state.Company)
	}
	if data, err := os.ReadFile(s.path(installStateFile)); err != nil || strings.Contains(string(data), "yeterince-uzun-sifre") {
		t.Fatalf("the install state must never hold the password: %v", err)
	}

	if err := install(ctx, s, activationArgs("KOD-1"), readyServer()); err != nil {
		t.Fatalf("continued run: %v", err)
	}
	if keys := readLines(t, filepath.Join(bin, "keys.log")); len(keys) != 1 {
		t.Fatalf("activations: %v (the license was already saved)", keys)
	}
	if got := readFile(t, filepath.Join(bin, "license.in")); got != "lisans" {
		t.Fatalf("installed license %q", got)
	}
	if got := readFile(t, filepath.Join(bin, "company.in")); got != "yeterince-uzun-sifre\n" {
		t.Fatalf("company create stdin %q", got)
	}
	if !strings.Contains(readFile(t, filepath.Join(bin, "calls.log")), "company create -id firma-a-s -name Firma A.Ş. -admin-email bt@firma.example -admin-name BT") {
		t.Fatal("the company was not created with the activation's name and administrator")
	}
	if _, err := os.Stat(s.path(installStateFile)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the install state remains after a complete installation: %v", err)
	}
	env, err := loadInstalledEnv(s)
	if err != nil {
		t.Fatal(err)
	}
	if env.Get("SPARTASK_DOMAIN") != "spartask.firma.example" || env.Get("SPARTASK_ROOT_TENANT") != "firma-a-s" || tlsMode(env) != tlsACME {
		t.Fatalf(".env: domain %q, company %q, TLS %q", env.Get("SPARTASK_DOMAIN"), env.Get("SPARTASK_ROOT_TENANT"), env.Get("SPARTASK_TLS"))
	}
	if err := install(ctx, s, activationArgs("KOD-1"), readyServer()); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("a complete installation was installed again: %v", err)
	}
}

// The first code had expired: the installation key is in .env already, so the new code from the
// on-prem page activates the same installation.
func TestInstallContinuesWithNewActivationCode(t *testing.T) {
	s, bin := installFixture(t)
	ctx := context.Background()
	if err := install(ctx, s, activationArgs("EXPIRED"), readyServer()); err == nil || !strings.Contains(err.Error(), "new one on the on-prem page") {
		t.Fatalf("expired code: %v", err)
	}
	if state, err := readInstallState(s); err != nil || state.Activation == nil {
		t.Fatalf("the activation must stay pending: %v", err)
	}
	if err := install(ctx, s, activationArgs("KOD-2"), readyServer()); err != nil {
		t.Fatalf("new code: %v", err)
	}
	keys := readLines(t, filepath.Join(bin, "keys.log"))
	if len(keys) != 2 || keys[0] != keys[1] || len(keys[0]) != 64 {
		t.Fatalf("both codes must be exchanged with the same installation key: %v", keys)
	}
}

// A server that is not ready is refused before anything is written, with every problem at once.
func TestInstallStopsOnPreflightProblems(t *testing.T) {
	s, bin := installFixture(t)
	busy := readyServer()
	busy.listen = func(port int) error {
		if port == 443 {
			return fmt.Errorf("listen tcp :443: %w", syscall.EADDRINUSE)
		}
		return nil
	}
	busy.memory = func() (uint64, error) { return 2 << 30, nil }
	err := install(context.Background(), s, activationArgs("KOD-1"), busy)
	if err == nil || !strings.Contains(err.Error(), "2 problem(s)") {
		t.Fatalf("got %v", err)
	}
	for _, name := range []string{".env", installStateFile} {
		if _, err := os.Stat(s.path(name)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%s was written by a refused installation", name)
		}
	}
	if _, err := os.Stat(filepath.Join(bin, "keys.log")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("the activation code was used by a refused installation")
	}
}

func TestPreflightExplainsProblems(t *testing.T) {
	probes := readyServer()
	probes.listen = func(port int) error { return fmt.Errorf("listen: %w", syscall.EADDRINUSE) }
	probes.freeDisk = func(path string) (uint64, error) { return 3 << 30, nil }
	probes.fetch = func(_ context.Context, url string) (time.Time, error) {
		if strings.Contains(url, "ghcr.io") {
			return time.Time{}, fmt.Errorf("get %s: %w", url, x509.UnknownAuthorityError{})
		}
		return time.Now().Add(-10 * time.Minute), nil
	}
	var out strings.Builder
	o := installOptions{activation: "kod", licenseServer: "https://accounts.example", tls: tlsACME}
	err := preflight(context.Background(), &out, stack{dir: "/opt/spartask"}, preflightTargets(o), probes)
	if err == nil {
		t.Fatal("a busy, small server with a wrong clock passed")
	}
	report := out.String()
	for _, want := range []string{"port 80: another program", "port 443: another program", "/opt/spartask: 3.0 GB",
		"/var/lib/docker: 3.0 GB", "replaces certificates", "clock: this server's clock is 10m0s off",
		"SparTask license service is reachable", "Let's Encrypt is reachable", "Docker Hub"} {
		if !strings.Contains(report, want) {
			t.Errorf("report lacks %q:\n%s", want, report)
		}
	}
	out.Reset()
	if err := preflight(context.Background(), &out, stack{dir: "/opt/spartask"}, preflightTargets(installOptions{}), readyServer()); err != nil {
		t.Fatalf("a ready server failed: %v\n%s", err, out.String())
	}
	if strings.Contains(out.String(), "license service") || strings.Contains(out.String(), "Let's Encrypt") {
		t.Fatalf("a platform installation with certificate files needs neither:\n%s", out.String())
	}
}

// writeCertificate writes a self-signed certificate for names into certs/.
func writeCertificate(t *testing.T, s stack, notAfter time.Time, names ...string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: names[0]}, DNSNames: names,
		NotBefore: notAfter.AddDate(-1, 0, 0), NotAfter: notAfter}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(s.path("certs"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.path("certs", "cert.pem"), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.path("certs", "key.pem"), pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestCheckCertificate(t *testing.T) {
	s := stack{dir: t.TempDir()}
	now := time.Now()
	writeCertificate(t, s, now.AddDate(0, 6, 0), "spartask.firma.example")
	if err := checkCertificate(s, []string{"spartask.firma.example"}, now); err != nil {
		t.Fatalf("matching certificate: %v", err)
	}
	if err := checkCertificate(s, []string{"baska.firma.example"}, now); err == nil || !strings.Contains(err.Error(), "does not cover baska.firma.example") {
		t.Fatalf("another address: %v", err)
	}
	if err := checkCertificate(s, []string{"spartask.firma.example"}, now.AddDate(1, 0, 0)); err == nil || !strings.Contains(err.Error(), "not now") {
		t.Fatalf("expired: %v", err)
	}

	writeCertificate(t, s, now.AddDate(0, 6, 0), "spartask.example", "*.spartask.example")
	if err := checkCertificate(s, []string{"spartask.example", "*.spartask.example"}, now); err != nil {
		t.Fatalf("platform certificate: %v", err)
	}
	writeCertificate(t, s, now.AddDate(0, 6, 0), "spartask.example")
	if err := checkCertificate(s, []string{"spartask.example", "*.spartask.example"}, now); err == nil {
		t.Fatal("a certificate without the wildcard passed for company subdomains")
	}
	if err := os.WriteFile(s.path("certs", "key.pem"), []byte("not a key"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := checkCertificate(s, []string{"spartask.example"}, now); err == nil {
		t.Fatal("a broken key passed")
	}
}

// The company's certificate files are checked against the address the activation brings, before
// the stack starts; Let's Encrypt needs the address's DNS record.
func TestCheckServedAddress(t *testing.T) {
	s := stack{dir: t.TempDir()}
	env := &envFile{}
	env.Set("SPARTASK_DOMAIN", "spartask.firma.example")
	env.Set("SPARTASK_SITES", "spartask.firma.example")
	env.Set("SPARTASK_TLS", certificateFilesValue)
	writeCertificate(t, s, time.Now().AddDate(0, 6, 0), "spartask.firma.example")
	noDNS := func(context.Context, string) ([]netip.Addr, error) { return nil, errors.New("no such host") }
	if err := checkServedAddress(context.Background(), s, env, noDNS, time.Now()); err != nil {
		t.Fatalf("certificate files need no public DNS record: %v", err)
	}
	env.Set("SPARTASK_TLS", "bt@firma.example")
	if err := checkServedAddress(context.Background(), s, env, noDNS, time.Now()); err == nil || !strings.Contains(err.Error(), "A record") {
		t.Fatalf("Let's Encrypt without DNS: %v", err)
	}
}

// An update downloads the new release while the old one runs, then backs up and switches; when
// the new release does not start, .env goes back and the pre-update backup is named for a
// database its migrations changed.
func TestUpdateDownloadsFirstAndNamesTheBackup(t *testing.T) {
	s, bin := installFixture(t)
	env, _, err := buildEnv(s, installOptions{version: "1.0.0", tls: tlsFiles, domain: "spartask.example"})
	if err != nil {
		t.Fatal(err)
	}
	if err := env.Save(s.path(".env")); err != nil {
		t.Fatal(err)
	}
	if err := prepareDirectories(s, installOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := runUpdate(context.Background(), s, []string{"1.1.0"}); err != nil {
		t.Fatal(err)
	}
	calls := readFile(t, filepath.Join(bin, "calls.log"))
	pulled, dumped := strings.Index(calls, "pulled SPARTASK_VERSION=1.1.0"), strings.Index(calls, "pg_dump")
	if pulled < 0 || dumped < 0 || pulled > dumped {
		t.Fatalf("the new release must be downloaded before the backup:\n%s", calls)
	}
	if env, err := loadInstalledEnv(s); err != nil || env.Get("SPARTASK_VERSION") != "1.1.0" {
		t.Fatalf("version after the update: %v", err)
	}

	if err := os.WriteFile(filepath.Join(bin, "fail-up"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	err = runUpdate(context.Background(), s, []string{"1.2.0"})
	if err == nil || !strings.Contains(err.Error(), "spartaskctl restore "+s.path("backups")) {
		t.Fatalf("failed update: %v", err)
	}
	if env, err := loadInstalledEnv(s); err != nil || env.Get("SPARTASK_VERSION") != "1.1.0" {
		t.Fatalf(".env must be back on 1.1.0: %v", err)
	}
}

// Without -version the installation takes the latest stable release and pins it in .env.
func TestInstallPinsTheLatestRelease(t *testing.T) {
	s, _ := installFixture(t)
	args := activationArgs("KOD-1")
	args = slices.Delete(args, 1, 3) // drop -version 1.2.3
	if slices.Contains(args, "-version") {
		t.Fatalf("args still pin a version: %v", args)
	}
	if err := install(context.Background(), s, args, readyServer()); err != nil {
		t.Fatal(err)
	}
	env, err := loadInstalledEnv(s)
	if err != nil || env.Get("SPARTASK_VERSION") != "1.4.1" {
		t.Fatalf("SPARTASK_VERSION = %q (%v), want the latest stable release 1.4.1", env.Get("SPARTASK_VERSION"), err)
	}
}

// update without a version moves to the latest release, and does nothing when it runs already.
func TestUpdateToTheLatestRelease(t *testing.T) {
	s, bin := installFixture(t)
	env, _, err := buildEnv(s, installOptions{version: "1.4.0", tls: tlsFiles, domain: "spartask.example"})
	if err != nil {
		t.Fatal(err)
	}
	if err := env.Save(s.path(".env")); err != nil {
		t.Fatal(err)
	}
	if err := prepareDirectories(s, installOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := update(context.Background(), s, nil, releaseTags); err != nil {
		t.Fatal(err)
	}
	if env, err := loadInstalledEnv(s); err != nil || env.Get("SPARTASK_VERSION") != "1.4.1" {
		t.Fatalf("after update: %v", err)
	}
	calls := len(readFile(t, filepath.Join(bin, "calls.log")))
	if err := update(context.Background(), s, nil, releaseTags); err != nil {
		t.Fatal(err)
	}
	if len(readFile(t, filepath.Join(bin, "calls.log"))) != calls {
		t.Fatal("an installation on the latest release was updated again")
	}
	unreachable := func(context.Context, string) ([]string, error) { return nil, errors.New("no network") }
	if err := update(context.Background(), s, nil, unreachable); err == nil || !strings.Contains(err.Error(), "spartaskctl update <version>") {
		t.Fatalf("unreachable registry: %v", err)
	}
}
