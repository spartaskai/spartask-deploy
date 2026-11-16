package main

import (
	"fmt"
	"io"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
)

// configureMeeting edits only the caller's in-memory draft. It deliberately configures the SFU
// connection, not arbitrary model images' authentication contracts. Config never starts services.
func configureMeeting(p *prompter, env *envFile) error {
	fmt.Fprintln(p.out, "Toplantı bağlantısı kurulumu — kamera zorunlu değildir; config servis başlatmaz.")
	fmt.Fprintln(p.out, "Bu adım görüntülü görüşme için LiveKit bağlantısını hazırlar.")
	fmt.Fprintln(p.out, "local/external seçimi medyayı etkinleştirir; değişiklik ancak restart ile uygulanır.")
	modeDefault := "local"
	if !meetingEnabled(env, "LIVEKIT_MANAGED") && env.Get("LIVEKIT_URL") != "" {
		modeDefault = "external"
	}
	mode, err := askMeetingMode(p, modeDefault)
	if err != nil {
		return err
	}
	if mode == "off" {
		env.Set("MEETING_MEDIA_ENABLED", "false")
		env.Set("MEETING_BROWSER_SPEECH_ENABLED", "false")
		fmt.Fprintln(p.out, "Meeting ve tarayıcı konuşması kapalı seçildi.")
		return nil
	}
	local := mode == "local"
	changedMode := local != meetingEnabled(env, "LIVEKIT_MANAGED")
	env.Set("MEETING_MEDIA_ENABLED", "true")
	env.Set("LIVEKIT_MANAGED", strconv.FormatBool(local))
	if changedMode {
		// Credentials belong to a server, not just a feature. Never reuse a local-generated key
		// for an external server, or silently copy a remote server's credential into a new SFU.
		for _, key := range []string{"LIVEKIT_API_KEY", "LIVEKIT_API_SECRET", "LIVEKIT_INTERNAL_URL"} {
			env.Set(key, "")
		}
		fmt.Fprintln(p.out, "Yerel/harici sunucu seçimi değişti: önceki sunucunun anahtarları yeni bağlantıda kullanılmayacak.")
	}
	def := meetingURLDefault(env, local)
	endpoint, err := askMeetingURL(p, env, def, local)
	if err != nil {
		return err
	}
	env.Set("LIVEKIT_URL", endpoint)
	if local {
		return configureLocalMeeting(p, env)
	}
	return configureExternalMeeting(p, env)
}

func askMeetingMode(p *prompter, def string) (string, error) {
	for {
		mode, err := p.ask("LiveKit nerede? local=bu sunucu, external=başka servis, off=meeting kapalı", def, true)
		if err != nil {
			return "", err
		}
		mode = strings.ToLower(mode)
		if mode == "local" || mode == "external" || mode == "off" {
			return mode, nil
		}
		fmt.Fprintln(p.out, "local, external veya off yazın.")
	}
}

func meetingURLDefault(env *envFile, local bool) string {
	raw := env.Get("LIVEKIT_URL")
	if validSetupMeetingURL(raw, env, local) {
		return raw
	}
	if local && domainPattern.MatchString(env.Get("SPARTASK_DOMAIN")) {
		return "wss://meetings." + env.Get("SPARTASK_DOMAIN")
	}
	return ""
}

func askMeetingURL(p *prompter, env *envFile, def string, local bool) (string, error) {
	for {
		raw, err := p.ask("Toplantı adresi (alan adı veya wss://adres)", def, true)
		if err != nil {
			return "", err
		}
		if !strings.Contains(raw, "://") {
			raw = "wss://" + raw
		}
		raw = strings.TrimRight(raw, "/")
		if validSetupMeetingURL(raw, env, local) {
			return raw, nil
		}
		fmt.Fprintln(p.out, "Geçerli bir WSS adresi girin; kullanıcı bilgisi, path veya query kullanmayın. Yerel kurulum ayrı bir DNS alan adı gerektirir.")
	}
}

func validSetupMeetingURL(raw string, env *envFile, local bool) bool {
	p := &meetingPlan{}
	u := p.endpoint(raw, "LIVEKIT_URL", "wss")
	if u == nil {
		return false
	}
	return !local || domainPattern.MatchString(u.Hostname()) && u.Port() == "" && !strings.EqualFold(u.Hostname(), env.Get("SPARTASK_DOMAIN"))
}

