package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// Opt-in lifecycle test. It uses a locally cached, digest-pinned Debian image as inert process
// fixtures, not real models or an SFU. It proves Compose reconciliation/removal, env selection,
// dependency and mount syntax. Real audio/browser acceptance remains a separate backend gate.
func TestMeetingDockerLifecycle(t *testing.T) {
	image := os.Getenv("SPARTASKCTL_TEST_IMAGE")
	if image == "" {
		t.Skip("set SPARTASKCTL_TEST_IMAGE to a cached digest-pinned Debian image for isolated Docker lifecycle acceptance")
	}
	if !meetingImagePattern.MatchString(image) || !strings.HasPrefix(image, "debian@sha256:") {
		t.Fatal("test image must be cached debian@sha256:<digest>")
	}
	s, env := meetingFixture(t)
	suffix, err := randomHex(8)
	if err != nil {
		t.Fatal(err)
	}
	project := "spartaskctl-meeting-test-" + suffix
	files := "compose.yml:" + meetingComposeFile + ":compose.test.yml"
	env.Set("COMPOSE_PROJECT_NAME", project)
	env.Set("COMPOSE_FILE", files)
	t.Setenv("COMPOSE_PROJECT_NAME", project)
	t.Setenv("COMPOSE_FILE", files)
	t.Setenv("COMPOSE_PATH_SEPARATOR", ":")
	base := map[string]any{"services": map[string]any{}, "networks": map[string]any{"spartask": map[string]any{}}}
	for _, name := range []string{"api", "worker", "scheduler", "caddy", "migrate"} {
		service := map[string]any{"image": image, "command": []string{"sleep", "600"}, "networks": []string{"spartask"}, "healthcheck": map[string]any{"test": []string{"CMD", "true"}, "interval": "1s", "timeout": "1s", "retries": 3}}
		if name == "migrate" {
			service["command"] = []string{"true"}
		}
		base["services"].(map[string]any)[name] = service
	}
	writeJSON := func(name string, value any) {
		t.Helper()
		body, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(s.path(name), body, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	writeJSON("compose.yml", base)
	// The fixture override must omit disabled services too: otherwise it would itself cause them
	// to start and invalidate the very property this test is meant to prove.
	setOverride := func(on bool) {
		if !on {
			writeJSON("compose.test.yml", map[string]any{"services": map[string]any{}})
			return
		}
		var body strings.Builder
		body.WriteString("services:\n")
		for _, name := range []string{"livekit", "voice", "speech-stt", "speech-tts"} {
			fmt.Fprintf(&body, "  %s:\n    image: %q\n    command: [sleep, \"600\"]\n", name, image)
			// Test-only reset prevents any host port binding. Production mappings remain covered
			// by the plan assertions and the base Compose parser. Requires Compose >=2.24.4.
			if name == "livekit" {
				body.WriteString("    ports: !reset []\n")
			}
			if strings.HasPrefix(name, "speech-") {
				body.WriteString("    healthcheck:\n      test: [CMD, \"true\"]\n      interval: 1s\n      timeout: 1s\n      start_period: 1s\n")
			}
		}
		if err := os.WriteFile(s.path("compose.test.yml"), []byte(body.String()), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	setOverride(false)
	if err := env.Save(s.path(".env")); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	var full []byte
	t.Cleanup(func() {
		cleanup, done := context.WithTimeout(context.Background(), 30*time.Second)
		defer done()
		// Restore the complete volume definitions solely for down --volumes. No services are started.
		if full != nil {
			if err := os.WriteFile(s.path(meetingComposeFile), full, 0o600); err != nil {
				t.Error(err)
				return
			}
		}
		if err := s.compose(cleanup, "down", "--volumes", "--remove-orphans"); err != nil {
			t.Error(err)
		}
	})
	if err := runRestart(ctx, s); err != nil {
		t.Fatal(err)
	}
	assertRunning := func(want bool) {
		t.Helper()
		running, err := s.composeOutput(ctx, "ps", "--services", "--status", "running")
		if err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"livekit", "voice", "speech-stt", "speech-tts"} {
			found := false
			for _, got := range strings.Fields(running) {
				if got == name {
					found = true
				}
			}
			if found != want {
				t.Fatalf("%s running=%v, want %v; services=%s", name, found, want, running)
			}
		}
	}
	assertRunning(false)
	setMeetingMedia(env)
	setMeetingSpeech(env)
	manageMeetingSpeech(t, s, env)
	env.Set("LIVEKIT_MANAGED", "true")
	env.Set("LIVEKIT_IMAGE", image)
	env.Set("LIVEKIT_PUBLIC_IP", "203.0.113.10")
	env.Set("LIVEKIT_API_SECRET", strings.Repeat("secret-$MEETING_TEST_LITERAL-", 4))
	env.Set("MEETING_VOICE_ENABLED", "true")
	env.Set("SPARTASK_VOICE_IMAGE", image)
	for _, kind := range []string{"STT", "TTS"} {
		env.Set("SPEECH_"+kind+"_IMAGE", image)
	}
	if err := env.Save(s.path(".env")); err != nil {
		t.Fatal(err)
	}
	setOverride(true)
	if err := runRestart(ctx, s); err != nil {
		t.Fatal(err)
	}
	assertRunning(true)
	resolved, err := s.composeOutput(ctx, "config", "--format", "json")
	if err != nil {
		t.Fatal(err)
	}
	var config struct {
		Services map[string]struct {
			Environment map[string]string `json:"environment"`
			Networks    map[string]struct {
				Aliases []string `json:"aliases"`
			} `json:"networks"`
		} `json:"services"`
	}
	if err := json.Unmarshal([]byte(resolved), &config); err != nil {
		t.Fatal(err)
	}
	// `compose config` re-escapes dollars for round-trip serialization. Verify the actual
	// container environment, which is what API authentication and the SFU really consume.
	for _, service := range []string{"api", "voice", "livekit"} {
		id, err := s.composeOutput(ctx, "ps", "-q", service)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := s.output(ctx, "inspect", "--format", "{{json .Config.Env}}", id)
		if err != nil {
			t.Fatal(err)
		}
		var values []string
		if err := json.Unmarshal([]byte(raw), &values); err != nil {
			t.Fatal(err)
		}
		key := "LIVEKIT_API_SECRET"
		if service == "livekit" {
			key = "LIVEKIT_CONFIG"
		}
		got := ""
		for _, value := range values {
			if strings.HasPrefix(value, key+"=") {
				got = strings.TrimPrefix(value, key+"=")
			}
		}
		if service == "livekit" {
			var lk struct {
				Keys map[string]string `json:"keys"`
			}
			if err := json.Unmarshal([]byte(got), &lk); err != nil {
				t.Fatal(err)
			}
			got = lk.Keys[env.Get("LIVEKIT_API_KEY")]
		}
		if got != env.Get("LIVEKIT_API_SECRET") {
			t.Fatalf("%s changed synthetic literal secret: got %q", service, got)
		}
	}
	aliases := config.Services["caddy"].Networks["spartask"].Aliases
	if len(aliases) != 1 || aliases[0] != "meeting.example.com" {
		t.Fatal("internal TLS domain alias missing")
	}
	if len(config.Services["voice"].Networks) != 1 {
		t.Fatal("voice network configuration lost")
	}
	full, err = os.ReadFile(s.path(meetingComposeFile))
	if err != nil {
		t.Fatal(err)
	}
	// Removing one speech credential must remove both model containers and voice; disabling
	// media removes LiveKit and its Caddy mount. Main app services remain running.
	env.Set("SPEECH_TTS_API_KEY", "")
	env.Set("MEETING_MEDIA_ENABLED", "false")
	if err := env.Save(s.path(".env")); err != nil {
		t.Fatal(err)
	}
	setOverride(false)
	if err := runRestart(ctx, s); err != nil {
		t.Fatal(err)
	}
	assertRunning(false)
	// All optional containers (including stopped ones) must be absent, not merely hidden by ps.
	left, err := s.output(ctx, "ps", "-a", "--filter", "label=com.docker.compose.project="+project, "--format", "{{.Label \"com.docker.compose.service\"}}")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range strings.Fields(left) {
		if name == "voice" || name == "livekit" || strings.HasPrefix(name, "speech-") {
			t.Fatalf("disabled container retained: %s", name)
		}
	}
}
