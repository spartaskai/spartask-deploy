package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
)

func TestLocalSpeechProfileGeneratesMatchingCredentialsAndPreservesThem(t *testing.T) {
	s := installedStack(t, olderEnv)
	release := speechRelease{Profile: localSpeechProfile, STT: testMeetingImage(), TTS: testMeetingImage(), Voice: testMeetingImage()}
	data, err := json.Marshal(release)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.path("speech-profile.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	env, err := loadInstalledEnv(s)
	if err != nil {
		t.Fatal(err)
	}
	if err := configureLocalSpeech(s, env); err != nil {
		t.Fatal(err)
	}
	key := env.Get("SPEECH_STT_API_KEY")
	if len(key) != 64 || key == env.Get("SPEECH_TTS_API_KEY") {
		t.Fatal("independent credentials required")
	}
	if err := configureLocalSpeech(s, env); err != nil {
		t.Fatal(err)
	}
	if key != env.Get("SPEECH_STT_API_KEY") {
		t.Fatal("profile rotated key")
	}
	cfg, err := checkConfigEnv(s, env)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.meeting.speech || len(cfg.meeting.invalid) != 0 {
		t.Fatalf("invalid profile: %v", cfg.meeting.invalid)
	}
	service := cfg.meeting.services["speech-stt"].(map[string]any)
	vars := service["environment"].(map[string]string)
	if vars["SPEECH_SERVICE_API_KEY"] != key || vars["SPARTASK_HEALTH_KEY"] != key {
		t.Fatal("provider credential mismatch")
	}
	if _, exists := service["env_file"]; exists {
		t.Fatal("builtin profile needs no manual provider env file")
	}
	if _, exists := service["ports"]; exists {
		t.Fatal("private speech service exposed")
	}
	if vars["SPEECH_CPU_THREADS"] != "2" {
		t.Fatal("STT must use two CPU threads")
	}
	tts := cfg.meeting.services["speech-tts"].(map[string]any)
	if service["tmpfs"].([]string)[0] != "/tmp:size=32m,noexec,nosuid" || tts["tmpfs"].([]string)[0] != "/tmp:size=32m,exec,nosuid" {
		t.Fatal("eSpeak requires executable temporary shared-library mappings only in TTS")
	}
	if env.Get("SPEECH_TTS_MEMORY") != "2g" {
		t.Fatal("TTS requires the measured pilot memory limit")
	}
	if tts["environment"].(map[string]string)["SPEECH_CPU_THREADS"] != "1" {
		t.Fatal("TTS must use one CPU thread")
	}
	for _, container := range []map[string]any{service, tts} {
		if container["read_only"] != true || container["user"] != "65532:65532" {
			t.Fatal("profile must use unprivileged read-only containers")
		}
		if _, exists := container["volumes"]; exists {
			t.Fatal("bundled models must not require writable volumes")
		}
	}
}

func writeTestSpeechRelease(t *testing.T, s stack, release speechRelease) {
	t.Helper()
	data, err := json.Marshal(release)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.path("speech-profile.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestLocalSpeechRejectsInvalidReleasedProfilesWithoutMutatingDraft(t *testing.T) {
	for _, tc := range []struct {
		name    string
		release speechRelease
	}{
		{"wrong-profile", speechRelease{Profile: "unknown", STT: testMeetingImage(), TTS: testMeetingImage(), Voice: testMeetingImage()}},
		{"mutable-stt", speechRelease{Profile: localSpeechProfile, STT: "example.com/test/stt:latest", TTS: testMeetingImage(), Voice: testMeetingImage()}},
		{"short-tts-digest", speechRelease{Profile: localSpeechProfile, STT: testMeetingImage(), TTS: "example.com/test/tts@sha256:abcd", Voice: testMeetingImage()}},
		{"missing-voice", speechRelease{Profile: localSpeechProfile, STT: testMeetingImage(), TTS: testMeetingImage()}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, env := meetingFixture(t)
			writeTestSpeechRelease(t, s, tc.release)
			before := env.Bytes()
			if err := configureLocalSpeech(s, env); err == nil {
				t.Fatal("invalid release accepted")
			}
			if !bytes.Equal(before, env.Bytes()) {
				t.Fatal("invalid release changed draft")
			}
		})
	}
}

func TestLocalSpeechProfileMarkerCannotBypassMatchingContract(t *testing.T) {
	for _, key := range []string{"SPEECH_LOCAL_PROFILE", "SPEECH_STT_IMAGE", "SPEECH_TTS_IMAGE", "SPARTASK_VOICE_IMAGE", "SPEECH_STT_MODEL", "SPEECH_TTS_MODEL", "SPEECH_TTS_VOICE", "SPEECH_LANGUAGE", "SPEECH_PROVIDER_REVISION", "SPEECH_STT_MANAGED", "SPEECH_TTS_ENV_FILE"} {
		t.Run(key, func(t *testing.T) {
			s, env := meetingFixture(t)
			writeTestSpeechRelease(t, s, speechRelease{Profile: localSpeechProfile, STT: testMeetingImage(), TTS: testMeetingImage(), Voice: testMeetingImage()})
			if err := configureLocalSpeech(s, env); err != nil {
				t.Fatal(err)
			}
			env.Set(key, "mismatch")
			cfg, err := checkConfigEnv(s, env)
			if err != nil {
				t.Fatal(err)
			}
			if !cfg.blocking() || !strings.Contains(strings.Join(cfg.meeting.invalid, "\n"), key) {
				t.Fatalf("mismatched %s was accepted: %v", key, cfg.meeting.invalid)
			}
		})
	}
}

func TestLocalSpeechCheckDoesNotSelectProfileGenerateCredentialsOrWrite(t *testing.T) {
	s := installedStack(t, olderEnv)
	if err := configWith(context.Background(), s, []string{"-speech-local", "-check"}, setupPrompter("", io.Discard), advisorProbes{}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(s.path(".env"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != olderEnv {
		t.Fatal("check changed installed env")
	}
	for _, name := range []string{meetingComposeFile, ".env.bak", ".secrets"} {
		if _, err := os.Stat(s.path(name)); !os.IsNotExist(err) {
			t.Fatalf("check wrote %s", name)
		}
	}
}

func TestLocalSpeechRequiresExplicitOptInForExternalProviders(t *testing.T) {
	s, env := meetingFixture(t)
	setMeetingSpeech(env)
	beforeSTT, beforeTTS := env.Get("SPEECH_STT_API_KEY"), env.Get("SPEECH_TTS_API_KEY")
	cfg, err := checkConfigEnv(s, env)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.blocking() {
		t.Fatalf("external provider rejected: %v", cfg.meeting.invalid)
	}
	if env.Get("SPEECH_LOCAL_PROFILE") != "" || env.Get("SPEECH_STT_URL") != "https://speech.example.com/v1" || env.Get("SPEECH_STT_API_KEY") != beforeSTT {
		t.Fatal("ordinary config took ownership of external provider")
	}
	writeTestSpeechRelease(t, s, speechRelease{Profile: localSpeechProfile, STT: testMeetingImage(), TTS: testMeetingImage(), Voice: testMeetingImage()})
	if err := configureLocalSpeech(s, env); err != nil {
		t.Fatal(err)
	}
	if env.Get("SPEECH_STT_API_KEY") == beforeSTT || env.Get("SPEECH_TTS_API_KEY") == beforeTTS {
		t.Fatal("explicit ownership change reused remote credentials")
	}
	if env.Get("SPEECH_STT_URL") != "" || env.Get("SPEECH_TTS_URL") != "" {
		t.Fatal("local profile retained external endpoint")
	}
	stt, tts := env.Get("SPEECH_STT_API_KEY"), env.Get("SPEECH_TTS_API_KEY")
	if err := configureLocalSpeech(s, env); err != nil {
		t.Fatal(err)
	}
	if env.Get("SPEECH_STT_API_KEY") != stt || env.Get("SPEECH_TTS_API_KEY") != tts {
		t.Fatal("repeat opt-in rotated local credentials")
	}
}

func TestUnknownLocalSpeechProfileBlocksEvenWhenSpeechDisabled(t *testing.T) {
	s, env := meetingFixture(t)
	env.Set("SPEECH_LOCAL_PROFILE", "english-cpu-typo")
	before := env.Bytes()
	if err := configureLocalSpeech(s, env); err == nil {
		t.Fatal("unknown marker overwritten")
	}
	if !bytes.Equal(before, env.Bytes()) {
		t.Fatal("unknown marker changed draft")
	}
	cfg, err := checkConfigEnv(s, env)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.blocking() {
		t.Fatal("unknown profile silently ignored")
	}
}

func TestLocalSpeechRequiresPublishedProfile(t *testing.T) {
	s := installedStack(t, olderEnv)
	env, err := loadInstalledEnv(s)
	if err != nil {
		t.Fatal(err)
	}
	if err := configureLocalSpeech(s, env); err == nil {
		t.Fatal("missing release profile accepted")
	}
}
