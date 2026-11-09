package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net/netip"
	"slices"
	"strings"
	"time"
)

// TLS modes of an installation, read from SPARTASK_TLS (see .env.example).
const (
	tlsACME     = "acme"     // an e-mail address: Caddy gets the certificate from Let's Encrypt
	tlsFiles    = "files"    // /certs/cert.pem /certs/key.pem: the operator's certificate files
	tlsInternal = "internal" // Caddy's own CA (closed networks)
)

// certificateFilesValue is SPARTASK_TLS for certificate files in certs/.
const certificateFilesValue = "/certs/cert.pem /certs/key.pem"

// tlsMode tells how the installation in env gets its certificate.
func tlsMode(env *envFile) string {
	switch value := env.Get("SPARTASK_TLS"); {
	case value == tlsInternal:
		return tlsInternal
	case strings.Contains(value, "@"):
		return tlsACME
	default:
		return tlsFiles
	}
}

// servedNames are the addresses Caddy serves (SPARTASK_SITES; compose's default is the domain and
// every company subdomain).
func servedNames(env *envFile) []string {
	sites := strings.TrimSpace(env.Get("SPARTASK_SITES"))
	if sites == "" {
		domain := env.Get("SPARTASK_DOMAIN")
		return []string{domain, "*." + domain}
	}
	var names []string
	for _, site := range strings.Split(sites, ",") {
		if site = strings.TrimSpace(site); site != "" {
			names = append(names, site)
		}
	}
	return names
}

// checkCertificate verifies the certificate files in certs/ before Caddy needs them: the key
// belongs to the certificate, the certificate is valid now and covers every served name. A
// wildcard name (*.domain) must appear in the certificate as it is.
func checkCertificate(s stack, names []string, now time.Time) error {
	pair, err := tls.LoadX509KeyPair(s.path("certs", "cert.pem"), s.path("certs", "key.pem"))
	if err != nil {
		return fmt.Errorf("certs/cert.pem and certs/key.pem are not a certificate and its key: %w", err)
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return fmt.Errorf("read certs/cert.pem: %w", err)
	}
	if now.Before(leaf.NotBefore) || now.After(leaf.NotAfter) {
		return fmt.Errorf("the certificate in certs/cert.pem is valid from %s to %s, not now",
			leaf.NotBefore.Format(time.DateOnly), leaf.NotAfter.Format(time.DateOnly))
	}
	for _, name := range names {
		covered := slices.Contains(leaf.DNSNames, name)
		if !strings.HasPrefix(name, "*.") {
			covered = leaf.VerifyHostname(name) == nil
		}
		if !covered {
			return fmt.Errorf("the certificate in certs/cert.pem does not cover %s (it covers: %s)", name, strings.Join(leaf.DNSNames, ", "))
		}
	}
	return nil
}

// checkServedAddress makes sure the installation's address can work before the stack starts:
// certificate files must fit it, and Let's Encrypt needs a DNS record pointing to the server.
func checkServedAddress(ctx context.Context, s stack, env *envFile, lookup func(context.Context, string) ([]netip.Addr, error), now time.Time) error {
	switch tlsMode(env) {
	case tlsFiles:
		if err := checkCertificate(s, servedNames(env), now); err != nil {
			return fmt.Errorf("%w\nPut the right certificate (full chain) and key into %s as cert.pem and key.pem", err, s.path("certs"))
		}
	case tlsACME:
		domain := env.Get("SPARTASK_DOMAIN")
		if addrs, err := lookup(ctx, domain); err != nil || len(addrs) == 0 {
			return errors.Join(fmt.Errorf("%s has no DNS record: add an A record pointing to this server's public IP address "+
				"(Let's Encrypt checks it over ports 80 and 443 from the internet)", domain), err)
		}
	}
	return nil
}
