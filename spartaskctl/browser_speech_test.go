package main

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestBrowserSpeechRetiresModelsWithoutChangingMedia(t *testing.T) {
	s, env := meetingFixture(t)
	setMeetingMedia(env)
	env.Set("SPEECH_ENABLED", "true")
	env.Set("SPEECH_LOCAL_PROFILE", "english-cpu-v1")
	env.Set("MEETING_VOICE_ENABLED", "true")
	url, key, secret := env.Get("LIVEKIT_URL"), env.Get("LIVEKIT_API_KEY"), env.Get("LIVEKIT_API_SECRET")
	configureBrowserSpeech(env)
	before := env.Bytes()
	configureBrowserSpeech(env)
	if !bytes.Equal(before, env.Bytes()) {
		t.Fatal("transition is not idempotent")
	}
	p := planMeeting(s, env)
	if len(p.invalid) > 0 || !p.media {
		t.Fatalf("browser plan: %+v", p)
	}
	for _, name := range []string{"voice", "speech-stt", "speech-tts"} {
		if _, ok := p.services[name]; ok {
			t.Fatal("retired service", name)
		}
	}
	if env.Get("LIVEKIT_URL") != url || env.Get("LIVEKIT_API_KEY") != key || env.Get("LIVEKIT_API_SECRET") != secret {
		t.Fatal("media settings changed")
	}
	if p.env["MEETING_BROWSER_SPEECH_ENABLED"] != "true" || env.has("SPEECH_ENABLED") || env.has("MEETING_VOICE_ENABLED") {
		t.Fatalf("app env: %+v", p.env)
	}
}
func TestBrowserSpeechRequiresMediaAndKnownLanguage(t *testing.T) {
	s, env := meetingFixture(t)
	configureBrowserSpeech(env)
	if len(planMeeting(s, env).invalid) == 0 {
		t.Fatal("missing media accepted")
	}
	setMeetingMedia(env)
	env.Set("MEETING_BROWSER_SPEECH_LANGUAGE", "invalid")
	if len(planMeeting(s, env).invalid) == 0 {
		t.Fatal("invalid language accepted")
	}
}

func TestRetiredEnvAssignmentsAreRemovedWithoutTouchingBrowserOrMedia(t *testing.T) {
	env := parseEnv([]byte("# Old provider\nSPEECH_STT_API_KEY=private-one\nexport SPEECH_STT_API_KEY=private-two\nMEETING_VOICE_ADDRESS_WINDOW=30s\nSPARTASK_VOICE_IMAGE=old\nMEETING_BROWSER_SPEECH_ENABLED=true\nMEETING_BROWSER_SPEECH_LANGUAGE=tr\nLIVEKIT_API_SECRET=keep-private\nOTHER_SETTING=keep\n"))
	removed := retireServerSpeech(env)
	if len(removed) != 3 {
		t.Fatalf("removed keys: %v", removed)
	}
	for _, key := range env.keys() {
		if retiredServerSpeechKey(key) {
			t.Fatal("retired assignment remains", key)
		}
	}
	if env.Get("LIVEKIT_API_SECRET") != "keep-private" || env.Get("MEETING_BROWSER_SPEECH_LANGUAGE") != "tr" || env.Get("OTHER_SETTING") != "keep" {
		t.Fatal("unrelated settings changed")
	}
	before := env.Bytes()
	retireServerSpeech(env)
	if !bytes.Equal(before, env.Bytes()) {
		t.Fatal("cleanup is not idempotent")
	}
	if strings.Contains(string(env.Bytes()), "private-one") || strings.Contains(string(env.Bytes()), "private-two") {
		t.Fatal("retired credential value retained")
	}
}
func TestInstalledConfigRemovesRetiredSettingsAndCheckDoesNotWrite(t *testing.T) {
	s := installedStack(t, olderEnv+"\nSPEECH_TTS_API_KEY=private-model-key\nMEETING_VOICE_ENABLED=true\n")
	before, err := os.ReadFile(s.path(".env"))
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := checkInstalledConfig(s)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.env.has("SPEECH_TTS_API_KEY") {
		t.Fatal("draft retained retired key")
	}
	after, err := os.ReadFile(s.path(".env"))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("read-only check wrote settings")
	}
	if _, err := prepareConfig(s); err != nil {
		t.Fatal(err)
	}
	saved, err := readEnvFile(s.path(".env"))
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range saved.keys() {
		if retiredServerSpeechKey(key) {
			t.Fatal("saved retired setting", key)
		}
	}
}
