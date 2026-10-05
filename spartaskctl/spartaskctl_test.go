package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestEnvFileKeepsCommentsAndOrder(t *testing.T) {
	env := parseEnv([]byte("# comment\r\nA=1\n\nB=\"two words\"\nA=override\n"))
	if env.Get("A") != "override" || env.Get("B") != "two words" || env.Get("missing") != "" {
		t.Fatalf("unexpected values A=%q B=%q", env.Get("A"), env.Get("B"))
	}
	env.Set("A", "3")
	env.Set("C", "/certs/cert.pem /certs/key.pem")
	want := "# comment\nA=3\n\nB=\"two words\"\nA=3\nC=/certs/cert.pem /certs/key.pem\n"
	if got := string(env.Bytes()); got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

func TestSecretFileIsPrivate(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	if err := parseEnv([]byte("X=1\n")).Save(path); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v, want 0600", info.Mode().Perm())
	}
}

// The templates shipped next to spartaskctl must contain every key install sets, so .env keeps
// the documented layout instead of growing a tail of appended keys.
func TestBuildEnvFillsTemplates(t *testing.T) {
	s := stack{dir: ".."}
	env, mp, err := buildEnv(s, installOptions{domain: "spartask.ai", version: "1.2.3", tls: "files", cloudflare: true,
		marketplace: true, marketplaceDomain: "marketplace.spartask.ai", marketplaceVer: "1.2.3", geminiKey: "g"})
	if err != nil {
		t.Fatal(err)
	}
	template, err := os.ReadFile("../.env.example")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := keysOf(env.Bytes()), keysOf(template); !slices.Equal(got, want) {
		t.Fatalf(".env keys differ from .env.example:\n got %v\nwant %v", got, want)
	}
	mpTemplate, err := os.ReadFile("../marketplace.env.example")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := keysOf(mp.Bytes()), keysOf(mpTemplate); !slices.Equal(got, want) {
		t.Fatalf("marketplace.env keys differ from template:\n got %v\nwant %v", got, want)
	}
	for _, key := range []string{"DB_PASSWORD", "SECRET_KEY", "WEB_AUTH_JWT_SECRET", "NATS_AUTH_TOKEN", "MARKETPLACE_DB_PASSWORD", "MARKETPLACE_PUBLIC_KEY", "MARKETPLACE_SSO_SHARED_SECRET"} {
		if len(env.Get(key)) < 32 {
			t.Fatalf("%s not generated: %q", key, env.Get(key))
		}
	}
	if env.Get("MARKETPLACE_SSO_SHARED_SECRET") != mp.Get("SSO_SHARED_SECRET") {
		t.Fatal("engine and marketplace must share the SSO secret")
	}
	if env.Get("COMPOSE_FILE") != "compose.yml:compose.marketplace.yml" || env.Get("SPARTASK_DOMAIN") != "spartask.ai" {
		t.Fatalf("unexpected stack settings: %q %q", env.Get("COMPOSE_FILE"), env.Get("SPARTASK_DOMAIN"))
	}
	if !strings.Contains(env.Get("CADDY_TRUSTED_PROXIES"), "173.245.48.0/20") {
		t.Fatal("Cloudflare ranges must be trusted when proxied by Cloudflare")
	}
	other, _, err := buildEnv(s, installOptions{domain: "acme.local", version: "1.2.3", tls: "internal"})
	if err != nil {
		t.Fatal(err)
	}
	if other.Get("SECRET_KEY") == env.Get("SECRET_KEY") || other.Get("SPARTASK_TLS") != "internal" || other.Get("CADDY_TRUSTED_PROXIES") != "private_ranges" {
		t.Fatal("each installation needs its own secrets and settings")
	}
}

func TestPruneBackupsKeepsNewest(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"20260101T000000Z", "20260103T000000Z", "20260102T000000Z", "not-a-backup"} {
		if err := os.Mkdir(filepath.Join(root, name), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := pruneBackups(root, 2); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(root)
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if !slices.Equal(names, []string{"20260102T000000Z", "20260103T000000Z", "not-a-backup"}) {
		t.Fatalf("unexpected remaining entries: %v", names)
	}
}

func TestTarRoundTripAndUnsafePaths(t *testing.T) {
	src := t.TempDir()
	if err := os.MkdirAll(filepath.Join(src, "tenants", "acme"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "tenants", "acme", "plugin.wasm"), []byte("wasm"), 0o644); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(t.TempDir(), "storage.tar.gz")
	if err := tarDirectory(src, archive); err != nil {
		t.Fatal(err)
	}
	dst := t.TempDir()
	if err := untar(archive, dst); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dst, "tenants", "acme", "plugin.wasm"))
	if err != nil || string(data) != "wasm" {
		t.Fatalf("round trip failed: %q %v", data, err)
	}
	// config archive skips missing paths
	if err := tarPaths(src, []string{"tenants", "missing"}, filepath.Join(t.TempDir(), "c.tar.gz")); err != nil {
		t.Fatal(err)
	}
}

func TestReorderFlagsAcceptsFlagsAfterVersion(t *testing.T) {
	got := reorderFlags([]string{"1.2.0", "-no-backup", "-marketplace-version", "1.1.0"})
	want := []string{"-no-backup", "-marketplace-version", "1.1.0", "1.2.0"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestDomainAndVersionValidation(t *testing.T) {
	for _, ok := range []string{"spartask.ai", "app.example.com.tr", "acme.local"} {
		if !domainPattern.MatchString(ok) {
			t.Fatalf("%s should be valid", ok)
		}
	}
	for _, bad := range []string{"https://spartask.ai", "spartask.ai:443", "localhost", "*.spartask.ai"} {
		if domainPattern.MatchString(bad) {
			t.Fatalf("%s should be rejected", bad)
		}
	}
	if !versionPattern.MatchString("1.2.3-rc.1") || versionPattern.MatchString("latest") {
		t.Fatal("version pattern")
	}
}

func TestComposeRequiresNATSTokenAndSeparatesMarketplace(t *testing.T) {
	compose, err := os.ReadFile("../compose.yml")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"\"--auth\", \"${NATS_AUTH_TOKEN:?", "SPARTASK_PLATFORM_CIDRS: ${SPARTASK_SUBNET"} {
		if !strings.Contains(string(compose), want) {
			t.Fatalf("compose.yml misses %q", want)
		}
	}
	marketplace, err := os.ReadFile("../compose.marketplace.yml")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(marketplace), "internal: true") || strings.Contains(string(marketplace), "networks: [spartask]") {
		t.Fatal("marketplace database must be on its own internal network")
	}
}
