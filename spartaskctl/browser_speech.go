package main

// This transition changes no LiveKit credentials or media settings. Keeping it
// separate from the old local profile makes repeated config calls safe.
func configureBrowserSpeech(env *envFile) {
	for key, value := range map[string]string{
		"MEETING_BROWSER_SPEECH_ENABLED": "true", "MEETING_VOICE_AGENT_SPEECH": "browser",
		"MEETING_VOICE_ENABLED": "false", "SPEECH_ENABLED": "false", "SPEECH_STT_ENABLED": "false", "SPEECH_TTS_ENABLED": "false",
		"SPEECH_STT_MANAGED": "false", "SPEECH_TTS_MANAGED": "false", "SPEECH_LOCAL_PROFILE": "",
	} {
		env.Set(key, value)
	}
	if env.Get("MEETING_BROWSER_SPEECH_LANGUAGE") == "" {
		env.Set("MEETING_BROWSER_SPEECH_LANGUAGE", "tr")
	}
}
