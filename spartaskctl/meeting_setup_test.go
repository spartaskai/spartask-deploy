package main

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"os"
	"strings"
	"testing"
)

func setupPrompter(input string, out io.Writer) *prompter {
	return &prompter{in: bufio.NewReader(strings.NewReader(input)), out: out}
}

func TestConfigGeneratesLocalMeetingKeysWhileFeatureRemainsOff(t *testing.T) {
	s := installedStack(t, olderEnv)
	env, err := prepareConfig(s)
	if err != nil {
		t.Fatal(err)
	}
	if !meetingEnabled(env, "LIVEKIT_MANAGED") || meetingEnabled(env, "MEETING_MEDIA_ENABLED") {
		t.Fatal("new settings must default to local ownership but disabled media")
	}
	key, secret := env.Get("LIVEKIT_API_KEY"), env.Get("LIVEKIT_API_SECRET")
	if len(key) != 32 || len(secret) != 64 {
		t.Fatal("config did not generate local credentials")
	}
	plan := planMeeting(s, env)
	if len(plan.services) != 0 || plan.media {
		t.Fatal("generating keys must not enable/start services")
	}
	again, err := prepareConfig(s)
	if err != nil {
		t.Fatal(err)
	}
	if again.Get("LIVEKIT_API_KEY") != key || again.Get("LIVEKIT_API_SECRET") != secret {
		t.Fatal("ordinary config rotated an existing credential")
	}
}

func TestMeetingWizardLocalDraftAndIdempotence(t *testing.T) {
	s := installedStack(t, olderEnv+"LIVEKIT_MANAGED=false\n")
	env, err := loadInstalledEnv(s)
	if err != nil {
		t.Fatal(err)
	}
	input := "local\nmeeting.example.com\n203.0.113.20\n" + testMeetingImage() + "\n"
	var out bytes.Buffer
	if err := configureMeeting(setupPrompter(input, &out), env); err != nil {
		t.Fatal(err)
	}
	// Finishing prompts does not touch any installation files yet.
	before, err := os.ReadFile(s.path(".env"))
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != olderEnv+"LIVEKIT_MANAGED=false\n" {
		t.Fatal("wizard wrote before validation/save")
	}
	cfg, err := checkConfigEnv(s, env)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.blocking() {
		t.Fatalf("wizard produced invalid configuration: %v", cfg.envResult.Invalid)
	}
	cfg.envResult.Derived = true
	if err := cfg.save(s); err != nil {
		t.Fatal(err)
	}
	ready, err := loadInstalledEnv(s)
	if err != nil {
		t.Fatal(err)
	}
	if ready.Get("LIVEKIT_URL") != "wss://meeting.example.com" || ready.Get("LIVEKIT_PUBLIC_IP") != "203.0.113.20" {
		t.Fatal("answers were not persisted")
	}
	if !planMeeting(s, ready).media {
		t.Fatal("complete local setup was not enabled for next restart")
	}
	key, secret := ready.Get("LIVEKIT_API_KEY"), ready.Get("LIVEKIT_API_SECRET")
	if strings.Contains(out.String(), key) || strings.Contains(out.String(), secret) {
		t.Fatal("wizard output leaked keys")
	}
	// Same-mode Enter accepts nonsecret defaults and preserves local secrets.
	if err := configureMeeting(setupPrompter("\n\n\n\n", io.Discard), ready); err != nil {
		t.Fatal(err)
	}
	if ready.Get("LIVEKIT_API_KEY") != key || ready.Get("LIVEKIT_API_SECRET") != secret {
		t.Fatal("same-mode wizard rotated keys")
	}
}

func TestMeetingWizardExternalDoesNotInventCredentials(t *testing.T) {
	s, env := meetingFixture(t)
	oldKey, oldSecret := env.Get("LIVEKIT_API_KEY"), env.Get("LIVEKIT_API_SECRET")
	input := "external\nwss://remote.example.com\n\n\n\n"
	if err := configureMeeting(setupPrompter(input, io.Discard), env); err != nil {
		t.Fatal(err)
	}
	cfg, err := checkConfigEnv(s, env)
	if err != nil {
		t.Fatal(err)
	}
	if meetingEnabled(cfg.env, "LIVEKIT_MANAGED") || cfg.env.Get("LIVEKIT_API_KEY") != "" || cfg.env.Get("LIVEKIT_API_SECRET") != "" {
		t.Fatal("external setup retained/generated local credentials")
	}
	if cfg.meeting.media {
		t.Fatal("external setup without actual keys must remain ineffective")
	}
	if oldKey == "" || oldSecret == "" {
		t.Fatal("test must begin with generated local credentials")
	}
	// An existing external connection keeps its actual keys when Enter is used.
	setMeetingMedia(cfg.env)
	if err := configureMeeting(setupPrompter("\n\n\n\n\n", io.Discard), cfg.env); err != nil {
		t.Fatal(err)
	}
	if cfg.env.Get("LIVEKIT_API_KEY") != "test-media-key" || cfg.env.Get("LIVEKIT_API_SECRET") != strings.Repeat("secret", 8) {
		t.Fatal("external credentials were replaced")
	}
}

