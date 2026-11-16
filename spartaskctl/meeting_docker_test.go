package main

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

// This opt-in test proves reconciliation removes the three retired containers
// while retaining the same running SFU and app containers. No models are loaded.
func TestMeetingDockerRetiresSpeechWithoutRemovingMedia(t *testing.T) {
	image := os.Getenv("SPARTASKCTL_TEST_IMAGE")
	if image == "" {
		t.Skip("set SPARTASKCTL_TEST_IMAGE to a cached digest-pinned image")
	}
	if !meetingImagePattern.MatchString(image) {
		t.Fatal("fixture image must be digest-pinned")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	s, env := meetingFixture(t)
	suffix, err := randomHex(8)
	if err != nil {
		t.Fatal(err)
	}
	project := "spartask-retirement-test-" + suffix
	t.Setenv("COMPOSE_PROJECT_NAME", project)
	t.Setenv("COMPOSE_FILE", "compose.lifecycle.json")
	services := map[string]any{}
	for _, name := range []string{"api", "worker", "scheduler", "caddy", "livekit", "voice", "speech-stt", "speech-tts"} {
		services[name] = map[string]any{"image": image, "command": []string{"sleep", "600"}}
	}
	save := func() {
		t.Helper()
		data, err := json.Marshal(map[string]any{"services": services})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(s.path("compose.lifecycle.json"), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	save()
	t.Cleanup(func() {
		cleanup, done := context.WithTimeout(context.Background(), time.Minute)
		defer done()
		if err := s.compose(cleanup, "down", "--remove-orphans"); err != nil {
			t.Error(err)
		}
	})
	if err := s.compose(ctx, "up", "-d", "--pull", "never"); err != nil {
		t.Fatal(err)
	}
	before, err := s.composeOutput(ctx, "ps", "-q", "livekit")
	if err != nil || before == "" {
		t.Fatal("fixture SFU missing", err)
	}
	for _, name := range []string{"voice", "speech-stt", "speech-tts"} {
		delete(services, name)
	}
	setMeetingMedia(env)
	configureBrowserSpeech(env)
	body, err := planMeeting(s, env).document(env)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct{ Services map[string]map[string]any }
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatal(err)
	}
	for name, overlay := range doc.Services {
		base, ok := services[name].(map[string]any)
		if !ok {
			t.Fatal("unexpected service", name)
		}
		for key, value := range overlay {
			base[key] = value
		}
	}
	save()
	if err := s.compose(ctx, "up", "-d", "--pull", "never", "--remove-orphans"); err != nil {
		t.Fatal(err)
	}
	after, err := s.composeOutput(ctx, "ps", "-q", "livekit")
	if err != nil || before != after {
		t.Fatal("SFU recreated or removed", err)
	}
	names, err := s.output(ctx, "ps", "-a", "--filter", "label=com.docker.compose.project="+project, "--format", "{{.Label \"com.docker.compose.service\"}}")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range strings.Fields(names) {
		if name == "voice" || name == "speech-stt" || name == "speech-tts" {
			t.Fatal("retired container remains", name)
		}
	}
}
