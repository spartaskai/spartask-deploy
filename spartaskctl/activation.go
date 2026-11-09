package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/mail"
	"net/url"
	"os"
	"regexp"
	"strings"
)

// An activated installation (install -activation) is a customer's own server with one company:
// the code from the license service's on-prem page brings the installation's address (the one
// the customer registered there), its license and the company's name. The company is served at
// that address itself (SPARTASK_ROOT_TENANT), company sign-up is closed (the administrator is
// created here) and the license renews itself (SPARTASK_LICENSE_SERVER). The certificate comes
// from Let's Encrypt when the address is reachable from the internet, or from the company's
// certificate files when it is reachable only in the company network or over VPN.

// installationKeyVariable is the installation key the license is bound to (spartask-admin reads it).
const installationKeyVariable = "SPARTASK_INSTALLATION_KEY"

// adminPasswordVariable gives the administrator's password to a non-interactive install (-y).
const adminPasswordVariable = "SPARTASK_ADMIN_PASSWORD"

// Password length rules of SparTask's company administrators (bcrypt reads at most 72 bytes).
const (
	minAdminPassword = 12
	maxAdminPassword = 72
)

// adminAccount is the first company's administrator.
type adminAccount struct {
	email, name, password string
}

// activationAnswer is spartask-admin license activate's output (licensedoc.ActivationResponse).
type activationAnswer struct {
	License          string `json:"license"`
	InstallationID   string `json:"installation_id"`
	OrganizationID   string `json:"organization_id"`
	OrganizationName string `json:"organization_name"`
	BaseURL          string `json:"base_url"`
	MarketplaceURL   string `json:"marketplace_url"`
	MarketplaceID    string `json:"marketplace_id"`
}

// companyIDPattern is a company address id SparTask accepts (its schema name is tenant_<id>).
var companyIDPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,41}[a-z0-9])?$`)

// askActivation asks what an activated installation needs besides the code: the license
// service, the administrator (also the certificate's contact) and the administrator's password.
func askActivation(p *prompter, o *installOptions) error {
	if o.marketplace {
		return errors.New("-activation installs a customer's SparTask; the marketplace runs only on the platform server")
	}
	var err error
	if o.licenseServer, err = p.ask("License service address (shown with the activation code)", o.licenseServer, true); err != nil {
		return err
	}
	if o.licenseServer, err = licenseServerURL(o.licenseServer); err != nil {
		return err
	}
	if o.admin.email, err = p.ask("Administrator e-mail (sign-in; also the certificate contact)", o.admin.email, true); err != nil {
		return err
	}
	if addr, err := mail.ParseAddress(o.admin.email); err != nil || addr.Address != o.admin.email {
		return fmt.Errorf("invalid administrator e-mail %q", o.admin.email)
	}
	if o.admin.name, err = p.ask("Administrator full name", o.admin.name, true); err != nil {
		return err
	}
	if o.admin.password, err = askAdminPassword(p); err != nil {
		return err
	}
	if o.tls, err = askActivationTLS(p, o.tls); err != nil {
		return err
	}
	if o.tls == tlsFiles {
		if o.certFile, err = p.ask("Certificate file of the address (PEM, full chain)", o.certFile, true); err != nil {
			return err
		}
		if o.keyFile, err = p.ask("Private key file of the certificate (PEM)", o.keyFile, true); err != nil {
			return err
		}
	}
	return nil
}

// askActivationTLS asks where the certificate of an activated installation comes from: Let's
// Encrypt (the default) when its address is reachable from the internet, or the company's
// certificate files when it is reachable only in the company network or over VPN.
func askActivationTLS(p *prompter, mode string) (string, error) {
	if mode == "" {
		mode = tlsACME
	}
	if !p.assumeYes {
		fmt.Fprintln(p.out, "Certificate: 1) Let's Encrypt: users reach the address from the internet (ports 80 and 443 open to the internet)")
		fmt.Fprintln(p.out, "             2) certificate files: users reach the address only in the company network or over VPN")
		def := map[string]string{tlsACME: "1", tlsFiles: "2"}[mode]
		choice, err := p.ask("Certificate", def, true)
		if err != nil {
			return "", err
		}
		mode = map[string]string{"1": tlsACME, "2": tlsFiles, tlsACME: tlsACME, tlsFiles: tlsFiles}[choice]
	}
	if mode != tlsACME && mode != tlsFiles {
		return "", fmt.Errorf(`with -activation, -tls must be "acme" (Let's Encrypt) or "files" (-cert and -key)`)
	}
	return mode, nil
}