func configureLocalMeeting(p *prompter, env *envFile) error {
	fmt.Fprintln(p.out, "Bu adresi DNS'te ayırın. TLS sertifikası meeting adresini de kapsamalı; özel TLS gerekirse LIVEKIT_TLS kullanılır.")
	for {
		raw, err := p.ask("Sunucunun istemcilerden erişilebilir IPv4 adresi", env.Get("LIVEKIT_PUBLIC_IP"), true)
		if err != nil {
			return err
		}
		ip, err := netip.ParseAddr(raw)
		if err == nil && ip.Is4() && !ip.IsLoopback() && !ip.IsUnspecified() && !ip.IsMulticast() {
			env.Set("LIVEKIT_PUBLIC_IP", ip.String())
			break
		}
		fmt.Fprintln(p.out, "Erişilebilir IPv4 girin; DNS/Cloudflare adresi veya localhost değil, medyanın ulaştığı sunucu IP'si gerekir.")
	}
	fmt.Fprintln(p.out, "LiveKit imajı sürüm profilinden alınır: repository@sha256:<digest>. Henüz bilmiyorsanız boş bırakın; meeting kapalı kalır.")
	for {
		def := env.Get("LIVEKIT_IMAGE")
		if !meetingImagePattern.MatchString(def) {
			def = ""
		}
		image, err := p.ask("Sabit LiveKit imajı", def, false)
		if err != nil {
			return err
		}
		if image == "" || meetingImagePattern.MatchString(image) {
			env.Set("LIVEKIT_IMAGE", image)
			break
		}
		fmt.Fprintln(p.out, "Değişken latest/tag yerine repository@sha256:<64 küçük hex> girin veya boş bırakın.")
	}
	fmt.Fprintln(p.out, "Key/secret boşsa otomatik üretilecek; dolu yerel değerler korunur. İç adres boş kalabilir; port/CPU/bellek değerleri varsayılan bırakılabilir.")
	return nil
}

func configureExternalMeeting(p *prompter, env *envFile) error {
	u, err := url.Parse(env.Get("LIVEKIT_URL"))
	if err != nil {
		return fmt.Errorf("parse configured meeting URL: %w", err)
	}
	def := env.Get("LIVEKIT_INTERNAL_URL")
	probe := &meetingPlan{}
	if probe.endpoint(def, "LIVEKIT_INTERNAL_URL", "https") == nil {
		def = "https://" + u.Host
	}
	for {
		internal, err := p.ask("Sunucuların erişeceği HTTPS LiveKit adresi", def, true)
		if err != nil {
			return err
		}
		check := &meetingPlan{}
		if check.endpoint(internal, "LIVEKIT_INTERNAL_URL", "https") != nil {
			env.Set("LIVEKIT_INTERNAL_URL", strings.TrimRight(internal, "/"))
			break
		}
		fmt.Fprintln(p.out, "HTTPS adresi girin; kullanıcı bilgisi, path veya query kullanmayın.")
	}
	fmt.Fprintln(p.out, "Harici sunucunun gerçek key/secret değerleri gerekir; burada rastgele üretilmez. Aynı sunucuda mevcut dolu değerler Enter ile korunur.")
	for _, key := range []string{"LIVEKIT_API_KEY", "LIVEKIT_API_SECRET"} {
		value, err := p.askSecret(key)
		if err != nil {
			return err
		}
		if value != "" {
			env.Set(key, value)
		}
	}
	return nil
}

func printMeetingGuide(w io.Writer, cfg *installedConfig, dryRun bool) {
	fmt.Fprintln(w, "\nToplantı kısa rehberi:")
	if !meetingEnabled(cfg.env, "MEETING_MEDIA_ENABLED") {
		fmt.Fprintln(w, "  Medya KAPALI: açmayacaksanız aşağıdaki alanları doldurmanız gerekmez. Kısa kurulum: spartaskctl config -meeting.")
	}
	if meetingEnabled(cfg.env, "LIVEKIT_MANAGED") {
		fmt.Fprintln(w, "  Yerel LiveKit: elle girilecek yalnız 3 alan:")
		for _, field := range []struct{ key, help string }{
			{"LIVEKIT_URL", "wss://toplantı-alan-adı (DNS ve TLS ayrıca hazırlanır)"},
			{"LIVEKIT_PUBLIC_IP", "medyanın ulaşacağı sunucu IPv4 adresi"},
			{"LIVEKIT_IMAGE", "dağıtım profilindeki sabit repository@sha256:…"},
		} {
			state := "dolu"
			if cfg.env.Get(field.key) == "" {
				state = "EKSİK"
			}
			fmt.Fprintf(w, "    %s: %s — %s\n", field.key, state, field.help)
		}
		verb := "hazır (boşsa otomatik üretilir)"
		if dryRun {
			verb = "boşsa config sırasında otomatik üretilecek"
		}
		fmt.Fprintln(w, "  LIVEKIT_API_KEY / LIVEKIT_API_SECRET:", verb, "; değerleri gösterilmez.")
		fmt.Fprintln(w, "  LIVEKIT_INTERNAL_URL boş kalabilir: HTTPS karşılığı otomatik türetilir.")
	} else {
		fmt.Fprintln(w, "  Harici LiveKit seçili: URL, INTERNAL_URL ve o servisin gerçek KEY/SECRET değerlerini girin.")
		fmt.Fprintln(w, "  Aynı sunucuda kurmak için: spartaskctl config -meeting → local. Böylece boş key/secret otomatik üretilir.")
	}
	fmt.Fprintln(w, "  Port, CPU, bellek ve oda sınırları gelişmiş ayarlardır; normal kurulumda varsayılan bırakın.")
	fmt.Fprintln(w, "  Tarayıcı STT/TTS geçişi: spartaskctl config -speech-browser (model servisi veya voice imajı gerekmez).")
	fmt.Fprintln(w, "  STT/TTS anahtarı sağlayıcıdakiyle aynı olmalı; sağlayıcıyı yapılandırmadan rastgele anahtar üretmeyin.")
	fmt.Fprintln(w, "  Rehber: MEETING_DEPLOYMENT.md · Uygulama: spartaskctl restart (config container başlatmaz).")
}
