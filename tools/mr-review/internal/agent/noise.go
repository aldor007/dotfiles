// noise.go — shared filter for agent-infrastructure chatter (warnings,
// session banners) that must never leak into user-visible answers.
package agent

import (
	"regexp"
	"strings"
)

var reANSI = regexp.MustCompile("\x1b\\[[0-9;?]*[a-zA-Z]")

var noiseSubstrings = []string{
	"[model-roles]",           // kimchi judge-model fallback warning
	"judge model",             // continuation of the same notice
	"Subagents for this role", // …and its trailing advice
	"Update via /multi-model",
	"^ Cancelled",               // kimchi shutdown line
	"KIMCHI_API_KEY",            // env-vs-config key warning
	"Using the environment key", // …and its explanation
	"Work matching",             // pi session-continuation banner
	"continued work:",           // …its continuation id
}

// IsNoise reports whether a line is agent-infrastructure chatter.
func IsNoise(line string) bool {
	clean := strings.TrimSpace(reANSI.ReplaceAllString(line, ""))
	if clean == "" {
		return true
	}
	for _, s := range noiseSubstrings {
		if strings.Contains(clean, s) {
			return true
		}
	}
	return false
}

// CleanAnswer strips noise lines from raw agent output and returns the
// remaining answer text ("" when the output was all noise).
func CleanAnswer(raw string) string {
	var keep []string
	for _, l := range strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n") {
		if !IsNoise(l) {
			keep = append(keep, strings.TrimRight(l, " "))
		}
	}
	return strings.TrimSpace(strings.Join(keep, "\n"))
}
