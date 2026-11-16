package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const meetingComposeFile = "compose.meeting.generated.yml"

var (
	meetingImagePattern  = regexp.MustCompile(`^([a-z0-9][a-z0-9.-]*(:[0-9]+)?/)?[a-z0-9]+([._-][a-z0-9]+)*(/[a-z0-9]+([._-][a-z0-9]+)*)*@sha256:[a-f0-9]{64}$`)
	meetingMemoryPattern = regexp.MustCompile(`^[1-9][0-9]*[mg]$`)
)

// meetingPlan is desired state, computed without Docker calls or file writes. Missing optional
// settings disable that feature; malformed populated settings block an apply. Requested flags
// stay in .env; the generated overlay supplies the effective flags to every app process.
type meetingPlan struct {
	media       bool
	services    map[string]any
	env         map[string]string
	caddy       string
	caddyDomain string
	notes       []string
	invalid     []string
}

func planMeeting(s stack, env *envFile) *meetingPlan {
	p := &meetingPlan{services: map[string]any{}, env: map[string]string{}}
	browser := meetingEnabled(env, "MEETING_BROWSER_SPEECH_ENABLED")
	for _, key := range env.keys() {
		if !retiredServerSpeechKey(key) && (strings.HasPrefix(key, "MEETING_") || strings.HasPrefix(key, "LIVEKIT_")) {
			p.env[key] = env.Get(key)
		}
	}
	if meetingEnabled(env, "MEETING_MEDIA_ENABLED") {
		p.media = p.mediaPlan(env)
	}
	if browser {
		if !p.media {
			p.invalid = append(p.invalid, "Tarayıcı konuşması için görüntülü toplantı altyapısı hazır olmalı")
		}
		language := envOr(env, "MEETING_BROWSER_SPEECH_LANGUAGE", "tr")
		if language != "tr" && language != "en" && language != "de" {
			p.invalid = append(p.invalid, "MEETING_BROWSER_SPEECH_LANGUAGE: tr, en veya de olmalı")
		}
		p.env["MEETING_BROWSER_SPEECH_LANGUAGE"] = language
		p.notes = append(p.notes, "STT ve TTS tarayıcıda; voice, speech-stt ve speech-tts sunucu servisleri kapalı.")
	}
	p.env["MEETING_MEDIA_ENABLED"] = strconv.FormatBool(p.media)
	if p.media {
		p.duration(env, "MEETING_CLOSE_DRAIN_TIMEOUT", time.Second, time.Minute)
	}
	if len(p.services) > 0 {
		p.duration(env, "MEETING_SERVICES_START_TIMEOUT", 30*time.Second, 15*time.Minute)
	}
	for _, feature := range []struct {
		name string
		on   bool
	}{{"Görüntülü toplantı", p.media}, {"Tarayıcı konuşması", browser}} {
		state := "KAPALI"
		if feature.on {
			state = "AÇIK"
		}
		p.notes = append(p.notes, feature.name+": "+state+" (restart ile uygulanır).")
	}
	return p
}

func (p *meetingPlan) complete(env *envFile, feature string, keys ...string) bool {
	var missing []string
	for _, key := range keys {
		if strings.TrimSpace(env.Get(key)) == "" {
			missing = append(missing, key)
		}
	}
	if len(missing) == 0 {
		return true
	}
	p.notes = append(p.notes, feature+" KAPALI; eksik ayarlar: "+strings.Join(missing, ", ")+". Ana uygulama çalışmaya devam edebilir.")
	return false
}

