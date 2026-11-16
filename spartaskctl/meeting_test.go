package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
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
	env.Set("LIVEKIT_MANAGED", "false")
	env.Set("LIVEKIT_URL", "wss://meeting.example.com")
	env.Set("LIVEKIT_INTERNAL_URL", "https://meeting.example.com")
	env.Set("LIVEKIT_API_KEY", "test-media-key")
	env.Set("LIVEKIT_API_SECRET", strings.Repeat("secret", 8))
}
func testMeetingImage() string { return "example.com/test/service@sha256:" + strings.Repeat("a", 64) }
func TestMeetingOptionalStateMatrix(t *testing.T) {
	for _, tc := range []struct {
		name      string
		configure func(*envFile)
		media     bool
	}{
		{name: "disabled defaults"},
		{name: "external media", configure: setMeetingMedia, media: true},
		{name: "partial media", configure: func(e *envFile) { setMeetingMedia(e); e.Set("LIVEKIT_API_SECRET", "") }},
		{name: "stale model configuration", configure: func(e *envFile) {
			e.Set("SPEECH_ENABLED", "true")
			e.Set("MEETING_VOICE_ENABLED", "true")
			e.Set("SPARTASK_VOICE_IMAGE", testMeetingImage())
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, env := meetingFixture(t)
			if tc.configure != nil {
				tc.configure(env)
			}
			p := planMeeting(s, env)
			if len(p.invalid) > 0 || p.media != tc.media {
				t.Fatalf("plan: %+v", p)
			}
			for _, name := range []string{"voice", "speech-stt", "speech-tts"} {
				if _, exists := p.services[name]; exists {
					t.Fatal("retired service generated", name)
				}
			}
			for key := range p.env {
				if retiredServerSpeechKey(key) {
					t.Fatal("retired setting forwarded", key)
				}
			}
		})
	}
}
func TestMeetingApplyIsIdempotent(t *testing.T) {
	s, env := meetingFixture(t)
	setMeetingMedia(env)
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
	saved, err := loadInstalledEnv(s)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(saved.Get("COMPOSE_FILE"), meetingComposeFile) != 1 {
		t.Fatal("overlay duplicated")
	}
	if _, err := prepareConfig(s); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(s.path(meetingComposeFile))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("not idempotent")
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

func TestMeetingComposeEscapesDollars(t *testing.T) {
	s, env := meetingFixture(t)
	setMeetingMedia(env)
	env.Set("LIVEKIT_API_SECRET", strings.Repeat("secret-$UNRELATED-", 4))
	env.Set("SECRET_KEY", "must-not-reach-overlay")
	body, err := planMeeting(s, env).document(env)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(body, []byte("$$UNRELATED")) || bytes.Contains(body, []byte("must-not-reach-overlay")) {
		t.Fatal("secret escaping or isolation failed")
	}
}
func TestMeetingNoServicesNeedsNoDockerOrTimeout(t *testing.T) {
	s, env := meetingFixture(t)
	env.Set("MEETING_SERVICES_START_TIMEOUT", "unused-invalid-value")
	if err := waitMeetingServices(context.Background(), s, env); err != nil {
		t.Fatal(err)
	}
}