// askAdminPassword asks for the administrator's password twice, or with -y reads it from
// SPARTASK_ADMIN_PASSWORD.
func askAdminPassword(p *prompter) (string, error) {
	if p.assumeYes {
		password := os.Getenv(adminPasswordVariable)
		return password, checkAdminPassword(password)
	}
	for {
		password, err := p.readHidden(fmt.Sprintf("Administrator password (%d-%d characters, input hidden)", minAdminPassword, maxAdminPassword))
		if err != nil {
			return "", err
		}
		if err := checkAdminPassword(password); err != nil {
			fmt.Fprintln(p.out, " ", err)
			continue
		}
		again, err := p.readHidden("Repeat the password")
		if err != nil {
			return "", err
		}
		if again == password {
			return password, nil
		}
		fmt.Fprintln(p.out, "  The passwords differ.")
	}
}

func checkAdminPassword(password string) error {
	if len(password) < minAdminPassword || len(password) > maxAdminPassword {
		return fmt.Errorf("the administrator password must be %d-%d characters (non-interactive: set %s)",
			minAdminPassword, maxAdminPassword, adminPasswordVariable)
	}
	return nil
}

// licenseServerURL checks the license service's address: https (http only for localhost).
func licenseServerURL(raw string) (string, error) {
	raw = strings.TrimRight(strings.TrimSpace(raw), "/")
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.RawQuery != "" || u.Fragment != "" ||
		(u.Scheme != "https" && !(u.Scheme == "http" && u.Hostname() == "localhost")) {
		return "", fmt.Errorf("invalid license service address %q, e.g. https://accounts.spartask.ai", raw)
	}
	return raw, nil
}

// activate exchanges the activation code for the installation's license (spartask-admin in the
// release image, with the installation key saved in .env) and points env at the registered
// address. The license service accepts the code again with the same key, so an interrupted
// installation exchanges it again.
func activate(ctx context.Context, s stack, env *envFile, a pendingActivation) (*companyState, error) {
	image := env.Get("SPARTASK_IMAGE") + ":" + env.Get("SPARTASK_VERSION")
	fmt.Printf("Activating with %s ...\n", a.LicenseServer)
	pull := s.command(ctx, "pull", image)
	pull.Stdout, pull.Stderr = os.Stdout, os.Stderr
	if err := pull.Run(); err != nil {
		return nil, fmt.Errorf("docker pull %s: %w (private images need -registry-user)", image, err)
	}
	// The key reaches the container through the environment, never the command line.
	run := s.command(ctx, "run", "--rm", "-e", installationKeyVariable, "--entrypoint", "/app/bin/spartask-admin", image,
		"license", "activate", "-code", a.Code, "-server", a.LicenseServer)
	run.Env = append(os.Environ(), installationKeyVariable+"="+env.Get(installationKeyVariable))
	var out bytes.Buffer
	run.Stdout, run.Stderr = &out, os.Stderr
	if err := run.Run(); err != nil {
		return nil, licenseAdmin(fmt.Errorf("activation failed (an expired code: get a new one on the on-prem page and pass it with -activation): %w", err))
	}
	var answer activationAnswer
	if err := json.Unmarshal(out.Bytes(), &answer); err != nil {
		return nil, fmt.Errorf("read the activation answer: %w", err)
	}
	return applyActivation(env, a, answer)
}

// applyActivation sets the installation's single address, its company and closed sign-up.
func applyActivation(env *envFile, a pendingActivation, answer activationAnswer) (*companyState, error) {
	domain, err := addressDomain(answer.BaseURL)
	if err != nil {
		return nil, err
	}
	id := a.CompanyID
	if id == "" {
		id = companyIDFromName(answer.OrganizationName)
	}
	if !companyIDPattern.MatchString(id) {
		return nil, fmt.Errorf("cannot make a company address id from %q: pass -company-id (lower-case letters, digits and '-', at most 43)", answer.OrganizationName)
	}
	if answer.License == "" || answer.OrganizationName == "" {
		return nil, errors.New("the activation answer is incomplete")
	}
	env.Set("SPARTASK_DOMAIN", domain)
	env.Set("SPARTASK_SITES", domain)
	env.Set("SPARTASK_ROOT_TENANT", id)
	env.Set("SPARTASK_SIGNUP_MODE", "closed")
	// The company runs the installation: its administrators see the license and accept a changed
	// agreement, and the scheduler renews the license with the license service.
	env.Set("SPARTASK_PLATFORM_TENANT_IDS", id)
	env.Set("SPARTASK_LICENSE_SERVER", a.LicenseServer)
	if err := applyMarketplace(env, answer); err != nil {
		return nil, err
	}
	return &companyState{ID: id, Name: answer.OrganizationName, Address: "https://" + domain,
		AdminEmail: a.AdminEmail, AdminName: a.AdminName, License: answer.License}, nil
}

