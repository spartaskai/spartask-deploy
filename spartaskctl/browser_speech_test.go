package main

import (
	"bytes"
	"testing"
)

func TestBrowserSpeechRetiresModelsWithoutChangingMedia(t *testing.T) {
	s, env := meetingFixture(t)
	setMeetingMedia(env)
	setMeetingSpeech(env)
	env.Set("SPEECH_LOCAL_PROFILE", localSpeechProfile)
	env.Set("MEETING_VOICE_ENABLED", "true")
	url, key, secret := env.Get("LIVEKIT_URL"), env.Get("LIVEKIT_API_KEY"), env.Get("LIVEKIT_API_SECRET")
	configureBrowserSpeech(env)
	before := env.Bytes()
	configureBrowserSpeech(env)
	if !bytes.Equal(before, env.Bytes()) {
		t.Fatal("transition is not idempotent")
	}
	p := planMeeting(s, env)
	if len(p.invalid) > 0 || !p.media || p.speech || p.voice {
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
	if p.env["MEETING_BROWSER_SPEECH_ENABLED"] != "true" || p.env["SPEECH_ENABLED"] != "false" || p.env["MEETING_VOICE_ENABLED"] != "false" {
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
