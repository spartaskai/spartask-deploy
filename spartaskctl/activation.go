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
// that address itself (SPARTASK_ROOT_TENANT), Caddy gets the certificate from Let's Encrypt,
// company sign-up is closed (the administrator is created here) and the license renews itself
// (SPARTASK_LICENSE_SERVER).

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

// companyPlan is the first company of an activated installation, created once the stack runs.
type companyPlan struct {
	id, name string
	address  string
	license  string
	admin    adminAccount
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
	o.tls = "acme"
	return nil
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
// release image, with the new installation key) and points env at the registered address.
func activate(ctx context.Context, s stack, env *envFile, o *installOptions) (*companyPlan, error) {
	image := env.Get("SPARTASK_IMAGE") + ":" + o.version
	fmt.Printf("Activating with %s ...\n", o.licenseServer)
	pull := s.command(ctx, "pull", image)
	pull.Stdout, pull.Stderr = os.Stdout, os.Stderr
	if err := pull.Run(); err != nil {
		return nil, fmt.Errorf("docker pull %s: %w (private images need -registry-user)", image, err)
	}
	// The key reaches the container through the environment, never the command line.
	run := s.command(ctx, "run", "--rm", "-e", installationKeyVariable, "--entrypoint", "/app/bin/spartask-admin", image,
		"license", "activate", "-code", o.activation, "-server", o.licenseServer)
	run.Env = append(os.Environ(), installationKeyVariable+"="+env.Get(installationKeyVariable))
	var out bytes.Buffer
	run.Stdout, run.Stderr = &out, os.Stderr
	if err := run.Run(); err != nil {
		return nil, licenseAdmin(fmt.Errorf("activation failed (nothing was installed; run install again with a new code if it expired): %w", err))
	}
	var answer activationAnswer
	if err := json.Unmarshal(out.Bytes(), &answer); err != nil {
		return nil, fmt.Errorf("read the activation answer: %w", err)
	}
	return applyActivation(env, o, answer)
}

// applyActivation sets the installation's single address, its company and closed sign-up.
func applyActivation(env *envFile, o *installOptions, answer activationAnswer) (*companyPlan, error) {
	domain, err := addressDomain(answer.BaseURL)
	if err != nil {
		return nil, err
	}
	id := o.companyID
	if id == "" {
		id = companyIDFromName(answer.OrganizationName)
	}
	if !companyIDPattern.MatchString(id) {
		return nil, fmt.Errorf("cannot make a company address id from %q: pass -company-id (lower-case letters, digits and '-', at most 43)", answer.OrganizationName)
	}
	if answer.License == "" || answer.OrganizationName == "" {
		return nil, errors.New("the activation answer is incomplete")
	}
	o.domain = domain
	env.Set("SPARTASK_DOMAIN", domain)
	env.Set("SPARTASK_SITES", domain)
	env.Set("SPARTASK_ROOT_TENANT", id)
	env.Set("SPARTASK_SIGNUP_MODE", "closed")
	// The company runs the installation: its administrators see the license and accept a changed
	// agreement, and the scheduler renews the license with the license service.
	env.Set("SPARTASK_PLATFORM_TENANT_IDS", id)
	env.Set("SPARTASK_LICENSE_SERVER", o.licenseServer)
	if err := applyMarketplace(env, o, answer); err != nil {
		return nil, err
	}
	return &companyPlan{id: id, name: answer.OrganizationName, address: "https://" + domain, license: answer.License, admin: o.admin}, nil
}

// applyMarketplace lets the company use the marketplace as the installation: the marketplace knows
// it by the installation's marketplace id, and its executor licenses are the installation's. The
// marketplace is the one the license service names unless -marketplace-url chose another.
func applyMarketplace(env *envFile, o *installOptions, answer activationAnswer) error {
	if answer.MarketplaceID == "" {
		return nil
	}
	env.Set("SPARTASK_MARKETPLACE_COMPANY", answer.MarketplaceID)
	if o.marketplaceURL != "" || answer.MarketplaceURL == "" {
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

// create installs the license and creates the company with its administrator in the running
// stack. The password reaches spartask-admin on standard input.
func (c *companyPlan) create(ctx context.Context, s stack) error {
	if err := s.composeStream(ctx, strings.NewReader(c.license), os.Stdout,
		"exec", "-T", "api", "/app/bin/spartask-admin", "license", "install", "-f", "-"); err != nil {
		path := s.path("license.jws")
		if saveErr := os.WriteFile(path, []byte(c.license), 0o600); saveErr != nil {
			return fmt.Errorf("install the license: %w (saving it to %s failed too: %v)", err, path, saveErr)
		}
		return fmt.Errorf("install the license: %w\nThe license is saved; after fixing the cause run: spartaskctl license install %s", err, path)
	}
	args := []string{"exec", "-T", "api", "/app/bin/spartask-admin", "company", "create",
		"-id", c.id, "-name", c.name, "-admin-email", c.admin.email, "-admin-name", c.admin.name}
	if err := s.composeStream(ctx, strings.NewReader(c.admin.password+"\n"), os.Stdout, args...); err != nil {
		return fmt.Errorf("create the company: %w\nThe installation is running; after fixing the cause run (the password on the first line of standard input):\n  docker compose %s",
			err, strings.Join(args, " "))
	}
	return nil
}

func (c *companyPlan) printSummary(o installOptions) {
	fmt.Printf("\nSpartask %s is running: %s\n", o.version, c.address)
	fmt.Printf("Company: %s\nAdministrator: %s (sign in with the password you chose)\n", c.name, c.admin.email)
	fmt.Println("\nNext steps:")
	fmt.Printf("  - DNS: an A record for %s points to this server; ports 80 and 443 are open (the certificate comes from Let's Encrypt)\n", o.domain)
	fmt.Println("  - Store a copy of .env (SECRET_KEY, SPARTASK_INSTALLATION_KEY) in a password manager; backups cannot be decrypted without it.")
	fmt.Println("  - Mail and other settings: edit .env, then run: spartaskctl restart")
}
