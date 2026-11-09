package main

import (
	"bufio"
	"io"
	"os"
	"slices"
	"strings"
	"testing"
)

func TestCompanyIDFromName(t *testing.T) {
	for name, want := range map[string]string{
		"Acme Yazılım A.Ş.":       "acme-yazilim-a-s",
		"  ÇAĞDAŞ  İnşaat  ":      "cagdas-insaat",
		"Öz-Ünlü Gıda (2026)":     "oz-unlu-gida-2026",
		"***":                     "",
		strings.Repeat("a", 50):   strings.Repeat("a", 43),
		strings.Repeat("ab ", 20): strings.TrimRight(strings.Repeat("ab-", 15)[:43], "-"),
	} {
		if got := companyIDFromName(name); got != want {
			t.Errorf("%q: got %q, want %q", name, got, want)
		}
	}
}

func TestAddressDomain(t *testing.T) {
	if got, err := addressDomain("https://SparTask.Firma.example/"); err != nil || got != "spartask.firma.example" {
		t.Fatalf("got %q %v", got, err)
	}
	for _, bad := range []string{"http://spartask.firma.example", "https://spartask.firma.example:8443", "https://spartask.firma.example/app", "https://localhost", ""} {
		if _, err := addressDomain(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestLicenseServerURL(t *testing.T) {
	if got, err := licenseServerURL(" https://accounts.example/ "); err != nil || got != "https://accounts.example" {
		t.Fatalf("got %q %v", got, err)
	}
	if got, err := licenseServerURL("http://localhost:8090"); err != nil || got != "http://localhost:8090" {
		t.Fatalf("local: %q %v", got, err)
	}
	for _, bad := range []string{"http://accounts.example", "accounts.example", "https://accounts.example?x=1"} {
		if _, err := licenseServerURL(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestAdminPasswordNonInteractive(t *testing.T) {
	p := newPrompter(true)
	t.Setenv(adminPasswordVariable, "kısa")
	if _, err := askAdminPassword(p); err == nil {
		t.Fatal("a short password was accepted")
	}
	t.Setenv(adminPasswordVariable, "yeterince-uzun-sifre")
	if got, err := askAdminPassword(p); err != nil || got != "yeterince-uzun-sifre" {
		t.Fatalf("got %q %v", got, err)
	}
}

// An activated installation serves one company at its registered address: its .env keeps the
// template's layout and sets the single address, the company and closed sign-up.
func TestApplyActivation(t *testing.T) {
	o := installOptions{version: "1.2.3", tls: tlsACME, admin: adminAccount{email: "bt@firma.example", name: "BT", password: "yeterince-uzun-sifre"}}
	env, _, err := buildEnv(stack{dir: ".."}, o)
	if err != nil {
		t.Fatal(err)
	}
	a := pendingActivation{Code: "kod", LicenseServer: "https://accounts.example", AdminEmail: o.admin.email, AdminName: o.admin.name}
	answer := activationAnswer{License: "lisans", InstallationID: "inst-1", OrganizationID: "org-1", OrganizationName: "Firma A.Ş.",
		BaseURL: "https://spartask.firma.example", MarketplaceURL: "https://marketplace.example", MarketplaceID: "onprem.inst-1"}
	company, err := applyActivation(env, a, answer)
	if err != nil {
		t.Fatal(err)
	}
	want := companyState{ID: "firma-a-s", Name: "Firma A.Ş.", Address: "https://spartask.firma.example",
		AdminEmail: "bt@firma.example", AdminName: "BT", License: "lisans"}
	if *company != want {
		t.Fatalf("company: %+v", company)
	}
	wantEnv := map[string]string{"SPARTASK_DOMAIN": "spartask.firma.example", "SPARTASK_SITES": "spartask.firma.example",
		"SPARTASK_ROOT_TENANT": "firma-a-s", "SPARTASK_SIGNUP_MODE": "closed", "SPARTASK_TLS": "bt@firma.example",
		"SPARTASK_PLATFORM_TENANT_IDS": "firma-a-s", "SPARTASK_LICENSE_SERVER": "https://accounts.example",
		"SPARTASK_MARKETPLACE_COMPANY": "onprem.inst-1", "SPARTASK_MARKETPLACE_URL": "https://marketplace.example",
		"MARKETPLACE_API_URL": "https://marketplace.example", "MARKETPLACE_ALLOWED_ORIGINS": "https://marketplace.example"}
	for key, value := range wantEnv {
		if got := env.Get(key); got != value {
			t.Errorf("%s = %q, want %q", key, got, value)
		}
	}
	if len(env.Get(installationKeyVariable)) != 64 || tlsMode(env) != tlsACME {
		t.Fatalf("key %q, TLS %q", env.Get(installationKeyVariable), tlsMode(env))
	}
	template, err := os.ReadFile("../.env.example")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := keysOf(env.Bytes()), keysOf(template); !slices.Equal(got, want) {
		t.Fatalf(".env keys differ from .env.example:\n got %v\nwant %v", got, want)
	}

	// -marketplace-url (already in .env) chooses another marketplace; the license service names an https one.
	env.Set("SPARTASK_MARKETPLACE_URL", "https://magaza.firma.example")
	if _, err := applyActivation(env, a, answer); err != nil || env.Get("SPARTASK_MARKETPLACE_URL") != "https://magaza.firma.example" {
		t.Fatalf("-marketplace-url: %q %v", env.Get("SPARTASK_MARKETPLACE_URL"), err)
	}
	env.Set("SPARTASK_MARKETPLACE_URL", "")
	answer.MarketplaceURL = "http://marketplace.example"
	if _, err := applyActivation(env, a, answer); err == nil {
		t.Fatal("a plain http marketplace was accepted")
	}
	answer.MarketplaceURL = "https://marketplace.example"

	a.CompanyID = "Kötü"
	if _, err := applyActivation(env, a, answer); err == nil {
		t.Fatal("an invalid -company-id was accepted")
	}
	a.CompanyID = ""
	answer.BaseURL = "https://spartask.firma.example:8443"
	if _, err := applyActivation(env, a, answer); err == nil {
		t.Fatal("an address with a port was accepted")
	}
}

// An activated installation gets its certificate from Let's Encrypt unless the company's own
// certificate files are chosen; the other modes belong to the platform server.
func TestActivationTLS(t *testing.T) {
	p := newPrompter(true)
	for given, want := range map[string]string{"": tlsACME, tlsACME: tlsACME, tlsFiles: tlsFiles} {
		if got, err := askActivationTLS(p, given); err != nil || got != want {
			t.Errorf("%q: got %q %v", given, got, err)
		}
	}
	if _, err := askActivationTLS(p, tlsInternal); err == nil {
		t.Fatal("internal TLS was accepted for an activated installation")
	}
	interactive := &prompter{in: bufio.NewReader(strings.NewReader("2\n")), out: io.Discard}
	if got, err := askActivationTLS(interactive, ""); err != nil || got != tlsFiles {
		t.Fatalf("choice 2: %q %v", got, err)
	}
}