func (p *meetingPlan) mediaPlan(env *envFile) bool {
	keys := []string{"LIVEKIT_URL", "LIVEKIT_API_KEY", "LIVEKIT_API_SECRET"}
	local := meetingEnabled(env, "LIVEKIT_MANAGED")
	if local {
		keys = append(keys, "LIVEKIT_IMAGE", "LIVEKIT_PUBLIC_IP")
	} else {
		keys = append(keys, "LIVEKIT_INTERNAL_URL")
	}
	if !p.complete(env, "Görüntülü toplantı", keys...) {
		return false
	}
	u := p.endpoint(env.Get("LIVEKIT_URL"), "LIVEKIT_URL", "wss")
	internal := env.Get("LIVEKIT_INTERNAL_URL")
	if local && internal == "" && u != nil {
		internal = "https://" + u.Host
		p.env["LIVEKIT_INTERNAL_URL"] = internal
	}
	p.endpoint(internal, "LIVEKIT_INTERNAL_URL", "https")
	if len(env.Get("LIVEKIT_API_SECRET")) < 32 {
		p.invalid = append(p.invalid, "LIVEKIT_API_SECRET: en az 32 karakter olmalı")
	}
	if raw := env.Get("MEETING_MEDIA_ALLOW_INSECURE_LOCAL"); raw != "" && !strings.EqualFold(raw, "false") {
		p.invalid = append(p.invalid, "MEETING_MEDIA_ALLOW_INSECURE_LOCAL: canlı dağıtımda kullanılamaz")
	}
	if !local {
		return true
	}
	p.managedMedia(env, u)
	return true
}

func (p *meetingPlan) managedMedia(env *envFile, u *url.URL) {
	p.image(env, "LIVEKIT_IMAGE")
	p.capacity(env, "LIVEKIT")
	tcp, udp := p.integer(env, "LIVEKIT_TCP_PORT", 1024, 65535), p.integer(env, "LIVEKIT_UDP_PORT", 1024, 65535)
	ip, err := netip.ParseAddr(env.Get("LIVEKIT_PUBLIC_IP"))
	if err != nil || !ip.Is4() || ip.IsLoopback() || ip.IsUnspecified() || ip.IsMulticast() {
		p.invalid = append(p.invalid, "LIVEKIT_PUBLIC_IP: istemcilerin erişebileceği IPv4 adresi gerekli")
	}
	if u == nil {
		return
	}
	if !domainPattern.MatchString(u.Hostname()) || u.Port() != "" || strings.EqualFold(u.Hostname(), env.Get("SPARTASK_DOMAIN")) {
		p.invalid = append(p.invalid, "LIVEKIT_URL: yönetilen LiveKit ayrı bir DNS alan adı ve varsayılan WSS portu kullanmalı")
	}
	tls := envOr(env, "LIVEKIT_TLS", env.Get("SPARTASK_TLS"))
	if !validMeetingTLS(tls) {
		p.invalid = append(p.invalid, "LIVEKIT_TLS: internal, ACME e-postası veya /certs/cert.pem /certs/key.pem olmalı")
	}
	p.caddy = fmt.Sprintf("%s {\n tls %s\n reverse_proxy livekit:7880\n}\n", u.Hostname(), tls)
	p.caddyDomain = u.Hostname()
	config := map[string]any{"port": 7880, "bind_addresses": []string{"0.0.0.0"}, "keys": map[string]string{env.Get("LIVEKIT_API_KEY"): env.Get("LIVEKIT_API_SECRET")}, "rtc": map[string]any{"tcp_port": tcp, "udp_port": udp, "node_ip": env.Get("LIVEKIT_PUBLIC_IP"), "use_external_ip": false}, "room": map[string]any{"auto_create": false}}
	// This tree contains only JSON-supported values; keep error propagation explicit.
	body, err := json.Marshal(config)
	if err != nil {
		p.invalid = append(p.invalid, "LiveKit yapılandırması üretilemedi")
		return
	}
	v := p.container(env.Get("LIVEKIT_IMAGE"), env, "LIVEKIT")
	v["environment"] = map[string]string{"LIVEKIT_CONFIG": string(body)}
	v["ports"] = []string{fmt.Sprintf("%d:%d/tcp", tcp, tcp), fmt.Sprintf("%d:%d/udp", udp, udp)}
	p.services["livekit"] = v
	p.notes = append(p.notes, "Yönetilen LiveKit: meeting DNS/TLS ve WebRTC TCP/UDP portlarını hazırlayın. Bu profil otomatik TURN kurmaz; kısıtlı ağlar için TURN yapılandırılmış harici LiveKit kullanılabilir.")
}

