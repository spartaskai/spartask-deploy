package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"
)

// validateFormats rejects values the application would refuse at start-up.
func validateFormats(env *envFile, result *syncResult) {
	if _, err := parseCIDRList(env.Get("SPARTASK_EGRESS_DENY_CIDRS")); err != nil {
		result.Invalid = append(result.Invalid, fmt.Sprintf("SPARTASK_EGRESS_DENY_CIDRS: %v (virgülle ayrılmış IP veya CIDR olmalı)", err))
	}
	if raw := strings.TrimSpace(env.Get("SPARTASK_EGRESS_PRIVATE_GRANTS")); raw != "" {
		var grants []map[string]any
		if err := json.Unmarshal([]byte(raw), &grants); err != nil {
			result.Invalid = append(result.Invalid, "SPARTASK_EGRESS_PRIVATE_GRANTS: geçerli bir JSON listesi değil (tek tırnak içine alın: '[...]')")
		}
	}
}

func parseCIDRList(raw string) ([]netip.Prefix, error) {
	var prefixes []netip.Prefix
	for _, item := range strings.Split(raw, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if !strings.Contains(item, "/") {
			addr, err := netip.ParseAddr(item)
			if err != nil {
				return nil, fmt.Errorf("%q geçersiz", item)
			}
			prefixes = append(prefixes, netip.PrefixFrom(addr, addr.BitLen()))
			continue
		}
		prefix, err := netip.ParsePrefix(item)
		if err != nil {
			return nil, fmt.Errorf("%q geçersiz", item)
		}
		prefixes = append(prefixes, prefix)
	}
	return prefixes, nil
}

type tenantRow struct{ ID, Subdomain, Name string }

// advisorProbes look at the running server; tests replace them.
type advisorProbes struct {
	publicIP func(ctx context.Context) (netip.Addr, error)
	lookup   func(ctx context.Context, host string) ([]netip.Addr, error)
	tenants  func(ctx context.Context, env *envFile) ([]tenantRow, error)
}

func defaultAdvisorProbes(s stack) advisorProbes {
	return advisorProbes{
		publicIP: digitalOceanPublicIP,
		lookup: func(ctx context.Context, host string) ([]netip.Addr, error) {
			ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		},
		tenants: func(ctx context.Context, env *envFile) ([]tenantRow, error) {
			user, db := env.Get("DB_USER"), env.Get("DB_NAME")
			out, err := s.composeOutput(ctx, "exec", "-T", "db", "psql", "-U", user, "-d", db, "-At", "-F", "\t",
				"-c", "SELECT id, subdomain, name FROM public.tenants ORDER BY created_at")
			if err != nil {
				return nil, err
			}
			var rows []tenantRow
			for _, line := range strings.Split(out, "\n") {
				fields := strings.SplitN(line, "\t", 3)
				if len(fields) == 3 {
					rows = append(rows, tenantRow{ID: fields[0], Subdomain: fields[1], Name: fields[2]})
				}
			}
			return rows, nil
		},
	}
}

