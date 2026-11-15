package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func meetingFixture(t *testing.T) (stack, *envFile) {
	t.Helper()
	s := installedStack(t, olderEnv)
	cfg, err := checkInstalledConfig(s)
	if err != nil {
		t.Fatal(err)
	}
	return s, cfg.env
}

func setMeetingMedia(env *envFile) {
	env.Set("MEETING_MEDIA_ENABLED", "true")
	env.Set("LIVEKIT_MANAGED", "false") // This helper explicitly configures an external SFU.
	env.Set("LIVEKIT_URL", "wss://meeting.example.com")
	env.Set("LIVEKIT_INTERNAL_URL", "https://meeting.example.com")
	env.Set("LIVEKIT_API_KEY", "test-media-key")
	env.Set("LIVEKIT_API_SECRET", strings.Repeat("secret", 8))
}

func setMeetingSpeech(env *envFile) {
	env.Set("SPEECH_ENABLED", "true")
	env.Set("SPEECH_PROVIDER_REVISION", "test-pinned-revision")
	for _, kind := range []string{"STT", "TTS"} {
		env.Set("SPEECH_"+kind+"_URL", "https://speech.example.com/v1")
		env.Set("SPEECH_"+kind+"_MODEL", "test-model")
		env.Set("SPEECH_"+kind+"_API_KEY", "test-private-key")
	}
}

func testMeetingImage() string { return "example.com/test/service@sha256:" + strings.Repeat("a", 64) }

func manageMeetingSpeech(t *testing.T, s stack, env *envFile) {
	t.Helper()
	if err := os.MkdirAll(s.path(".secrets"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"STT", "TTS"} {
		file := filepath.ToSlash(filepath.Join(".secrets", strings.ToLower(kind)+".env"))
		if err := os.WriteFile(s.path(file), []byte("PRIVATE_MODEL_SETTING=test\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		env.Set("SPEECH_"+kind+"_MANAGED", "true")
		env.Set("SPEECH_"+kind+"_IMAGE", testMeetingImage())
		env.Set("SPEECH_"+kind+"_ENV_FILE", file)
		env.Set("SPEECH_"+kind+"_URL", "")
	}
}

func TestMeetingOptionalStateMatrix(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		configure            func(*testing.T, stack, *envFile)
		media, speech, voice bool
		services             int
	}{
		{name: "disabled defaults"},
		{name: "credentials alone never enable", configure: func(_ *testing.T, _ stack, e *envFile) {
			setMeetingMedia(e)
			setMeetingSpeech(e)
			e.Set("MEETING_MEDIA_ENABLED", "false")
			e.Set("SPEECH_ENABLED", "false")
		}},
		{name: "partial media remains off", configure: func(_ *testing.T, _ stack, e *envFile) { setMeetingMedia(e); e.Set("LIVEKIT_API_SECRET", "") }},
		{name: "video only external", configure: func(_ *testing.T, _ stack, e *envFile) { setMeetingMedia(e) }, media: true},
		{name: "reusable speech external without meeting", configure: func(_ *testing.T, _ stack, e *envFile) { setMeetingSpeech(e) }, speech: true},
		{name: "external providers start only voice", configure: func(_ *testing.T, _ stack, e *envFile) {
			setMeetingMedia(e)
			setMeetingSpeech(e)
			e.Set("MEETING_VOICE_ENABLED", "true")
			e.Set("SPARTASK_VOICE_IMAGE", testMeetingImage())
		}, media: true, speech: true, voice: true, services: 1},
		{name: "voice missing image leaves other features", configure: func(_ *testing.T, _ stack, e *envFile) {
			setMeetingMedia(e)
			setMeetingSpeech(e)
			e.Set("MEETING_VOICE_ENABLED", "true")
		}, media: true, speech: true},
		{name: "voice dependency missing", configure: func(_ *testing.T, _ stack, e *envFile) {
			setMeetingMedia(e)
			e.Set("MEETING_VOICE_ENABLED", "true")
			e.Set("SPARTASK_VOICE_IMAGE", testMeetingImage())
		}, media: true},
		{name: "managed models", configure: func(t *testing.T, s stack, e *envFile) { setMeetingSpeech(e); manageMeetingSpeech(t, s, e) }, speech: true, services: 2},
		{name: "missing model credential starts neither model", configure: func(t *testing.T, s stack, e *envFile) {
			setMeetingSpeech(e)
			manageMeetingSpeech(t, s, e)
			e.Set("SPEECH_TTS_API_KEY", "")
		}},
		{name: "case insensitive flags", configure: func(_ *testing.T, _ stack, e *envFile) { setMeetingMedia(e); e.Set("MEETING_MEDIA_ENABLED", "TRUE") }, media: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, env := meetingFixture(t)
			if tc.configure != nil {
				tc.configure(t, s, env)
			}
			p := planMeeting(s, env)
			if len(p.invalid) > 0 {
				t.Fatalf("invalid plan: %v", p.invalid)
			}
			if p.media != tc.media || p.speech != tc.speech || p.voice != tc.voice || len(p.services) != tc.services {
				t.Fatalf("state: media=%v speech=%v voice=%v services=%v", p.media, p.speech, p.voice, p.services)
			}
			body, err := p.document(env)
			if err != nil {
				t.Fatal(err)
			}
			var doc struct {
				Services map[string]struct {
					Environment map[string]string `json:"environment"`
				} `json:"services"`
			}
			if err := json.Unmarshal(body, &doc); err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"api", "worker", "scheduler"} {
				for _, key := range []string{"MEETING_MEDIA_ENABLED", "SPEECH_ENABLED", "MEETING_VOICE_ENABLED"} {
					if doc.Services[name].Environment[key] != p.env[key] {
						t.Fatalf("%s has inconsistent %s", name, key)
					}
				}
			}
		})
	}
}

