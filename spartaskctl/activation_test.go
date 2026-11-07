package main

import (
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
	o := installOptions{version: "1.2.3", tls: "acme", licenseServer: "https://accounts.example", admin: adminAccount{email: "bt@firma.example", name: "BT", password: "yeterince-uzun-sifre"}}
	env, _, err := buildEnv(stack{dir: ".."}, o)
	if err != nil {
		t.Fatal(err)
	}
	answer := activationAnswer{License: "lisans", InstallationID: "inst-1", OrganizationID: "org-1", OrganizationName: "Firma A.Ş.",
		BaseURL: "https://spartask.firma.example"}
	plan, err := applyActivation(env, &o, answer)
	if err != nil {
		t.Fatal(err)
	}
	if plan.id != "firma-a-s" || plan.name != "Firma A.Ş." || plan.address != "https://spartask.firma.example" || plan.license != "lisans" || plan.admin != o.admin {
		t.Fatalf("plan: %+v", plan)
	}
	want := map[string]string{"SPARTASK_DOMAIN": "spartask.firma.example", "SPARTASK_SITES": "spartask.firma.example",
		"SPARTASK_ROOT_TENANT": "firma-a-s", "SPARTASK_SIGNUP_MODE": "closed", "SPARTASK_TLS": "bt@firma.example",
		"SPARTASK_PLATFORM_TENANT_IDS": "firma-a-s", "SPARTASK_LICENSE_SERVER": "https://accounts.example"}
	for key, value := range want {
		if got := env.Get(key); got != value {
			t.Errorf("%s = %q, want %q", key, got, value)
		}
	}
	if o.domain != "spartask.firma.example" || len(env.Get(installationKeyVariable)) != 64 {
		t.Fatalf("domain %q, key %q", o.domain, env.Get(installationKeyVariable))
	}
	template, err := os.ReadFile("../.env.example")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := keysOf(env.Bytes()), keysOf(template); !slices.Equal(got, want) {
		t.Fatalf(".env keys differ from .env.example:\n got %v\nwant %v", got, want)
	}

	o.companyID = "Kötü"
	if _, err := applyActivation(env, &o, answer); err == nil {
		t.Fatal("an invalid -company-id was accepted")
	}
	o.companyID = ""
	answer.BaseURL = "https://spartask.firma.example:8443"
	if _, err := applyActivation(env, &o, answer); err == nil {
		t.Fatal("an address with a port was accepted")
	}
}