// digitalOceanPublicIP asks the droplet metadata service; other hosts simply have no answer.
func digitalOceanPublicIP(ctx context.Context) (netip.Addr, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://169.254.169.254/metadata/v1/interfaces/public/0/ipv4/address", nil)
	if err != nil {
		return netip.Addr{}, err
	}
	resp, err := (&http.Client{Transport: &http.Transport{Proxy: nil}}).Do(req)
	if err != nil {
		return netip.Addr{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return netip.Addr{}, fmt.Errorf("metadata: HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64))
	if err != nil {
		return netip.Addr{}, err
	}
	return netip.ParseAddr(strings.TrimSpace(string(body)))
}

// advise adds server-specific suggestions for the settings operators find hardest.
func advise(ctx context.Context, cfg *installedConfig, probes advisorProbes) {
	adviseEgressDeny(ctx, cfg.env, cfg.envResult, probes)
	advisePlatformTenants(ctx, cfg.env, cfg.envResult, probes)
}

func adviseEgressDeny(ctx context.Context, env *envFile, result *syncResult, probes advisorProbes) {
	domain := env.Get("SPARTASK_DOMAIN")
	if domain == "" || probes.publicIP == nil {
		return
	}
	serverIP, err := probes.publicIP(ctx)
	if err != nil {
		return // not a DigitalOcean droplet: nothing reliable to suggest
	}
	domainIPs, err := probes.lookup(ctx, domain)
	if err != nil {
		result.Notes = append(result.Notes, fmt.Sprintf("SPARTASK_EGRESS_DENY_CIDRS: %s çözülemedi, öneri yapılamadı.", domain))
		return
	}
	direct := false
	for _, ip := range domainIPs {
		if ip.Unmap() == serverIP {
			direct = true
		}
	}
	denied := false
	if prefixes, err := parseCIDRList(env.Get("SPARTASK_EGRESS_DENY_CIDRS")); err == nil {
		for _, prefix := range prefixes {
			if prefix.Contains(serverIP) {
				denied = true
			}
		}
	}
	switch {
	case !direct && !denied:
		result.Notes = append(result.Notes, fmt.Sprintf(
			"SPARTASK_EGRESS_DENY_CIDRS: Sunucunun public IP'si %s; %s ise başka adreslere (Cloudflare) çözülüyor.\n"+
				"Firma süreçlerinin sunucuya doğrudan IP ile bağlanmasını engellemek için .env'e yazın:\n"+
				"SPARTASK_EGRESS_DENY_CIDRS=%s", serverIP, domain, mergeCIDR(env.Get("SPARTASK_EGRESS_DENY_CIDRS"), serverIP.String())))
	case direct && denied:
		result.Notes = append(result.Notes, fmt.Sprintf(
			"SPARTASK_EGRESS_DENY_CIDRS: %s doğrudan bu sunucuya (%s) çözülüyor ama bu IP engelli listesinde.\n"+
				"Firma süreçlerinin kendi Spartask adresinize çağrıları (ör. platform API'si) engellenir; IP'yi listeden çıkarın.", domain, serverIP))
	}
}

func mergeCIDR(existing, ip string) string {
	if strings.TrimSpace(existing) == "" {
		return ip
	}
	return strings.TrimSpace(existing) + "," + ip
}

func advisePlatformTenants(ctx context.Context, env *envFile, result *syncResult, probes advisorProbes) {
	configured := map[string]bool{}
	for _, id := range strings.Split(env.Get("SPARTASK_PLATFORM_TENANT_IDS"), ",") {
		if id = strings.TrimSpace(id); id != "" {
			configured[id] = true
		}
	}
	if probes.tenants == nil {
		return
	}
	rows, err := probes.tenants(ctx, env)
	if err != nil {
		if len(configured) == 0 {
			result.Notes = append(result.Notes, "SPARTASK_PLATFORM_TENANT_IDS: firmalar okunamadı (stack çalışıyor mu?). Elle bakmak için:\n"+
				`sudo docker compose exec db psql -U spartask spartask -c "SELECT id, subdomain, name FROM public.tenants"`)
		}
		return
	}
	if len(configured) == 0 {
		if len(rows) == 0 {
			return
		}
		var b strings.Builder
		b.WriteString("SPARTASK_PLATFORM_TENANT_IDS: Platformu işleten firmanın id'sini yazın. Sunucudaki firmalar:")
		for _, row := range rows {
			fmt.Fprintf(&b, "\n  %s   %s   %s", row.ID, row.Subdomain, row.Name)
		}
		result.Notes = append(result.Notes, b.String())
		return
	}
	exists := map[string]bool{}
	for _, row := range rows {
		exists[row.ID] = true
	}
	for id := range configured {
		if !exists[id] {
			result.Notes = append(result.Notes, fmt.Sprintf("SPARTASK_PLATFORM_TENANT_IDS: %q kimliğinde firma yok; değeri kontrol edin.", id))
		}
	}
}