func TestMeetingApplySelectionAndDisable(t *testing.T) {
	s, env := meetingFixture(t)
	setMeetingMedia(env)
	setMeetingSpeech(env)
	manageMeetingSpeech(t, s, env)
	env.Set("MEETING_VOICE_ENABLED", "true")
	env.Set("SPARTASK_VOICE_IMAGE", testMeetingImage())
	if err := env.Save(s.path(".env")); err != nil {
		t.Fatal(err)
	}
	if _, err := prepareConfig(s); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(s.path(meetingComposeFile))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(before, []byte(`"voice":`)) || !bytes.Contains(before, []byte(`"speech-stt":`)) {
		t.Fatal("enabled services missing")
	}
	// The selected settings file is persisted for ordinary docker compose, not just the CLI.
	saved, err := loadInstalledEnv(s)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(saved.Get("COMPOSE_FILE"), meetingComposeFile) != 1 {
		t.Fatal("generated compose selection missing or duplicated")
	}
	if _, err := prepareConfig(s); err != nil {
		t.Fatal(err)
	}
	again, err := os.ReadFile(s.path(meetingComposeFile))
	if err != nil || !bytes.Equal(before, again) {
		t.Fatal("repeat preparation must be idempotent")
	}
	// An accidentally removed credential must turn off speech AND voice, even though requested
	// flags remain true. The next up --remove-orphans sees neither service in desired state.
	saved.Set("SPEECH_TTS_API_KEY", "")
	if err := saved.Save(s.path(".env")); err != nil {
		t.Fatal(err)
	}
	if _, err := prepareConfig(s); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(s.path(meetingComposeFile))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{`"voice":`, `"speech-stt":`, `"speech-tts":`} {
		if bytes.Contains(after, []byte(name)) {
			t.Fatalf("disabled service retained: %s", name)
		}
	}
	if !bytes.Contains(after, []byte(`"SPEECH_ENABLED": "false"`)) || !bytes.Contains(after, []byte(`"MEETING_MEDIA_ENABLED": "true"`)) {
		t.Fatal("effective features incorrect")
	}
}

func TestMeetingConfigCheckDoesNotWrite(t *testing.T) {
	s := installedStack(t, olderEnv)
	if _, err := checkInstalledConfig(s); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{meetingComposeFile, ".env.bak", ".secrets"} {
		if _, err := os.Stat(s.path(name)); !os.IsNotExist(err) {
			t.Fatalf("read-only check wrote %s", name)
		}
	}
	data, err := os.ReadFile(s.path(".env"))
	if err != nil || string(data) != olderEnv {
		t.Fatal("read-only check changed .env")
	}
}

