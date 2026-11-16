package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"
)

// mcpKeyBits is the size of the signing key spartaskctl creates; the API refuses keys under 2048.
const mcpKeyBits = 2048

// mcpPlan is what saving the configuration does for MCP; computed without writing anything.
type mcpPlan struct {
	generate bool   // create .secrets/mcp/private.pem and public.pem
	public   []byte // write .secrets/mcp/public.pem, derived from the existing private.pem
}

// planMCP fills the MCP addresses derivable from SPARTASK_DOMAIN and checks everything the API
// refuses at start-up (it exits on any MCP misconfiguration), so update/restart stop instead.
func (c *installedConfig) planMCP(s stack) {
	env, result := c.env, c.envResult
	for _, key := range []string{"MCP_ENABLED", "MCP_TOKEN_ISSUANCE_ENABLED"} {
		if value := env.Get(key); value != "" && value != strings.ToLower(value) {
			result.Invalid = append(result.Invalid, fmt.Sprintf("%s=%s: küçük harfle true veya false yazın", key, value))
			return
		}
	}
	enabled, issuance := env.Get("MCP_ENABLED") == "true", env.Get("MCP_TOKEN_ISSUANCE_ENABLED") == "true"
	if issuance && !enabled {
		result.Invalid = append(result.Invalid, "MCP_TOKEN_ISSUANCE_ENABLED=true için MCP_ENABLED=true da olmalı")
		return
	}
	if !enabled {
		return
	}
	if domain := env.Get("SPARTASK_DOMAIN"); domain != "" {
		c.deriveMCP("MCP_RESOURCE_URL", "https://"+domain+"/mcp")
		c.deriveMCP("MCP_JWT_ISSUER", "https://"+domain)
	}
	invalid := len(result.Invalid)
	checkMCPURL(result, "MCP_RESOURCE_URL", env.Get("MCP_RESOURCE_URL"), "/mcp")
	checkMCPURL(result, "MCP_JWT_ISSUER", env.Get("MCP_JWT_ISSUER"), "")
	if raw := env.Get("MCP_TOKEN_MAX_TTL"); raw != "" {
		if ttl, err := time.ParseDuration(raw); err != nil || ttl < time.Minute || ttl > 7*24*time.Hour {
			result.Invalid = append(result.Invalid, "MCP_TOKEN_MAX_TTL: 1m ile 168h arasında bir süre olmalı (ör. 24h)")
		}
	}
	plan := c.planMCPKeys(s, issuance)
	if len(result.Invalid) == invalid {
		c.mcp = plan
	}
}

// deriveMCP writes value into an empty setting; existing values are never changed.
func (c *installedConfig) deriveMCP(key, value string) {
	if c.env.Get(key) != "" {
		return
	}
	c.env.Set(key, value)
	c.envResult.Derived = true
	c.envResult.Notes = append(c.envResult.Notes, fmt.Sprintf("%s boştu: %s yazıldı (harici bir issuer kullanıyorsanız .env içinde değiştirin)", key, value))
	missing := c.envResult.Missing[:0]
	for _, rule := range c.envResult.Missing {
		if rule.Key != key {
			missing = append(missing, rule)
		}
	}
	c.envResult.Missing = missing
}

func checkMCPURL(result *syncResult, key, raw, path string) {
	if raw == "" {
		return // reported by the template rule
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		result.Invalid = append(result.Invalid, fmt.Sprintf("%s=%s: https:// ile başlayan, sorgu içermeyen bir adres olmalı", key, raw))
		return
	}
	if path != "" && u.Path != path {
		result.Invalid = append(result.Invalid, fmt.Sprintf("%s=%s: yolu %s olmalı (ör. https://%s%s)", key, raw, path, u.Host, path))
	}
}

