package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
)

// randomHex returns 2*n hex characters from n random bytes.
func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate random secret: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// readMarketplaceSigningKey reads the marketplace license signing key from a file: the base64
// Ed25519 private key (64 bytes) or seed (32 bytes), bare or as an ED25519_PRIVATE_KEY= line. It
// returns the private key and its public key, standard base64. The key is never generated here:
// SparTask releases trust only the marketplace keys built into them.
func readMarketplaceSigningKey(path string) (private, public string, err error) {
	if strings.TrimSpace(path) == "" {
		return "", "", fmt.Errorf("the marketplace needs its signing key: pass -marketplace-signing-key <file> (ED25519_PRIVATE_KEY of the platform marketplace)")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", "", fmt.Errorf("read marketplace signing key: %w", err)
	}
	value := strings.TrimSpace(string(raw))
	for _, line := range strings.Split(value, "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), "ED25519_PRIVATE_KEY="); ok {
			value = strings.TrimSpace(v)
		}
	}
	b, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		return "", "", fmt.Errorf("marketplace signing key is not base64: %w", err)
	}
	var key ed25519.PrivateKey
	switch len(b) {
	case ed25519.SeedSize:
		key = ed25519.NewKeyFromSeed(b)
	case ed25519.PrivateKeySize:
		key = ed25519.PrivateKey(b)
		if !ed25519.NewKeyFromSeed(key.Seed()).Equal(key) {
			return "", "", fmt.Errorf("marketplace signing key is inconsistent (public half does not match the seed)")
		}
	default:
		return "", "", fmt.Errorf("marketplace signing key must be 32 or 64 bytes, got %d", len(b))
	}
	pub := key.Public().(ed25519.PublicKey)
	return base64.StdEncoding.EncodeToString(key), base64.StdEncoding.EncodeToString(pub), nil
}

// cloudflareRanges are the published Cloudflare edge networks (https://www.cloudflare.com/ips/).
var cloudflareRanges = []string{
	"173.245.48.0/20", "103.21.244.0/22", "103.22.200.0/22", "103.31.4.0/22", "141.101.64.0/18",
	"108.162.192.0/18", "190.93.240.0/20", "188.114.96.0/20", "197.234.240.0/22", "198.41.128.0/17",
	"162.158.0.0/15", "104.16.0.0/13", "104.24.0.0/14", "172.64.0.0/13", "131.0.72.0/22",
	"2400:cb00::/32", "2606:4700::/32", "2803:f800::/32", "2405:b500::/32", "2405:8100::/32",
	"2a06:98c0::/29", "2c0f:f248::/32",
}

func cloudflareTrustedProxies() string {
	return strings.Join(cloudflareRanges, " ")
}