func validMeetingTLS(raw string) bool {
	if raw == "internal" || raw == "/certs/cert.pem /certs/key.pem" {
		return true
	}
	// Prevent a dotenv value from introducing Caddy directives or additional site labels.
	return regexp.MustCompile(`^[a-zA-Z0-9._+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`).MatchString(raw)
}

func (p *meetingPlan) endpoint(raw, key, scheme string) *url.URL {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || (u.Path != "" && u.Path != "/") || u.Scheme != scheme {
		p.invalid = append(p.invalid, key+": güvenli ve kimlik bilgisi/query içermeyen servis adresi gerekli")
		return nil
	}
	return u
}

func (p *meetingPlan) image(env *envFile, key string) {
	if !meetingImagePattern.MatchString(env.Get(key)) {
		p.invalid = append(p.invalid, key+": onaylanmış repository@sha256:<64 küçük hex> imaj referansı gerekli")
	}
}

func meetingEnabled(env *envFile, key string) bool { return strings.EqualFold(env.Get(key), "true") }

func (p *meetingPlan) integer(env *envFile, key string, min, max int) int {
	n, err := strconv.Atoi(env.Get(key))
	if err != nil || n < min || n > max {
		p.invalid = append(p.invalid, fmt.Sprintf("%s: %d–%d arasında tamsayı olmalı", key, min, max))
	}
	return n
}

func (p *meetingPlan) duration(env *envFile, key string, min, max time.Duration) {
	d, err := time.ParseDuration(env.Get(key))
	if err != nil || d < min || d > max {
		p.invalid = append(p.invalid, fmt.Sprintf("%s: %s–%s arasında süre olmalı", key, min, max))
	}
}

func (p *meetingPlan) capacity(env *envFile, prefix string) {
	cpu, err := strconv.ParseFloat(env.Get(prefix+"_CPUS"), 64)
	if err != nil || !(cpu > 0 && cpu <= 256) {
		p.invalid = append(p.invalid, prefix+"_CPUS: 0'dan büyük, en fazla 256 olmalı")
	}
	if !meetingMemoryPattern.MatchString(env.Get(prefix + "_MEMORY")) {
		p.invalid = append(p.invalid, prefix+"_MEMORY: 512m veya 2g gibi pozitif bellek sınırı gerekli")
	}
}

func (p *meetingPlan) container(image string, env *envFile, prefix string) map[string]any {
	return map[string]any{
		"image": image, "restart": "unless-stopped", "init": true,
		"networks": []string{"spartask"},
		"cpus":     env.Get(prefix + "_CPUS"), "mem_limit": env.Get(prefix + "_MEMORY"),
		"pids_limit": 128, "cap_drop": []string{"ALL"}, "security_opt": []string{"no-new-privileges:true"},
		"logging": map[string]any{"driver": "json-file", "options": map[string]string{"max-size": "10m", "max-file": "5"}},
	}
}

