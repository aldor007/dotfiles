package agent

import (
	"os"
	"strings"
)

// sensitiveEnvSubstr matches credential-shaped env vars by substring.
// The reviewer's read-only contract is enforced by exclusion: any env
// var whose name contains one of these is stripped before the agent
// subprocess starts. False positives (e.g. PATH_ANDROID_KEYSTORE) are
// extremely rare; false negatives (custom token names) are a known
// limitation of name-based filtering — the reviewer should not store
// tokens under non-standard names, and the broader containment comes
// from running in a worktree with restricted filesystem writes.
var sensitiveEnvSubstr = []string{
	"TOKEN", "SECRET", "PASSWORD", "PASSWD", "API_KEY", "APIKEY",
	"PRIVATE_KEY", "ACCESS_KEY", "CREDENTIALS",
	// hosted credential services
	"AWS_SECRET", "AWS_SESSION", "GCP_APPLICATION", "GCP_SERVICE_ACCOUNT",
	"GITHUB_TOKEN", "GH_TOKEN", "GITLAB_TOKEN", "GITLAB_FEATURES",
	"KIMCHI_API", "OPENAI_API", "ANTHROPIC_API", "CLAUDE_API",
	"SLACK_TOKEN", "NOTION_TOKEN", "GOOGLE_API",
}

// keepEnvSubstr is the allowlist for non-credential env vars. We preserve
// the agent's runtime essentials (PATH for tool invocation, HOME for
// config reads, LANG/LC_* for output, TMPDIR for scratch space) plus
// LLM gateway endpoints the agent must reach to read MR context. This
// is INCLUSIVE on purpose: anything not matched here gets stripped.
var keepEnvSubstr = []string{
	"PATH", "HOME", "USER", "LOGNAME", "SHELL", "TMPDIR", "TMP",
	"LANG", "LC_", "TZ",
	// LLM gateway configuration (NOT credentials — those have TOKEN/KEY
	// and are stripped). Just the endpoint URL + project name.
	"LITELLM", "OPENAI_BASE_URL", "ANTHROPIC_BASE_URL",
	// tool-specific read-only flags
	"GOPATH", "GOMODCACHE", "GOFLAGS", "GOROOT",
	"DOCKER_HOST", // read-only docker queries via socket
	"KUBECONFIG",  // cluster inspection (read-only)
}

// sandboxEnv returns a sanitized environment for the reviewer subprocess.
// It's an allowlist of safe prefixes + a denylist of sensitive substrings:
// any var matching keepEnvSubstr passes through; any var matching
// sensitiveEnvSubstr is stripped; everything else (unrecognized tokens
// the user happens to have in their shell) is also stripped.
func sandboxEnv() []string {
	src := os.Environ()
	out := make([]string, 0, len(src))
	for _, kv := range src {
		eq := strings.IndexByte(kv, '=')
		if eq < 0 {
			continue
		}
		name := kv[:eq]
		upper := strings.ToUpper(name)
		// Strip credential-shaped names outright (the denylist wins
		// over the allowlist — a var named "GH_TOKEN_PROXY" still has
		// the substring TOKEN).
		if matchesAny(upper, sensitiveEnvSubstr) {
			continue
		}
		// Pass through the allowlist.
		if matchesAny(upper, keepEnvSubstr) {
			out = append(out, kv)
			continue
		}
	}
	// Always inject the worktree + LLM-friendly defaults so the agent
	// has a deterministic runtime even when the parent shell was bare.
	out = append(out,
		"MR_REVIEW_SANDBOX=1",
		"GIT_TERMINAL_PROMPT=0", // never prompt for credentials
		"PYTHONDONTWRITEBYTECODE=1",
	)
	return out
}

func matchesAny(s string, subs []string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
