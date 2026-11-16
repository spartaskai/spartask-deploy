package main

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"os"
	"strings"
	"testing"
)

func mcpEnv(enabled, issuance string) string {
	env := strings.Replace(olderEnv, "MCP_ENABLED=false", "MCP_ENABLED="+enabled, 1)
	return strings.Replace(env, "MCP_TOKEN_ISSUANCE_ENABLED=false", "MCP_TOKEN_ISSUANCE_ENABLED="+issuance, 1)
}

func invalidContains(result *syncResult, text string) bool {
	for _, message := range result.Invalid {
		if strings.Contains(message, text) {
			return true
		}
	}
	return false
}

func TestPrepareConfigEnablesMCPWithGeneratedKeyPair(t *testing.T) {
	s := installedStack(t, mcpEnv("true", "true"))
	if _, err := prepareConfig(s); err != nil {
		t.Fatal(err)
	}
	saved, err := readEnvFile(s.path(".env"))
	if err != nil {
		t.Fatal(err)
	}
	if saved.Get("MCP_RESOURCE_URL") != "https://spartask.ai/mcp" || saved.Get("MCP_JWT_ISSUER") != "https://spartask.ai" {
		t.Fatalf("MCP addresses not derived: %q %q", saved.Get("MCP_RESOURCE_URL"), saved.Get("MCP_JWT_ISSUER"))
	}
	private, err := readMCPPrivateKey(s.path(".secrets", "mcp", "private.pem"))
	if err != nil || private == nil {
		t.Fatalf("private key not generated: %v", err)
	}
	publicData, err := os.ReadFile(s.path(".secrets", "mcp", "public.pem"))
	if err != nil {
		t.Fatal(err)
	}
	public, err := parseMCPPublicKey(publicData)
	if err != nil || !private.PublicKey.Equal(public) {
		t.Fatalf("public.pem must match private.pem: %v", err)
	}
	// A second run keeps the key: replacing it would invalidate every issued token.
	before, err := os.ReadFile(s.path(".secrets", "mcp", "private.pem"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := prepareConfig(s); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(s.path(".secrets", "mcp", "private.pem"))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("existing private.pem must never change: %v", err)
	}
}

func TestCheckConfigWritesNoMCPKey(t *testing.T) {
	s := installedStack(t, mcpEnv("true", "true"))
	cfg, err := checkInstalledConfig(s)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.blocking() || cfg.mcp == nil || !cfg.mcp.generate {
		t.Fatalf("expected a key generation plan, got invalid=%v missing=%v", cfg.envResult.Invalid, cfg.envResult.Missing)
	}
	if _, err := os.Stat(s.path(".secrets", "mcp", "private.pem")); !os.IsNotExist(err) {
		t.Fatalf("checking must not write the key: %v", err)
	}
}

func TestMCPConfigBlocksWhatTheAPIRefuses(t *testing.T) {
	cases := map[string]struct{ env, want string }{
		"issuance without MCP": {mcpEnv("false", "true"), "MCP_ENABLED=true da olmalı"},
		"upper-case flag":      {mcpEnv("True", "false"), "küçük harfle"},
		"resource path": {strings.Replace(mcpEnv("true", "true"), "MCP_ENABLED=true",
			"MCP_ENABLED=true\nMCP_RESOURCE_URL=https://spartask.ai/api/mcp", 1), "yolu /mcp olmalı"},
		"plain http issuer": {strings.Replace(mcpEnv("true", "false"), "MCP_ENABLED=true",
			"MCP_ENABLED=true\nMCP_JWT_ISSUER=http://spartask.ai", 1), "https://"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			cfg, err := checkInstalledConfig(installedStack(t, tc.env))
			if err != nil {
				t.Fatal(err)
			}
			if !invalidContains(cfg.envResult, tc.want) || cfg.mcp != nil {
				t.Fatalf("want invalid %q and no key plan, got %v", tc.want, cfg.envResult.Invalid)
			}
		})
	}
}

func TestMCPConfigRejectsMismatchedKeyFiles(t *testing.T) {
	s := installedStack(t, mcpEnv("true", "true"))
	if err := os.MkdirAll(s.path(".secrets", "mcp"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeKey := func(name string, key *rsa.PrivateKey, public bool) {
		var block *pem.Block
		if public {
			der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
			if err != nil {
				t.Fatal(err)
			}
			block = &pem.Block{Type: "PUBLIC KEY", Bytes: der}
		} else {
			block = &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}
		}
		if err := os.WriteFile(s.path(".secrets", "mcp", name), pem.EncodeToMemory(block), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	first, err := rsa.GenerateKey(rand.Reader, mcpKeyBits)
	if err != nil {
		t.Fatal(err)
	}
	second, err := rsa.GenerateKey(rand.Reader, mcpKeyBits)
	if err != nil {
		t.Fatal(err)
	}
	writeKey("private.pem", first, false)
	// Only private.pem (PKCS#1): public.pem is derived from it.
	cfg, err := checkInstalledConfig(s)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.blocking() || cfg.mcp == nil || cfg.mcp.generate || cfg.mcp.public == nil {
		t.Fatalf("expected public.pem derived from the existing key, got %v", cfg.envResult.Invalid)
	}
	writeKey("public.pem", second, true)
	if cfg, err = checkInstalledConfig(s); err != nil {
		t.Fatal(err)
	}
	if !invalidContains(cfg.envResult, "eşleşmiyor") {
		t.Fatalf("mismatched key files must block, got %v", cfg.envResult.Invalid)
	}
}
