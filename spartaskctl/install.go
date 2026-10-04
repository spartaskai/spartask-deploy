package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

var (
	domainPattern  = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z][a-z0-9-]{0,61}[a-z0-9]$`)
	versionPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$`)
)

type installOptions struct {
	domain            string
	version           string
	tls               string // "files" or "internal"
	certFile, keyFile string
	cloudflare        bool
	geminiKey         string
	registryUser      string
	marketplace       bool
	marketplaceDomain string
	marketplaceVer    string
	marketplaceURL    string // central marketplace for on-premise installations
	backupCron        bool
	assumeYes         bool
}

func runInstall(ctx context.Context, s stack, args []string) error {
	fs := flag.NewFlagSet("install", flag.ContinueOnError)
	var o installOptions
	fs.StringVar(&o.domain, "domain", "", "application domain, e.g. spartask.ai (tenants use *.domain)")
	fs.StringVar(&o.version, "version", defaultVersion, "Spartask release to install, e.g. 1.0.0")
	fs.StringVar(&o.tls, "tls", "files", `"files" (certificate files covering domain and *.domain) or "internal" (Caddy's own CA)`)
	fs.StringVar(&o.certFile, "cert", "", "certificate file to copy to certs/cert.pem (PEM, full chain)")
	fs.StringVar(&o.keyFile, "key", "", "private key file to copy to certs/key.pem (PEM)")
	fs.BoolVar(&o.cloudflare, "cloudflare", false, "the domain is proxied by Cloudflare (trust Cloudflare's client IP header)")
	fs.StringVar(&o.registryUser, "registry-user", "", "ghcr.io user for private images (token is asked)")
	fs.BoolVar(&o.marketplace, "with-marketplace", false, "also run the marketplace (platform server only)")
	fs.StringVar(&o.marketplaceDomain, "marketplace-domain", "", "marketplace domain (default marketplace.<domain>)")
	fs.StringVar(&o.marketplaceVer, "marketplace-version", "", "marketplace release (default: -version)")
	fs.StringVar(&o.marketplaceURL, "marketplace-url", "", "central marketplace for this installation, e.g. https://marketplace.spartask.ai")
	fs.BoolVar(&o.assumeYes, "y", false, "non-interactive: use flags and defaults, never prompt")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if _, err := os.Stat(s.path(".env")); err == nil {
		return errors.New(".env already exists: this directory is installed (use update, restart or status)")
	}
	for _, required := range []string{"compose.yml", ".env.example", filepath.Join("caddy", "Caddyfile")} {
		if _, err := os.Stat(s.path(required)); err != nil {
			return fmt.Errorf("%s not found: run spartaskctl inside the extracted spartask-deploy directory (or pass -dir)", required)
		}
	}
	if err := checkDocker(ctx); err != nil {
		return err
	}
	p := newPrompter(o.assumeYes)
	if err := askInstallOptions(p, &o); err != nil {
		return err
	}
	env, mpEnv, err := buildEnv(s, o)
	if err != nil {
		return err
	}
	if err := prepareDirectories(s, o); err != nil {
		return err
	}
	if err := installCertificates(s, o); err != nil {
		return err
	}
	if o.registryUser != "" {
		token, err := p.askSecret("ghcr.io token (read:packages)")
		if err != nil {
			return err
		}
		if token != "" {
			login := s.command(ctx, "login", "ghcr.io", "-u", o.registryUser, "--password-stdin")
			login.Stdin = strings.NewReader(token)
			login.Stdout, login.Stderr = os.Stdout, os.Stderr
			if err := login.Run(); err != nil {
				return fmt.Errorf("docker login ghcr.io: %w", err)
			}
		}
	}
	if err := env.Save(s.path(".env")); err != nil {
		return err
	}
	if mpEnv != nil {
		if err := mpEnv.Save(s.path("marketplace.env")); err != nil {
			return err
		}
	}
	fmt.Println("\n.env written (mode 600). Pulling images and starting the stack...")
	// From here on the installation exists; after fixing a failure, `spartaskctl restart` continues.
	resume := func(err error, hint string) error {
		return fmt.Errorf("%w\n%s\nThe configuration is saved; after fixing the cause run: spartaskctl restart", err, hint)
	}
	if err := s.compose(ctx, "pull"); err != nil {
		return resume(err, "Private images need a registry login: docker login ghcr.io (or install -registry-user).")
	}
	if err := s.compose(ctx, "up", "-d", "--remove-orphans"); err != nil {
		return resume(err, "Inspect with: docker compose logs migrate api")
	}
	if err := s.waitHealthy(ctx, "api", 3*time.Minute); err != nil {
		return resume(err, "Inspect with: docker compose logs api")
	}
	if o.backupCron {
		if err := installBackupCron(s); err != nil {
			return err
		}
	}
	printInstallSummary(o, env)
	return nil
}

