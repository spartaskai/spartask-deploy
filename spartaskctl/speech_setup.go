package main

import (
	"encoding/json"
	"fmt"
	"os"
)

const localSpeechProfile = "english-cpu-v1"

type speechRelease struct {
	Profile string `json:"profile"`
	STT     string `json:"stt_image"`
	TTS     string `json:"tts_image"`
	Voice   string `json:"voice_image"`
}

// The bundle supplies reviewed immutable images. No network or Docker operation
// happens during config. Reapplying preserves keys; ownership changes are explicit.
func readSpeechRelease(s stack) (speechRelease, error) {
	var release speechRelease
	data, err := os.ReadFile(s.path("speech-profile.json"))
	if err != nil {
		return release, fmt.Errorf("read released speech profile: %w", err)
	}
	if err := json.Unmarshal(data, &release); err != nil {
		return release, fmt.Errorf("decode speech profile: %w", err)
	}
	if release.Profile != localSpeechProfile || !meetingImagePattern.MatchString(release.STT) || !meetingImagePattern.MatchString(release.TTS) || !meetingImagePattern.MatchString(release.Voice) {
		return release, fmt.Errorf("speech profile %s requires three published immutable image references", localSpeechProfile)
	}
	return release, nil
}

func configureLocalSpeech(s stack, env *envFile) error {
	if profile := env.Get("SPEECH_LOCAL_PROFILE"); profile != "" && profile != localSpeechProfile {
		return fmt.Errorf("SPEECH_LOCAL_PROFILE: unsupported profile %q; clear it before selecting %s", profile, localSpeechProfile)
	}
	release, err := readSpeechRelease(s)
	if err != nil {
		return err
	}
	credentials := make(map[string]string)
	for _, kind := range []string{"STT", "TTS"} {
		key := "SPEECH_" + kind + "_API_KEY"
		if env.Get(key) == "" || env.Get("SPEECH_LOCAL_PROFILE") != localSpeechProfile {
			value, err := randomHex(32)
			if err != nil {
				return fmt.Errorf("generate local speech credential: %w", err)
			}
			credentials[key] = value
		}
	}
	for key, value := range credentials {
		env.Set(key, value)
	}
	for _, kind := range []string{"STT", "TTS"} {
		env.Set("SPEECH_"+kind+"_MANAGED", "true")
		env.Set("SPEECH_"+kind+"_ENV_FILE", "")
		env.Set("SPEECH_"+kind+"_URL", "")
	}
	for key, value := range map[string]string{
		"SPEECH_LOCAL_PROFILE": localSpeechProfile, "SPEECH_PROVIDER_REVISION": localSpeechProfile,
		"SPEECH_ENABLED": "true", "MEETING_VOICE_ENABLED": "true", "SPEECH_LANGUAGE": "en",
		"SPEECH_STT_MODEL": "whisper-base.en", "SPEECH_TTS_MODEL": "kokoro-en", "SPEECH_TTS_VOICE": "af_heart",
		"SPEECH_STT_IMAGE": release.STT, "SPEECH_TTS_IMAGE": release.TTS, "SPARTASK_VOICE_IMAGE": release.Voice,
		"SPEECH_STT_CPUS": "2", "SPEECH_STT_MEMORY": "1g", "SPEECH_TTS_CPUS": "1", "SPEECH_TTS_MEMORY": "2g",
		"SPEECH_GLOBAL_CONCURRENCY": "1", "SPEECH_TENANT_CONCURRENCY": "1", "MEETING_VOICE_MAX_ROOMS": "1", "MEETING_VOICE_MAX_TENANT_ROOMS": "1",
		"MEETING_SERVICES_START_TIMEOUT": "300s",
	} {
		env.Set(key, value)
	}
	return nil
}

// A profile marker grants the bundled provider contract only to matching images
// and model settings; a custom provider must leave the marker empty.
func (p *meetingPlan) validateLocalSpeech(s stack, env *envFile) {
	profile := env.Get("SPEECH_LOCAL_PROFILE")
	if profile == "" {
		return
	}
	if profile != localSpeechProfile {
		p.invalid = append(p.invalid, "SPEECH_LOCAL_PROFILE: desteklenmeyen profil; özel sağlayıcı için boş bırakın")
		return
	}
	release, err := readSpeechRelease(s)
	if err != nil {
		p.invalid = append(p.invalid, err.Error())
		return
	}
	for key, want := range map[string]string{
		"SPEECH_PROVIDER_REVISION": localSpeechProfile, "SPEECH_LANGUAGE": "en",
		"SPEECH_STT_MODEL": "whisper-base.en", "SPEECH_TTS_MODEL": "kokoro-en", "SPEECH_TTS_VOICE": "af_heart",
		"SPEECH_STT_IMAGE": release.STT, "SPEECH_TTS_IMAGE": release.TTS, "SPARTASK_VOICE_IMAGE": release.Voice,
		"SPEECH_STT_MANAGED": "true", "SPEECH_TTS_MANAGED": "true",
		"SPEECH_STT_ENV_FILE": "", "SPEECH_TTS_ENV_FILE": "",
	} {
		if env.Get(key) != want {
			p.invalid = append(p.invalid, key+": yayımlanan yerel speech profiliyle eşleşmiyor; config -speech-local ile yeniden yapılandırın veya özel sağlayıcı için SPEECH_LOCAL_PROFILE alanını boşaltın")
		}
	}
}