// planMCPKeys decides the key files. MCP_JWT_PUBLIC_KEY (an external issuer) wins over public.pem;
// token issuance needs private.pem matching whichever public key the API verifies with.
func (c *installedConfig) planMCPKeys(s stack, issuance bool) *mcpPlan {
	result := c.envResult
	privatePath, publicPath := s.path(".secrets", "mcp", "private.pem"), s.path(".secrets", "mcp", "public.pem")
	private, err := readMCPPrivateKey(privatePath)
	if err != nil {
		result.Invalid = append(result.Invalid, err.Error())
		return nil
	}
	if raw := c.env.Get("MCP_JWT_PUBLIC_KEY"); raw != "" {
		if !issuance {
			return &mcpPlan{}
		}
		if private == nil {
			result.Invalid = append(result.Invalid, "MCP_TOKEN_ISSUANCE_ENABLED=true: .secrets/mcp/private.pem yok. MCP_JWT_PUBLIC_KEY'i silin (spartaskctl anahtar çiftini üretir) ya da eşleşen özel anahtarı oraya koyun")
			return nil
		}
		public, err := parseMCPPublicKey([]byte(strings.ReplaceAll(raw, `\n`, "\n")))
		if err != nil {
			result.Notes = append(result.Notes, "MCP_JWT_PUBLIC_KEY okunamadı, .secrets/mcp/private.pem ile eşleştiği kontrol edilemedi; eşleşmezse API başlamaz")
			return &mcpPlan{}
		}
		if !private.PublicKey.Equal(public) {
			result.Invalid = append(result.Invalid, "MCP_JWT_PUBLIC_KEY, .secrets/mcp/private.pem ile eşleşmiyor")
			return nil
		}
		return &mcpPlan{}
	}
	publicData, err := os.ReadFile(publicPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		result.Invalid = append(result.Invalid, fmt.Sprintf("read .secrets/mcp/public.pem: %v", err))
		return nil
	}
	hasPublic := err == nil
	switch {
	case private == nil && !hasPublic:
		result.Notes = append(result.Notes, "MCP imzalama anahtarı yok: .secrets/mcp/private.pem ve public.pem üretilir (yedeklerde .secrets ile birlikte saklanır)")
		return &mcpPlan{generate: true}
	case private == nil:
		if issuance {
			result.Invalid = append(result.Invalid, "MCP_TOKEN_ISSUANCE_ENABLED=true: .secrets/mcp/public.pem var ama private.pem yok; token imzalanamaz. Eşleşen private.pem'i koyun ya da public.pem'i kaldırın (spartaskctl yeni çift üretir)")
			return nil
		}
		if _, err := parseMCPPublicKey(publicData); err != nil {
			result.Invalid = append(result.Invalid, fmt.Sprintf(".secrets/mcp/public.pem: %v", err))
			return nil
		}
		return &mcpPlan{}
	case !hasPublic:
		public, err := marshalMCPPublicKey(&private.PublicKey)
		if err != nil {
			result.Invalid = append(result.Invalid, err.Error())
			return nil
		}
		result.Notes = append(result.Notes, ".secrets/mcp/public.pem yok: mevcut private.pem'den üretilir")
		return &mcpPlan{public: public}
	}
	public, err := parseMCPPublicKey(publicData)
	if err != nil {
		result.Invalid = append(result.Invalid, fmt.Sprintf(".secrets/mcp/public.pem: %v", err))
		return nil
	}
	if !private.PublicKey.Equal(public) {
		result.Invalid = append(result.Invalid, ".secrets/mcp/public.pem, private.pem ile eşleşmiyor")
		return nil
	}
	return &mcpPlan{}
}

// saveMCP creates the planned key files. An existing private.pem is never replaced: every token
// it signed would stop working.
func (c *installedConfig) saveMCP(s stack) error {
	if c.mcp == nil || (!c.mcp.generate && c.mcp.public == nil) {
		return nil
	}
	if err := os.MkdirAll(s.path(".secrets", "mcp"), 0o700); err != nil {
		return fmt.Errorf("create MCP key directory: %w", err)
	}
	privatePath, publicPath := s.path(".secrets", "mcp", "private.pem"), s.path(".secrets", "mcp", "public.pem")
	public := c.mcp.public
	if c.mcp.generate {
		if _, err := os.Stat(privatePath); !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("%s appeared while configuring; run spartaskctl config again", privatePath)
		}
		key, err := rsa.GenerateKey(rand.Reader, mcpKeyBits)
		if err != nil {
			return fmt.Errorf("generate MCP signing key: %w", err)
		}
		der, err := x509.MarshalPKCS8PrivateKey(key)
		if err != nil {
			return fmt.Errorf("encode MCP signing key: %w", err)
		}
		if public, err = marshalMCPPublicKey(&key.PublicKey); err != nil {
			return err
		}
		if err := writeSecretFile(privatePath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})); err != nil {
			return err
		}
	}
	return writeSecretFile(publicPath, public)
}

// readMCPPrivateKey returns nil without error when the file does not exist.
func readMCPPrivateKey(path string) (*rsa.PrivateKey, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read .secrets/mcp/private.pem: %w", err)
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, errors.New(".secrets/mcp/private.pem: PEM biçiminde değil")
	}
	var key *rsa.PrivateKey
	if parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		rsaKey, ok := parsed.(*rsa.PrivateKey)
		if !ok {
			return nil, errors.New(".secrets/mcp/private.pem: RSA anahtarı değil")
		}
		key = rsaKey
	} else if key, err = x509.ParsePKCS1PrivateKey(block.Bytes); err != nil {
		return nil, errors.New(".secrets/mcp/private.pem: RSA özel anahtarı okunamadı")
	}
	if key.N.BitLen() < mcpKeyBits {
		return nil, fmt.Errorf(".secrets/mcp/private.pem: en az %d bit olmalı", mcpKeyBits)
	}
	return key, nil
}

func parseMCPPublicKey(data []byte) (*rsa.PublicKey, error) {
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, errors.New("PEM biçiminde değil")
	}
	parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		if key, pkcs1Err := x509.ParsePKCS1PublicKey(block.Bytes); pkcs1Err == nil {
			parsed = key
		} else {
			return nil, errors.New("RSA public key okunamadı")
		}
	}
	key, ok := parsed.(*rsa.PublicKey)
	if !ok || key.N.BitLen() < mcpKeyBits {
		return nil, fmt.Errorf("en az %d bit RSA public key olmalı", mcpKeyBits)
	}
	return key, nil
}

func marshalMCPPublicKey(key *rsa.PublicKey) ([]byte, error) {
	der, err := x509.MarshalPKIXPublicKey(key)
	if err != nil {
		return nil, fmt.Errorf("encode MCP public key: %w", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}), nil
}