// applyMarketplace lets the company use the marketplace as the installation: the marketplace knows
// it by the installation's marketplace id, and its executor licenses are the installation's. The
// marketplace is the one the license service names unless -marketplace-url chose another (it is
// in .env already).
func applyMarketplace(env *envFile, answer activationAnswer) error {
	if answer.MarketplaceID == "" {
		return nil
	}
	env.Set("SPARTASK_MARKETPLACE_COMPANY", answer.MarketplaceID)
	if env.Get("SPARTASK_MARKETPLACE_URL") != "" || answer.MarketplaceURL == "" {
		return nil
	}
	u, err := url.Parse(answer.MarketplaceURL)
	if err != nil || u.Scheme != "https" || u.Host == "" || (u.Path != "" && u.Path != "/") {
		return fmt.Errorf("the license service named an invalid marketplace address %q", answer.MarketplaceURL)
	}
	setMarketplaceURL(env, answer.MarketplaceURL)
	return nil
}

// addressDomain returns the host of the address registered on the on-prem page: https, no port,
// no path.
func addressDomain(baseURL string) (string, error) {
	u, err := url.Parse(baseURL)
	if err != nil || u.Scheme != "https" || u.Port() != "" || (u.Path != "" && u.Path != "/") || !domainPattern.MatchString(strings.ToLower(u.Hostname())) {
		return "", fmt.Errorf("the registered address %q is not an https domain without port or path; register the installation again on the on-prem page", baseURL)
	}
	return strings.ToLower(u.Hostname()), nil
}

// turkishLetters are spelled in ASCII in company address ids.
var turkishLetters = strings.NewReplacer("ç", "c", "ğ", "g", "ı", "i", "İ", "i", "ö", "o", "ş", "s", "ü", "u",
	"Ç", "c", "Ğ", "g", "Ö", "o", "Ş", "s", "Ü", "u")

// companyIDFromName makes a company address id from its name: "Acme Yazılım A.Ş." becomes
// "acme-yazilim-a-s". An empty result means the name has no usable letters.
func companyIDFromName(name string) string {
	lower := strings.ToLower(turkishLetters.Replace(name))
	var b strings.Builder
	dash := false
	for _, r := range lower {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			dash = false
		} else if !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
		}
	}
	id := strings.TrimRight(b.String(), "-")
	if len(id) > 43 {
		id = strings.TrimRight(id[:43], "-")
	}
	return id
}

// installLicense installs the company's license in the running stack.
func (c *companyState) installLicense(ctx context.Context, s stack) error {
	if err := licenseAdmin(s.composeStream(ctx, strings.NewReader(c.License), os.Stdout,
		"exec", "-T", "api", "/app/bin/spartask-admin", "license", "install", "-f", "-")); err != nil {
		return fmt.Errorf("install the license: %w", err)
	}
	return nil
}

// create creates the company with its administrator in the running stack. The password reaches
// spartask-admin on standard input.
func (c *companyState) create(ctx context.Context, s stack, password string) error {
	if err := s.composeStream(ctx, strings.NewReader(password+"\n"), os.Stdout, "exec", "-T", "api", "/app/bin/spartask-admin", "company", "create",
		"-id", c.ID, "-name", c.Name, "-admin-email", c.AdminEmail, "-admin-name", c.AdminName); err != nil {
		return fmt.Errorf("create the company: %w", err)
	}
	return nil
}

func (c *companyState) printSummary(env *envFile) {
	fmt.Printf("\nSparTask %s is running: %s\n", env.Get("SPARTASK_VERSION"), c.Address)
	fmt.Printf("Company: %s\nAdministrator: %s (sign in with the password you chose)\n", c.Name, c.AdminEmail)
	fmt.Println("\nNext steps:")
	domain := env.Get("SPARTASK_DOMAIN")
	if tlsMode(env) == tlsACME {
		fmt.Printf("  - DNS: the A record of %s points to this server; ports 80 and 443 stay open (Let's Encrypt renews the certificate)\n", domain)
	} else {
		fmt.Printf("  - DNS: %s points to this server in your network; replace certs/cert.pem and certs/key.pem before the certificate expires, then run: spartaskctl restart\n", domain)
	}
	printHandover()
}

// printHandover lists what the server's owner keeps safe after the installation.
func printHandover() {
	fmt.Println("  - Store a copy of .env (SECRET_KEY, SPARTASK_INSTALLATION_KEY, BACKUP_ENCRYPTION_KEY) in a password manager; backups cannot be decrypted without it.")
	fmt.Println("  - Backups are written to backups/ on this server: copy them to another place regularly, a lost server takes its backups with it.")
	fmt.Println("  - Mail and other settings: edit .env, then run: spartaskctl restart")
}
