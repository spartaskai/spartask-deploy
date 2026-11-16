package main

import "strings"

// Server inference configuration is retired. Browser and LiveKit settings are separate.
func retiredServerSpeechKey(key string) bool {
	return strings.HasPrefix(key, "SPEECH_") || strings.HasPrefix(key, "MEETING_VOICE_") || key == "SPARTASK_VOICE_IMAGE"
}
func retireServerSpeech(env *envFile) []string {
	var removed []string
	for _, key := range env.keys() {
		if retiredServerSpeechKey(key) {
			env.Remove(key)
			removed = append(removed, key)
		}
	}
	return removed
}
func configureBrowserSpeech(env *envFile) {
	retireServerSpeech(env)
	env.Set("MEETING_BROWSER_SPEECH_ENABLED", "true")
	if env.Get("MEETING_BROWSER_SPEECH_LANGUAGE") == "" {
		env.Set("MEETING_BROWSER_SPEECH_LANGUAGE", "tr")
	}
}