func askInstallOptions(p *prompter, o *installOptions) error {
	var err error
	if o.domain, err = p.ask("Domain (company addresses become <company>.<domain>)", o.domain, true); err != nil {
		return err
	}
	o.domain = strings.ToLower(strings.TrimSpace(o.domain))
	if !domainPattern.MatchString(o.domain) {
		return fmt.Errorf("invalid domain %q (no scheme, no port), e.g. spartask.ai", o.domain)
	}
	if o.version, err = p.ask("Spartask version", o.version, true); err != nil {
		return err
	}
	o.version = strings.TrimPrefix(o.version, "v")
	if !versionPattern.MatchString(o.version) {
		return fmt.Errorf("invalid version %q, e.g. 1.0.0", o.version)
	}
	if !p.assumeYes {
		fmt.Println("TLS: 1) certificate files for the domain and *.domain (Cloudflare Origin Certificate, company certificate)")
		fmt.Println("     2) internal (Caddy's own CA; closed networks only)")
		def := "1"
		if o.tls == "internal" {
			def = "2"
		}
		choice, err := p.ask("TLS mode", def, true)
		if err != nil {
			return err
		}
		o.tls = map[string]string{"1": "files", "2": "internal", "files": "files", "internal": "internal"}[choice]
	}
	switch o.tls {
	case "files":
		if o.cloudflare, err = p.confirm("Is the domain proxied by Cloudflare (orange cloud)?", o.cloudflare); err != nil {
			return err
		}
	case "internal":
	default:
		return fmt.Errorf(`-tls must be "files" or "internal"`)
	}
	// Optional; with -y it stays empty and can be set in .env later.
	if o.geminiKey, err = p.askSecret("Gemini API key"); err != nil {
		return err
	}
	if o.marketplace {
		def := o.marketplaceDomain
		if def == "" {
			def = "marketplace." + o.domain
		}
		if o.marketplaceDomain, err = p.ask("Marketplace domain", def, true); err != nil {
			return err
		}
		if o.marketplaceVer == "" {
			o.marketplaceVer = o.version
		}
		if o.marketplaceVer, err = p.ask("Marketplace version", o.marketplaceVer, true); err != nil {
			return err
		}
		o.marketplaceVer = strings.TrimPrefix(o.marketplaceVer, "v")
		if !versionPattern.MatchString(o.marketplaceVer) {
			return fmt.Errorf("invalid marketplace version %q", o.marketplaceVer)
		}
	}
	if runtime.GOOS == "linux" && os.Geteuid() == 0 {
		if o.backupCron, err = p.confirm("Install a daily backup job (03:30, /etc/cron.d/spartask-backup)?", true); err != nil {
			return err
		}
	}
	return nil
}

// buildEnv fills .env (and marketplace.env) from the templates with the answers and new secrets.
func buildEnv(s stack, o installOptions) (*envFile, *envFile, error) {
	env, err := readEnvFile(s.path(".env.example"))
	if err != nil {
		return nil, nil, err
	}
	secrets := map[string]int{"DB_PASSWORD": 24, "SECRET_KEY": 32, "WEB_AUTH_JWT_SECRET": 32}
	for key, size := range secrets {
		value, err := randomHex(size)
		if err != nil {
			return nil, nil, err
		}
		env.Set(key, value)
	}
	env.Set("SPARTASK_DOMAIN", o.domain)
	env.Set("SPARTASK_VERSION", o.version)
	env.Set("GEMINI_API_KEY", o.geminiKey)
	if o.tls == "internal" {
		env.Set("SPARTASK_TLS", "internal")
	} else {
		env.Set("SPARTASK_TLS", "/certs/cert.pem /certs/key.pem")
	}
	if o.cloudflare {
		env.Set("CADDY_TRUSTED_PROXIES", cloudflareTrustedProxies())
	}
	if o.marketplaceURL != "" {
		url := strings.TrimRight(o.marketplaceURL, "/")
		env.Set("SPARTASK_MARKETPLACE_URL", url)
		env.Set("MARKETPLACE_API_URL", url)
		env.Set("MARKETPLACE_ALLOWED_ORIGINS", url)
	}
	if !o.marketplace {
		return env, nil, nil
	}

	mp, err := readEnvFile(s.path("marketplace.env.example"))
	if err != nil {
		return nil, nil, err
	}
	private, public, err := marketplaceKeys()
	if err != nil {
		return nil, nil, err
	}
	mp.Set("ED25519_PRIVATE_KEY", private)
	values := map[string]int{"JWT_SECRET": 32, "ENGINE_API_KEY": 24, "PUBLISH_API_KEY": 24, "SSO_SHARED_SECRET": 32}
	for key, size := range values {
		value, err := randomHex(size)
		if err != nil {
			return nil, nil, err
		}
		mp.Set(key, value)
	}
	dbPassword, err := randomHex(24)
	if err != nil {
		return nil, nil, err
	}
	url := "https://" + o.marketplaceDomain
	env.Set("COMPOSE_FILE", "compose.yml:compose.marketplace.yml")
	env.Set("MARKETPLACE_DOMAIN", o.marketplaceDomain)
	env.Set("MARKETPLACE_VERSION", o.marketplaceVer)
	env.Set("MARKETPLACE_DB_PASSWORD", dbPassword)
	env.Set("MARKETPLACE_PUBLIC_KEY", public)
	env.Set("SPARTASK_MARKETPLACE_URL", url)
	env.Set("MARKETPLACE_API_URL", url)
	env.Set("MARKETPLACE_ALLOWED_ORIGINS", url)
	env.Set("MARKETPLACE_SSO_ENABLED", "true")
	env.Set("MARKETPLACE_SSO_SHARED_SECRET", mp.Get("SSO_SHARED_SECRET"))
	env.Set("MARKETPLACE_SSO_CALLBACK_URL", url+"/auth/spartask/callback")
	return env, mp, nil
}

