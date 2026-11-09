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
	tls               string // tlsFiles, tlsInternal or tlsACME (Let's Encrypt, single address)
	certFile, keyFile string
	cloudflare        bool
	registryUser      string
	marketplace       bool
	marketplaceDomain string
	marketplaceVer    string
	marketplaceURL    string // central marketplace for on-premise installations
	marketplaceKey    string // file holding the marketplace signing key (ED25519_PRIVATE_KEY)
	backupCron        bool
	assumeYes         bool
	// An installation activated with a code from the license service's on-prem page: the code
	// brings its address, license and first company (see activation.go).
	activation    string
	licenseServer string
	companyID     string
	admin         adminAccount
}

func parseInstallFlags(args []string) (installOptions, error) {
	fs := flag.NewFlagSet("install", flag.ContinueOnError)
	var o installOptions
	fs.StringVar(&o.domain, "domain", "", "application domain, e.g. spartask.ai (tenants use *.domain)")
	fs.StringVar(&o.version, "version", defaultVersion, "SparTask release to install, e.g. 1.0.0")
	fs.StringVar(&o.tls, "tls", "", `"files" (certificate files; default) or "internal" (Caddy's own CA); with -activation "acme" (Let's Encrypt; default) or "files"`)
	fs.StringVar(&o.certFile, "cert", "", "certificate file to copy to certs/cert.pem (PEM, full chain)")
	fs.StringVar(&o.keyFile, "key", "", "private key file to copy to certs/key.pem (PEM)")
	fs.BoolVar(&o.cloudflare, "cloudflare", false, "the domain is proxied by Cloudflare (trust Cloudflare's client IP header)")
	fs.StringVar(&o.registryUser, "registry-user", "", "ghcr.io user for private images (token is asked)")
	fs.BoolVar(&o.marketplace, "with-marketplace", false, "also run the marketplace (platform server only)")
	fs.StringVar(&o.marketplaceDomain, "marketplace-domain", "", "marketplace domain (default marketplace.<domain>)")
	fs.StringVar(&o.marketplaceVer, "marketplace-version", "", "marketplace release (default: -version)")
	fs.StringVar(&o.marketplaceKey, "marketplace-signing-key", "", "file with the marketplace signing key (ED25519_PRIVATE_KEY); its public key must be built into SparTask")
	fs.StringVar(&o.marketplaceURL, "marketplace-url", "", "central marketplace for this installation, e.g. https://marketplace.spartask.ai")
	fs.BoolVar(&o.backupCron, "backup-job", true, "offer the daily backup job (with -y: install it unless -backup-job=false)")
	fs.BoolVar(&o.assumeYes, "y", false, "non-interactive: use flags and defaults, never prompt")
	fs.StringVar(&o.activation, "activation", "", "activation code from the license service's on-prem page: the installation takes its address, license and first company from it")
	fs.StringVar(&o.licenseServer, "license-server", "", "license service address shown with the activation code, e.g. https://accounts.spartask.ai")
	fs.StringVar(&o.admin.email, "admin-email", "", "with -activation: the administrator's e-mail address (also Let's Encrypt's contact)")
	fs.StringVar(&o.admin.name, "admin-name", "", "with -activation: the administrator's full name")
	fs.StringVar(&o.companyID, "company-id", "", "with -activation: the company's address id (default: made from its name)")
	err := fs.Parse(args)
	return o, err
}

func runInstall(ctx context.Context, s stack, args []string) error {
	return install(ctx, s, args, defaultPreflightProbes(s))
}