func (p *meetingPlan) document(env *envFile) ([]byte, error) {
	services := map[string]any{}
	for name, service := range p.services {
		services[name] = service
	}
	for _, name := range []string{"api", "worker", "scheduler"} {
		services[name] = map[string]any{"environment": p.env}
	}
	// Content changes force Caddy recreation; a changed bind-mounted file alone does not reload it.
	hash := sha256.Sum256([]byte(p.caddy))
	caddy := map[string]any{"environment": map[string]string{"SPARTASK_MEETING_CONFIG_HASH": fmt.Sprintf("%x", hash)}}
	if p.caddy != "" {
		caddy["volumes"] = []string{"./.secrets/meeting.caddy:/etc/caddy/sites/meeting.caddy:ro"}
		// Preserve TLS/SNI while routing internal API/voice traffic straight to Caddy, avoiding
		// public-IP hairpin NAT. Compose merges other networks (e.g. marketplace-front).
		caddy["networks"] = map[string]any{"spartask": map[string]any{"aliases": []string{p.caddyDomain}}}
	}
	services["caddy"] = caddy
	doc := map[string]any{"services": services}
	body, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode optional meeting compose: %w", err)
	}
	// JSON is valid YAML. Escape every literal dollar so Compose cannot substitute secrets,
	// provider env paths or Python commands using the invoking shell's environment.
	return append(bytes.ReplaceAll(body, []byte("$"), []byte("$$")), '\n'), nil
}

func (c *installedConfig) planMeeting(s stack) {
	c.meeting = planMeeting(s, c.env)
	c.envResult.Invalid = append(c.envResult.Invalid, c.meeting.invalid...)
	c.envResult.Notes = append(c.envResult.Notes, c.meeting.notes...)
	if len(c.meeting.invalid) > 0 {
		return
	}
	files := strings.Split(c.env.Get("COMPOSE_FILE"), ":")
	for _, file := range files {
		if file == meetingComposeFile {
			return
		}
	}
	c.env.Set("COMPOSE_FILE", c.env.Get("COMPOSE_FILE")+":"+meetingComposeFile)
	c.envResult.Derived = true
}

func (c *installedConfig) saveMeeting(s stack) error {
	if c.meeting == nil || len(c.meeting.invalid) > 0 {
		return nil
	}
	body, err := c.meeting.document(c.env)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.path(".secrets"), 0o700); err != nil {
		return fmt.Errorf("create meeting secrets directory: %w", err)
	}
	if err := writeChangedSecret(s.path(".secrets", "meeting.caddy"), []byte(c.meeting.caddy)); err != nil {
		return err
	}
	return writeChangedSecret(s.path(meetingComposeFile), body)
}

func writeChangedSecret(path string, data []byte) error {
	old, err := os.ReadFile(path)
	if err == nil && bytes.Equal(old, data) {
		return nil
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read generated meeting file: %w", err)
	}
	return writeSecretFile(path, data)
}

// Application health is checked separately. Model images must implement the documented warm
// /health probe. Voice and the official SFU have no health endpoint in their images: report
// only stable running state, never claim that this replaces a browser/voice acceptance test.
func waitMeetingServices(ctx context.Context, s stack, env *envFile) error {
	p := planMeeting(s, env)
	if len(p.services) == 0 {
		return nil
	}
	timeout, err := time.ParseDuration(envOr(env, "MEETING_SERVICES_START_TIMEOUT", "180s"))
	if err != nil {
		return fmt.Errorf("parse optional service startup timeout: %w", err)
	}
	return waitMeetingRunning(ctx, s, "livekit", timeout)
}

func waitMeetingRunning(ctx context.Context, s stack, name string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	stable := 0
	last := ""
	for {
		id, err := s.composeOutput(ctx, "ps", "-q", name)
		if err != nil {
			return fmt.Errorf("find optional service %s: %w", name, err)
		}
		if id != "" {
			state, err := s.output(ctx, "inspect", "--format", "{{.State.Status}} {{.RestartCount}}", id)
			if err != nil {
				return fmt.Errorf("inspect optional service %s: %w", name, err)
			}
			current := id + ":" + state
			if strings.HasPrefix(state, "running ") {
				if current == last {
					stable++
				} else {
					stable = 1
				}
			} else {
				stable = 0
			}
			last = current
			if stable >= 3 {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("optional service %s did not remain running: %w", name, ctx.Err())
		case <-time.After(time.Second):
		}
	}
}