func TestMeetingWizardCancellationPreservesInstallation(t *testing.T) {
	s := installedStack(t, olderEnv)
	env, err := loadInstalledEnv(s)
	if err != nil {
		t.Fatal(err)
	}
	if err := configureMeeting(setupPrompter("local\nmeeting.example.com\n", io.Discard), env); err == nil {
		t.Fatal("EOF should cancel incomplete setup")
	}
	data, err := os.ReadFile(s.path(".env"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != olderEnv {
		t.Fatal("cancelled wizard changed the installation")
	}
	if _, err := os.Stat(s.path(meetingComposeFile)); !os.IsNotExist(err) {
		t.Fatal("cancelled wizard wrote a deployment")
	}
}

func TestMeetingWizardOffDisablesBrowserSpeech(t *testing.T) {
	_, env := meetingFixture(t)
	env.Set("MEETING_BROWSER_SPEECH_ENABLED", "true")
	if err := configureMeeting(setupPrompter("off\n", io.Discard), env); err != nil {
		t.Fatal(err)
	}
	if meetingEnabled(env, "MEETING_MEDIA_ENABLED") || meetingEnabled(env, "MEETING_BROWSER_SPEECH_ENABLED") {
		t.Fatal("meeting shutdown must also disable browser speech")
	}
}

func TestMeetingWizardRejectsInvalidInputWithoutPrintingSecretDefaults(t *testing.T) {
	_, env := meetingFixture(t)
	env.Set("LIVEKIT_URL", "wss://user:must-not-print@bad.example.com")
	var out bytes.Buffer
	input := "wrong-mode\nlocal\nws://public.example.com\nvalid.example.com\n127.0.0.1\n203.0.113.5\nrepo:latest\n\n"
	if err := configureMeeting(setupPrompter(input, &out), env); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "must-not-print") {
		t.Fatal("invalid credential-bearing URL leaked as a prompt default")
	}
	if env.Get("LIVEKIT_URL") != "wss://valid.example.com" || env.Get("LIVEKIT_IMAGE") != "" {
		t.Fatal("invalid answers were accepted")
	}
}

func TestMeetingGuideDoesNotPrintSecretsOrAdvancedValues(t *testing.T) {
	s, env := meetingFixture(t)
	cfg, err := checkConfigEnv(s, env)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	printMeetingGuide(&out, cfg, false)
	for _, key := range []string{"LIVEKIT_API_KEY", "LIVEKIT_API_SECRET", "DB_PASSWORD", "SECRET_KEY"} {
		if value := cfg.env.Get(key); value != "" && strings.Contains(out.String(), value) {
			t.Fatalf("guide leaked %s", key)
		}
	}
	for _, key := range []string{"LIVEKIT_URL", "LIVEKIT_PUBLIC_IP", "LIVEKIT_IMAGE"} {
		if !strings.Contains(out.String(), key) {
			t.Fatalf("guide omitted essential field %s", key)
		}
	}
}

func TestLegacyEndpointWithoutOwnershipRemainsExternal(t *testing.T) {
	s := installedStack(t, olderEnv+"MEETING_MEDIA_ENABLED=true\nLIVEKIT_URL=wss://remote.example.com\nLIVEKIT_INTERNAL_URL=https://remote.example.com\n")
	env, err := prepareConfig(s)
	if err != nil {
		t.Fatal(err)
	}
	if meetingEnabled(env, "LIVEKIT_MANAGED") || env.Get("LIVEKIT_API_KEY") != "" || env.Get("LIVEKIT_API_SECRET") != "" {
		t.Fatal("legacy external endpoint was claimed or given invented credentials")
	}
	if env.Get("LIVEKIT_URL") != "wss://remote.example.com" {
		t.Fatal("legacy endpoint changed")
	}
}

func TestMeetingConfigCommandCheckNeverPromptsOrWrites(t *testing.T) {
	s := installedStack(t, olderEnv)
	// Empty stdin would fail immediately if -check accidentally entered the wizard.
	if err := configWith(context.Background(), s, []string{"-meeting", "-check"}, setupPrompter("", io.Discard), advisorProbes{}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(s.path(".env"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != olderEnv {
		t.Fatal("meeting -check changed .env")
	}
	for _, name := range []string{".env.bak", meetingComposeFile, ".secrets"} {
		if _, err := os.Stat(s.path(name)); !os.IsNotExist(err) {
			t.Fatalf("meeting -check wrote %s", name)
		}
	}
}

func TestMeetingConfigCommandPersistsAnswersAndBacksUpExistingSettings(t *testing.T) {
	s := installedStack(t, olderEnv+"LIVEKIT_MANAGED=false\n")
	input := "local\nmeeting.example.com\n203.0.113.11\n" + testMeetingImage() + "\n"
	if err := configWith(context.Background(), s, []string{"-meeting"}, setupPrompter(input, io.Discard), advisorProbes{}); err != nil {
		t.Fatal(err)
	}
	env, err := loadInstalledEnv(s)
	if err != nil {
		t.Fatal(err)
	}
	if !meetingEnabled(env, "LIVEKIT_MANAGED") || env.Get("LIVEKIT_URL") != "wss://meeting.example.com" || len(env.Get("LIVEKIT_API_SECRET")) != 64 {
		t.Fatal("guided command did not persist complete local configuration")
	}
	backup, err := os.ReadFile(s.path(".env.bak"))
	if err != nil {
		t.Fatal(err)
	}
	if string(backup) != olderEnv+"LIVEKIT_MANAGED=false\n" {
		t.Fatal("wizard did not preserve the original .env backup")
	}
}