func prepareDirectories(s stack, o installOptions) error {
	dirs := map[string]os.FileMode{
		"data": 0o700, filepath.Join("data", "postgres"): 0o700, filepath.Join("data", "storage"): 0o755,
		filepath.Join("data", "downloads"): 0o755, filepath.Join("data", "nats"): 0o700,
		"mobile": 0o755, "certs": 0o700, "backups": 0o700, ".secrets": 0o700, filepath.Join(".secrets", "mcp"): 0o700,
	}
	if o.marketplace {
		dirs[filepath.Join("data", "marketplace-postgres")] = 0o700
	}
	for dir, mode := range dirs {
		if err := os.MkdirAll(s.path(dir), mode); err != nil {
			return fmt.Errorf("create %s: %w", dir, err)
		}
	}
	return nil
}

func installCertificates(s stack, o installOptions) error {
	if o.tls != "files" {
		return nil
	}
	for _, c := range []struct{ src, dst string }{{o.certFile, "cert.pem"}, {o.keyFile, "key.pem"}} {
		dst := s.path("certs", c.dst)
		if c.src != "" {
			if err := copyFile(c.src, dst, 0o600); err != nil {
				return err
			}
		}
		if _, err := os.Stat(dst); err != nil {
			return fmt.Errorf("certs/%s is missing: copy the certificate (cert.pem) and key (key.pem) covering %s and *.%s into %s, or pass -cert and -key",
				c.dst, o.domain, o.domain, s.path("certs"))
		}
	}
	return nil
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open %s: %w", src, err)
	}
	defer in.Close()
	data, err := io.ReadAll(in)
	if err != nil {
		return fmt.Errorf("read %s: %w", src, err)
	}
	if err := os.WriteFile(dst, data, mode); err != nil {
		return fmt.Errorf("write %s: %w", dst, err)
	}
	return os.Chmod(dst, mode)
}

func installBackupCron(s stack) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate spartaskctl: %w", err)
	}
	line := fmt.Sprintf("30 3 * * * root %s -dir %s backup >> %s 2>&1\n", exe, s.dir, s.path("backups", "backup.log"))
	content := "# Spartask daily backup (spartaskctl install)\nSHELL=/bin/sh\nPATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin\n" + line
	if err := os.WriteFile("/etc/cron.d/spartask-backup", []byte(content), 0o644); err != nil {
		return fmt.Errorf("install backup job: %w", err)
	}
	return nil
}

func printInstallSummary(o installOptions, env *envFile) {
	fmt.Printf("\nSpartask %s is running: https://%s (companies: https://<company>.%s)\n", o.version, o.domain, o.domain)
	if o.marketplace {
		fmt.Printf("Marketplace %s: https://%s\n", o.marketplaceVer, o.marketplaceDomain)
	}
	fmt.Println("\nNext steps:")
	fmt.Printf("  - DNS: A records for %s and *.%s point to this server", o.domain, o.domain)
	if o.marketplace {
		fmt.Printf(" (and %s)", o.marketplaceDomain)
	}
	fmt.Println()
	fmt.Println("  - Store a copy of .env (SECRET_KEY) in a password manager; backups cannot be decrypted without it.")
	fmt.Println("  - Mail, Google sign-in and other settings: edit .env, then run: spartaskctl restart")
	if env.Get("GEMINI_API_KEY") == "" {
		fmt.Println("  - GEMINI_API_KEY is empty: AI features stay off until it is set in .env.")
	}
	if o.marketplace {
		fmt.Println("  - CI publishing key (GitHub org secret MARKETPLACE_PUBLISH_KEY): PUBLISH_API_KEY in marketplace.env")
	}
}