func TestMeetingManagedLiveKitAndCredentialPreservation(t *testing.T) {
	s, env := meetingFixture(t)
	setMeetingMedia(env)
	env.Set("LIVEKIT_MANAGED", "true")
	env.Set("LIVEKIT_IMAGE", testMeetingImage())
	env.Set("LIVEKIT_PUBLIC_IP", "203.0.113.10")
	env.Set("LIVEKIT_INTERNAL_URL", "")
	env.Set("LIVEKIT_API_KEY", "")
	env.Set("LIVEKIT_API_SECRET", "")
	if err := env.Save(s.path(".env")); err != nil {
		t.Fatal(err)
	}
	ready, err := prepareConfig(s)
	if err != nil {
		t.Fatal(err)
	}
	key, secret := ready.Get("LIVEKIT_API_KEY"), ready.Get("LIVEKIT_API_SECRET")
	if len(key) != 32 || len(secret) != 64 {
		t.Fatal("managed credentials were not generated")
	}
	p := planMeeting(s, ready)
	if len(p.invalid) > 0 || p.env["LIVEKIT_INTERNAL_URL"] != "https://meeting.example.com" {
		t.Fatalf("invalid managed media: %v", p.invalid)
	}
	lk := p.services["livekit"].(map[string]any)
	var config struct {
		Room struct {
			AutoCreate bool `json:"auto_create"`
		} `json:"room"`
		Keys map[string]string `json:"keys"`
	}
	if err := json.Unmarshal([]byte(lk["environment"].(map[string]string)["LIVEKIT_CONFIG"]), &config); err != nil {
		t.Fatal(err)
	}
	if config.Room.AutoCreate || config.Keys[key] != secret {
		t.Fatal("room auto creation or credentials incorrect")
	}
	var out bytes.Buffer
	cfg, err := checkInstalledConfig(s)
	if err != nil {
		t.Fatal(err)
	}
	printReport(&out, cfg.results(), true, true)
	if strings.Contains(out.String(), secret) || strings.Contains(out.String(), key) {
		t.Fatal("secret leaked in report")
	}
	second, err := prepareConfig(s)
	if err != nil || second.Get("LIVEKIT_API_SECRET") != secret || second.Get("LIVEKIT_API_KEY") != key {
		t.Fatal("credentials rotated on repeat")
	}
	// Caddy's mount disappears and its config hash changes when media is disabled.
	enabled, err := os.ReadFile(s.path(meetingComposeFile))
	if err != nil {
		t.Fatal(err)
	}
	second.Set("MEETING_MEDIA_ENABLED", "false")
	if err := second.Save(s.path(".env")); err != nil {
		t.Fatal(err)
	}
	if _, err := prepareConfig(s); err != nil {
		t.Fatal(err)
	}
	disabled, err := os.ReadFile(s.path(meetingComposeFile))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(enabled, disabled) || bytes.Contains(disabled, []byte("/etc/caddy/sites/meeting.caddy")) || bytes.Contains(disabled, []byte(`"livekit":`)) {
		t.Fatal("media disable did not remove SFU/site")
	}
}

func TestMeetingRejectsPopulatedUnsafeConfigurationBeforeApply(t *testing.T) {
	for _, tc := range []struct{ key, value string }{
		{"LIVEKIT_URL", "ws://meeting.example.com"},
		{"LIVEKIT_INTERNAL_URL", "https://user:password@meeting.example.com"},
		{"LIVEKIT_API_SECRET", "short"},
		{"LIVEKIT_IMAGE", "livekit/livekit-server:latest"},
		{"LIVEKIT_PUBLIC_IP", "127.0.0.1"},
		{"LIVEKIT_TLS", "internal { import attacker }"},
		{"LIVEKIT_TCP_PORT", "443"},
		{"LIVEKIT_CPUS", "NaN"},
		{"LIVEKIT_MEMORY", "0g"},
		{"MEETING_MEDIA_ALLOW_INSECURE_LOCAL", "true"},
	} {
		t.Run(tc.key, func(t *testing.T) {
			s, env := meetingFixture(t)
			setMeetingMedia(env)
			env.Set("LIVEKIT_MANAGED", "true")
			env.Set("LIVEKIT_IMAGE", testMeetingImage())
			env.Set("LIVEKIT_PUBLIC_IP", "203.0.113.10")
			env.Set(tc.key, tc.value)
			if err := env.Save(s.path(".env")); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(s.path(".env"))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := prepareConfig(s); err == nil {
				t.Fatal("unsafe setting accepted")
			}
			after, err := os.ReadFile(s.path(".env"))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(before, after) {
				t.Fatal("rejected apply modified .env")
			}
			if _, err := os.Stat(s.path(meetingComposeFile)); !os.IsNotExist(err) {
				t.Fatal("rejected apply wrote compose")
			}
		})
	}
}

func TestMeetingComposeEscapesDollarsAndScopesVoiceSecrets(t *testing.T) {
	s, env := meetingFixture(t)
	setMeetingMedia(env)
	setMeetingSpeech(env)
	env.Set("LIVEKIT_API_SECRET", strings.Repeat("secret-$UNRELATED-", 4))
	env.Set("MEETING_VOICE_ENABLED", "true")
	env.Set("SPARTASK_VOICE_IMAGE", testMeetingImage())
	env.Set("DB_PASSWORD", "db:/p@ss$WORD")
	env.Set("SECRET_KEY", "must-not-reach-voice")
	p := planMeeting(s, env)
	body, err := p.document(env)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(body, []byte("$$UNRELATED")) {
		t.Fatal("Compose interpolation would corrupt secret")
	}
	var doc struct {
		Services map[string]struct {
			Environment map[string]string `json:"environment"`
		} `json:"services"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatal(err)
	}
	voice := doc.Services["voice"].Environment
	if voice["SECRET_KEY"] != "" || strings.Contains(string(body), "must-not-reach-voice") {
		t.Fatal("unrelated core secret reached generated overlay")
	}
	if !strings.Contains(voice["DATABASE_URL"], "db%3A%2Fp%40ss$$WORD") {
		t.Fatalf("database credentials not escaped: %s", voice["DATABASE_URL"])
	}
}

func TestMeetingNoServicesNeedsNoDockerOrTimeout(t *testing.T) {
	s, env := meetingFixture(t)
	env.Set("MEETING_SERVICES_START_TIMEOUT", "unused-invalid-value")
	if err := waitMeetingServices(context.Background(), s, env); err != nil {
		t.Fatal(err)
	}
}