// install checks the server, writes .env and the install state, and completes the installation.
// In a directory with an unfinished installation it continues that one: the same command, run
// again after fixing a failure, takes it to the end.
func install(ctx context.Context, s stack, args []string, probes preflightProbes) error {
	o, err := parseInstallFlags(args)
	if err != nil {
		return err
	}
	if _, err := os.Stat(s.path(".env")); err == nil {
		return continueInstall(ctx, s, o, probes)
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
	if err := preflight(ctx, os.Stdout, s, preflightTargets(o), probes); err != nil {
		return err
	}
	env, mpEnv, err := buildEnv(s, o)
	if err != nil {
		return err
	}
	if err := registryLogin(ctx, s, p, o); err != nil {
		return err
	}
	if o.marketplace {
		_, public, _ := readMarketplaceSigningKey(o.marketplaceKey)
		fmt.Printf("Marketplace public key: %s\nSparTask releases verify licenses only with their built-in keys (spartask_backend/pkg/crypto/license/keys.go); this key must be one of them.\n", public)
	}
	if err := prepareDirectories(s, o); err != nil {
		return err
	}
	if err := installCertificates(s, o); err != nil {
		return err
	}
	// The install state first: from the moment .env exists, install continues instead of refusing.
	state := newInstallState(o)
	if err := state.save(s); err != nil {
		return err
	}
	if err := env.Save(s.path(".env")); err != nil {
		return err
	}
	if mpEnv != nil {
		if err := mpEnv.Save(s.path("marketplace.env")); err != nil {
			return err
		}
	}
	fmt.Println("\n.env written (mode 600).")
	return completeInstall(ctx, s, p, env, state, o.admin.password, probes)
}

// continueInstall takes an unfinished installation to the end. A new -activation code replaces
// one that expired before it was exchanged, and -cert/-key replace the certificate files.
func continueInstall(ctx context.Context, s stack, o installOptions, probes preflightProbes) error {
	state, err := readInstallState(s)
	if errors.Is(err, os.ErrNotExist) {
		return errors.New(".env already exists: this directory is installed (use update, restart or status)")
	}
	if err != nil {
		return err
	}
	fmt.Println("This directory has an unfinished installation; continuing it with its saved configuration.")
	if err := checkDocker(ctx); err != nil {
		return err
	}
	env, err := loadInstalledEnv(s)
	if err != nil {
		return err
	}
	if state.Activation != nil && o.activation != "" {
		state.Activation.Code = o.activation
	}
	if err := copyCertificates(s, o); err != nil {
		return err
	}
	p := newPrompter(o.assumeYes)
	if err := registryLogin(ctx, s, p, o); err != nil {
		return err
	}
	return completeInstall(ctx, s, p, env, state, "", probes)
}

// completeInstall runs the steps after .env exists. Every step that changes something outside the
// stack is recorded in the install state, so a failure is fixed and the same command run again.
func completeInstall(ctx context.Context, s stack, p *prompter, env *envFile, state *installState, password string, probes preflightProbes) error {
	again := func(err error, hint string) error {
		if hint != "" {
			err = fmt.Errorf("%w\n%s", err, hint)
		}
		return fmt.Errorf("%w\nThe configuration is saved. After fixing the cause run the same install command again: it continues where it stopped", err)
	}
	if state.Activation != nil {
		company, err := activate(ctx, s, env, *state.Activation)
		if err != nil {
			return again(err, "")
		}
		if err := env.Save(s.path(".env")); err != nil {
			return err
		}
		state.Activation, state.Company = nil, company
		if err := state.save(s); err != nil {
			return err
		}
	}
	if err := checkServedAddress(ctx, s, env, probes.lookup, probes.now()); err != nil {
		return again(err, "")
	}
	fmt.Println("Pulling images and starting the stack...")
	if err := s.compose(ctx, "pull"); err != nil {
		return again(err, "Images come from ghcr.io and Docker Hub: allow outbound HTTPS to them (behind a proxy also in Docker's proxy settings).")
	}
	if err := s.compose(ctx, "up", "-d", "--remove-orphans"); err != nil {
		return again(err, "Inspect with: docker compose logs migrate api")
	}
	if err := s.waitHealthy(ctx, "api", 3*time.Minute); err != nil {
		return again(err, "Inspect with: docker compose logs api")
	}
	if state.Company != nil {
		if err := completeCompany(ctx, s, p, state, password); err != nil {
			return again(err, "")
		}
	}
	if state.BackupJob {
		if err := installBackupCron(s); err != nil {
			return again(err, "")
		}
	}
	if err := removeInstallState(s); err != nil {
		return err
	}
	printInstallSummary(env, state.Company)
	// Settings the operator still has to fill in, with their explanations.
	cfg, err := checkInstalledConfig(s)
	if err != nil {
		return err
	}
	advise(ctx, cfg, defaultAdvisorProbes(s))
	printReport(os.Stdout, cfg.results(), true, true)
	return nil
}

// completeCompany installs the activated installation's license and creates its company with the
// administrator, recording each step. A continued installation asks for the password again.
func completeCompany(ctx context.Context, s stack, p *prompter, state *installState, password string) error {
	c := state.Company
	if !c.LicenseInstalled {
		if err := c.installLicense(ctx, s); err != nil {
			return err
		}
		c.LicenseInstalled = true
		if err := state.save(s); err != nil {
			return err
		}
	}
	if c.Created {
		return nil
	}
	if password == "" {
		fmt.Printf("The administrator %s of %s is created now.\n", c.AdminEmail, c.Name)
		var err error
		if password, err = askAdminPassword(p); err != nil {
			return err
		}
	}
	if err := c.create(ctx, s, password); err != nil {
		return err
	}
	c.Created = true
	return state.save(s)
}

func askInstallOptions(p *prompter, o *installOptions) error {
	var err error
	if o.version, err = p.ask("SparTask version", o.version, true); err != nil {
		return err
	}
	o.version = strings.TrimPrefix(o.version, "v")
	if !versionPattern.MatchString(o.version) {
		return fmt.Errorf("invalid version %q, e.g. 1.0.0", o.version)
	}
	if o.activation != "" {
		if err := askActivation(p, o); err != nil {
			return err
		}
	} else if err := askAddressAndTLS(p, o); err != nil {
		return err
	}
	if runtime.GOOS == "linux" && os.Geteuid() == 0 && o.backupCron {
		if o.backupCron, err = p.confirm("Install a daily backup job (03:30, /etc/cron.d/spartask-backup)?", true); err != nil {
			return err
		}
	} else {
		o.backupCron = false
	}
	return nil
}

// askAddressAndTLS asks for the domain, the certificate and the marketplace of an installation
// whose companies live on subdomains of the domain (spartask.ai).
func askAddressAndTLS(p *prompter, o *installOptions) error {
	var err error
	if o.domain, err = p.ask("Domain (company addresses become <company>.<domain>)", o.domain, true); err != nil {
		return err
	}
	o.domain = strings.ToLower(strings.TrimSpace(o.domain))
	if !domainPattern.MatchString(o.domain) {
		return fmt.Errorf("invalid domain %q (no scheme, no port), e.g. spartask.ai", o.domain)
	}
	if o.tls == "" {
		o.tls = tlsFiles
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
		// The key is never generated: engines trust only the marketplace keys built into the
		// release, so a new key would make every license and module grant unverifiable.
		if o.marketplaceKey, err = p.ask("Marketplace signing key file (ED25519_PRIVATE_KEY, from the password manager)", o.marketplaceKey, true); err != nil {
			return err
		}
	}
	return nil
}

// setMarketplaceURL connects the installation to a central marketplace: the studio link, package
// downloads and receipts, and the origin allowed to deliver install requests from the browser.
func setMarketplaceURL(env *envFile, raw string) {
	url := strings.TrimRight(raw, "/")
	env.Set("SPARTASK_MARKETPLACE_URL", url)
	env.Set("MARKETPLACE_API_URL", url)
	env.Set("MARKETPLACE_ALLOWED_ORIGINS", url)
}

// registryLogin logs in to ghcr.io for private images when -registry-user is given.
func registryLogin(ctx context.Context, s stack, p *prompter, o installOptions) error {
	if o.registryUser == "" {
		return nil
	}
	token, err := p.askSecret("ghcr.io token (read:packages)")
	if err != nil || token == "" {
		return err
	}
	login := s.command(ctx, "login", "ghcr.io", "-u", o.registryUser, "--password-stdin")
	login.Stdin = strings.NewReader(token)
	login.Stdout, login.Stderr = os.Stdout, os.Stderr
	if err := login.Run(); err != nil {
		return fmt.Errorf("docker login ghcr.io: %w", err)
	}
	return nil
}

// buildEnv fills .env (and marketplace.env) from the templates with the answers and new secrets.
func buildEnv(s stack, o installOptions) (*envFile, *envFile, error) {
	env, err := readEnvFile(s.path(".env.example"))
	if err != nil {
		return nil, nil, err
	}
	rules, err := readRules(s.path(".env.example"))
	if err != nil {
		return nil, nil, err
	}
	env.Set("SPARTASK_VERSION", o.version)
	switch o.tls {
	case tlsInternal:
		env.Set("SPARTASK_TLS", tlsInternal)
	case tlsACME:
		env.Set("SPARTASK_TLS", o.admin.email)
	default:
		env.Set("SPARTASK_TLS", certificateFilesValue)
	}
	// An activated installation learns its address from the activation (activate).
	env.Set("SPARTASK_DOMAIN", o.domain)
	if o.cloudflare {
		env.Set("CADDY_TRUSTED_PROXIES", cloudflareTrustedProxies())
	}
	if o.marketplaceURL != "" {
		setMarketplaceURL(env, o.marketplaceURL)
	}
	if !o.marketplace {
		// Secrets marked "generate" in the template (database password, SECRET_KEY, tokens ...).
		if _, err := syncEnv(".env", env, rules, nil, true); err != nil {
			return nil, nil, err
		}
		return env, nil, nil
	}

	mp, err := readEnvFile(s.path("marketplace.env.example"))
	if err != nil {
		return nil, nil, err
	}
	mpRules, err := readRules(s.path("marketplace.env.example"))
	if err != nil {
		return nil, nil, err
	}
	private, _, err := readMarketplaceSigningKey(o.marketplaceKey)
	if err != nil {
		return nil, nil, err
	}
	mp.Set("ED25519_PRIVATE_KEY", private)
	url := "https://" + o.marketplaceDomain
	env.Set("COMPOSE_FILE", "compose.yml:compose.marketplace.yml")
	env.Set("MARKETPLACE_DOMAIN", o.marketplaceDomain)
	env.Set("MARKETPLACE_VERSION", o.marketplaceVer)
	env.Set("SPARTASK_MARKETPLACE_URL", url)
	env.Set("MARKETPLACE_API_URL", url)
	env.Set("MARKETPLACE_ALLOWED_ORIGINS", url)
	if _, err := syncEnv(".env", env, rules, nil, true); err != nil {
		return nil, nil, err
	}
	if _, err := syncEnv("marketplace.env", mp, mpRules, env, true); err != nil {
		return nil, nil, err
	}
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

// installCertificates puts the certificate files given with -cert and -key into certs/; a
// certificate-files installation needs both there.
func installCertificates(s stack, o installOptions) error {
	if err := copyCertificates(s, o); err != nil {
		return err
	}
	if o.tls != tlsFiles {
		return nil
	}
	for _, name := range []string{"cert.pem", "key.pem"} {
		if _, err := os.Stat(s.path("certs", name)); err != nil {
			return fmt.Errorf("certs/%s is missing: pass -cert and -key, or copy the certificate (cert.pem, full chain) and its key (key.pem) into %s",
				name, s.path("certs"))
		}
	}
	return nil
}

// copyCertificates copies the files given with -cert and -key into certs/ (also when an
// unfinished installation continues with a corrected certificate).
func copyCertificates(s stack, o installOptions) error {
	for _, c := range []struct{ src, dst string }{{o.certFile, "cert.pem"}, {o.keyFile, "key.pem"}} {
		if c.src == "" {
			continue
		}
		if err := copyFile(c.src, s.path("certs", c.dst), 0o600); err != nil {
			return err
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
	content := "# SparTask daily backup (spartaskctl install)\nSHELL=/bin/sh\nPATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin\n" + line
	if err := os.WriteFile("/etc/cron.d/spartask-backup", []byte(content), 0o644); err != nil {
		return fmt.Errorf("install backup job: %w", err)
	}
	return nil
}

func printInstallSummary(env *envFile, company *companyState) {
	if company != nil {
		company.printSummary(env)
		return
	}
	domain := env.Get("SPARTASK_DOMAIN")
	marketplace := scopeOf(env).marketplace
	fmt.Printf("\nSparTask %s is running: https://%s (companies: https://<company>.%s)\n", env.Get("SPARTASK_VERSION"), domain, domain)
	if marketplace {
		fmt.Printf("Marketplace %s: https://%s\n", env.Get("MARKETPLACE_VERSION"), env.Get("MARKETPLACE_DOMAIN"))
	}
	fmt.Println("\nNext steps:")
	fmt.Printf("  - DNS: A records for %s and *.%s point to this server", domain, domain)
	if marketplace {
		fmt.Printf(" (and %s)", env.Get("MARKETPLACE_DOMAIN"))
	}
	fmt.Println()
	printHandover()
	if marketplace {
		fmt.Println("  - CI publishing key (GitHub org secret MARKETPLACE_PUBLISH_KEY): PUBLISH_API_KEY in marketplace.env")
	}
}
