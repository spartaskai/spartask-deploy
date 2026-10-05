package main

import (
	"bytes"
	"context"
	"errors"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// installedStack is a directory with the package templates and the given .env.
func installedStack(t *testing.T, env string) stack {
	t.Helper()
	s := stack{dir: t.TempDir()}
	for _, name := range []string{".env.example", "marketplace.env.example"} {
		data, err := os.ReadFile(filepath.Join("..", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(s.path(name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(s.path(".env"), []byte(env), 0o600); err != nil {
		t.Fatal(err)
	}
	return s
}

// An installation from an older package: no NATS token, no platform API settings.
const olderEnv = `# Spartask
COMPOSE_FILE=compose.yml
COMPOSE_PROJECT_NAME=spartask
SPARTASK_IMAGE=ghcr.io/spartaskai/spartask
SPARTASK_VERSION=1.0.0
SPARTASK_SUBNET=172.30.10.0/24
SPARTASK_DOMAIN=spartask.ai
SPARTASK_TLS=/certs/cert.pem /certs/key.pem
CADDY_TRUSTED_PROXIES=private_ranges
DB_USER=spartask
DB_NAME=spartask
DB_PASSWORD=existing-db-password
SECRET_KEY=keep-this-encryption-key-forever-0001
WEB_AUTH_JWT_SECRET=existing-session-secret-0123456789abcdef
GEMINI_MODEL=gemini-2.5-flash
GEMINI_API_KEY=
INVITATION_SMTP_HOST=
INVITATION_SMTP_PORT=2525
INVITATION_TTL=72h
INVITATION_HOURLY_LIMIT=50
MARKETPLACE_SSO_ENABLED=false
PLATFORM_EVENTS_WEBHOOK_URL=
MCP_ENABLED=false
MCP_TOKEN_ISSUANCE_ENABLED=false
BACKUP_KEEP=14
LEGACY_SETTING=1
`

func TestEveryTemplateSettingIsDocumented(t *testing.T) {
	for _, name := range []string{".env.example", "marketplace.env.example"} {
		rules, err := readRules(filepath.Join("..", name))
		if err != nil {
			t.Fatal(err)
		}
		for _, rule := range rules {
			if len(rule.Description) == 0 {
				t.Errorf("%s: %s has no explanation comment", name, rule.Key)
			}
			if rule.InstallOnly && rule.Generate == 0 {
				t.Errorf("%s: %s is install-only but not generated", name, rule.Key)
			}
		}
	}
}

func TestPrepareConfigUpgradesOlderInstallation(t *testing.T) {
	s := installedStack(t, olderEnv)
	env, err := prepareConfig(s)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := readEnvFile(s.path(".env"))
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"NATS_AUTH_TOKEN", "SPARTASK_PLATFORM_API_TOKEN"} {
		if len(saved.Get(key)) != 64 || env.Get(key) != saved.Get(key) {
			t.Fatalf("%s not generated: %q", key, saved.Get(key))
		}
	}
	keep := map[string]string{
		"SECRET_KEY": "keep-this-encryption-key-forever-0001", "DB_PASSWORD": "existing-db-password",
		"SPARTASK_DOMAIN": "spartask.ai", "LEGACY_SETTING": "1", "SPARTASK_EGRESS_ALLOW_PRIVATE": "false",
		"MARKETPLACE_FRONT_SUBNET": "172.30.11.0/24",
	}
	for key, want := range keep {
		if saved.Get(key) != want {
			t.Fatalf("%s = %q, want %q", key, saved.Get(key), want)
		}
	}
	// New settings land next to their template neighbours, with their explanation.
	text := string(saved.Bytes())
	if !strings.Contains(text, "SPARTASK_SUBNET=172.30.10.0/24\n# Marketplace ön ağı") {
		t.Fatalf("new setting not placed after its neighbour:\n%s", text)
	}
	if strings.Contains(text, "#@") {
		t.Fatal("template rules must not be copied into .env")
	}
	backup, err := os.ReadFile(s.path(".env.bak"))
	if err != nil || string(backup) != olderEnv {
		t.Fatalf("previous .env must be kept as .env.bak: %v", err)
	}
	// A second run changes nothing; generated values stay.
	token := saved.Get("NATS_AUTH_TOKEN")
	if _, err := prepareConfig(s); err != nil {
		t.Fatal(err)
	}
	again, err := readEnvFile(s.path(".env"))
	if err != nil {
		t.Fatal(err)
	}
	if again.Get("NATS_AUTH_TOKEN") != token || string(again.Bytes()) != text {
		t.Fatal("an up-to-date .env must not change")
	}
}

func TestPrepareConfigStopsOnMissingRequiredSettings(t *testing.T) {
	broken := strings.Replace(olderEnv, "SPARTASK_DOMAIN=spartask.ai", "SPARTASK_DOMAIN=", 1)
	broken = strings.Replace(broken, "SECRET_KEY=keep-this-encryption-key-forever-0001", "SECRET_KEY=", 1)
	broken = strings.Replace(broken, "MCP_ENABLED=false", "MCP_ENABLED=yes", 1)
	s := installedStack(t, broken)
	cfg, err := checkInstalledConfig(s)
	if err != nil {
		t.Fatal(err)
	}
	missing := map[string]bool{}
	for _, rule := range cfg.envResult.Missing {
		missing[rule.Key] = true
	}
	// SECRET_KEY is never generated on an existing installation: stored passwords depend on it.
	if !missing["SPARTASK_DOMAIN"] || !missing["SECRET_KEY"] || len(cfg.envResult.Invalid) != 1 {
		t.Fatalf("unexpected result: missing %v invalid %v", missing, cfg.envResult.Invalid)
	}
	if _, err := prepareConfig(s); err == nil {
		t.Fatal("missing required settings must stop update/restart")
	}
	data, err := os.ReadFile(s.path(".env"))
	if err != nil || string(data) != broken {
		t.Fatal(".env must stay untouched when settings are missing")
	}
}

func TestConditionalRules(t *testing.T) {
	withSMTP := strings.Replace(olderEnv, "INVITATION_SMTP_HOST=", "INVITATION_SMTP_HOST=smtp-relay.brevo.com", 1)
	cfg, err := checkInstalledConfig(installedStack(t, withSMTP))
	if err != nil {
		t.Fatal(err)
	}
	if !hasRule(cfg.envResult.Missing, "INVITATION_SMTP_FROM") || !hasRule(cfg.envResult.Recommended, "INVITATION_SMTP_USER") {
		t.Fatalf("SMTP host needs a sender: missing %v", cfg.envResult.Missing)
	}
	cfg, err = checkInstalledConfig(installedStack(t, olderEnv))
	if err != nil {
		t.Fatal(err)
	}
	if hasRule(cfg.envResult.Missing, "INVITATION_SMTP_FROM") || hasRule(cfg.envResult.Missing, "MARKETPLACE_DOMAIN") {
		t.Fatal("conditional settings must not be required when their condition is off")
	}
	if !hasRule(cfg.envResult.Recommended, "INVITATION_SMTP_HOST") || !hasRule(cfg.envResult.Recommended, "SPARTASK_PLATFORM_TENANT_IDS") {
		t.Fatalf("recommended settings not reported: %v", cfg.envResult.Recommended)
	}
	withMarketplace := strings.Replace(olderEnv, "COMPOSE_FILE=compose.yml", "COMPOSE_FILE=compose.yml:compose.marketplace.yml", 1)
	cfg, err = checkInstalledConfig(installedStack(t, withMarketplace))
	if err != nil {
		t.Fatal(err)
	}
	if !hasRule(cfg.envResult.Missing, "MARKETPLACE_DOMAIN") || !hasRule(cfg.envResult.Missing, "MARKETPLACE_DB_PASSWORD") ||
		!strings.Contains(strings.Join(cfg.envResult.Invalid, " "), "marketplace.env yok") {
		t.Fatalf("marketplace settings not checked: %v %v", cfg.envResult.Missing, cfg.envResult.Invalid)
	}
}

func hasRule(rules []envRule, key string) bool {
	for _, rule := range rules {
		if rule.Key == key {
			return true
		}
	}
	return false
}

func TestAdvisors(t *testing.T) {
	server := netip.MustParseAddr("203.0.113.10")
	probes := func(domainIPs ...string) advisorProbes {
		return advisorProbes{
			publicIP: func(context.Context) (netip.Addr, error) { return server, nil },
			lookup: func(context.Context, string) ([]netip.Addr, error) {
				var result []netip.Addr
				for _, ip := range domainIPs {
					result = append(result, netip.MustParseAddr(ip))
				}
				return result, nil
			},
			tenants: func(context.Context, *envFile) ([]tenantRow, error) {
				return []tenantRow{{ID: "11111111-aaaa", Subdomain: "spartask", Name: "SparTask"}}, nil
			},
		}
	}
	cfg, err := checkInstalledConfig(installedStack(t, olderEnv))
	if err != nil {
		t.Fatal(err)
	}
	advise(context.Background(), cfg, probes("172.67.200.120", "104.21.74.81"))
	notes := strings.Join(cfg.envResult.Notes, "\n")
	if !strings.Contains(notes, "SPARTASK_EGRESS_DENY_CIDRS=203.0.113.10") || !strings.Contains(notes, "11111111-aaaa   spartask") {
		t.Fatalf("expected Cloudflare and tenant suggestions:\n%s", notes)
	}

	denied := strings.Replace(olderEnv, "SPARTASK_SUBNET=172.30.10.0/24", "SPARTASK_SUBNET=172.30.10.0/24\nSPARTASK_EGRESS_DENY_CIDRS=203.0.113.10\nSPARTASK_PLATFORM_TENANT_IDS=missing-id", 1)
	cfg, err = checkInstalledConfig(installedStack(t, denied))
	if err != nil {
		t.Fatal(err)
	}
	advise(context.Background(), cfg, probes("203.0.113.10"))
	notes = strings.Join(cfg.envResult.Notes, "\n")
	if !strings.Contains(notes, "çıkarın") || !strings.Contains(notes, `"missing-id"`) {
		t.Fatalf("expected warnings for a self-blocking deny list and an unknown company:\n%s", notes)
	}

	cfg, err = checkInstalledConfig(installedStack(t, olderEnv))
	if err != nil {
		t.Fatal(err)
	}
	quiet := probes("203.0.113.10")
	quiet.tenants = func(context.Context, *envFile) ([]tenantRow, error) { return nil, errors.New("stack down") }
	advise(context.Background(), cfg, quiet)
	notes = strings.Join(cfg.envResult.Notes, "\n")
	if strings.Contains(notes, "SPARTASK_EGRESS_DENY_CIDRS") || !strings.Contains(notes, "SELECT id, subdomain") {
		t.Fatalf("direct DNS needs no deny entry; tenants fall back to the manual command:\n%s", notes)
	}
}

func TestInvalidFormatsAreReported(t *testing.T) {
	bad := strings.Replace(olderEnv, "SPARTASK_SUBNET=172.30.10.0/24", "SPARTASK_SUBNET=172.30.10.0/24\nSPARTASK_EGRESS_DENY_CIDRS=1.2.3.4,not-an-ip\nSPARTASK_EGRESS_PRIVATE_GRANTS=[{broken", 1)
	cfg, err := checkInstalledConfig(installedStack(t, bad))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.envResult.Invalid) != 2 {
		t.Fatalf("expected two format errors, got %v", cfg.envResult.Invalid)
	}
}

func TestReportNeverPrintsSecrets(t *testing.T) {
	s := installedStack(t, olderEnv)
	cfg, err := checkInstalledConfig(s)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	printReport(&out, cfg.results(), true, false)
	if strings.Contains(out.String(), cfg.env.Get("NATS_AUTH_TOKEN")) || strings.Contains(out.String(), "keep-this-encryption-key") {
		t.Fatal("report must not contain secret values")
	}
	if !strings.Contains(out.String(), "NATS_AUTH_TOKEN") || !strings.Contains(out.String(), "ÖNERİLEN") {
		t.Fatalf("report misses sections:\n%s", out.String())
	}
}

func TestSummaryKeepsAbbreviations(t *testing.T) {
	rule := envRule{Description: []string{"Gönderen adresi, ör. \"Spartask <no-reply@spartask.ai>\". İkinci cümle."}}
	if got := rule.summary(); got != "Gönderen adresi, ör. \"Spartask <no-reply@spartask.ai>\"." {
		t.Fatalf("summary = %q", got)
	}
}
