package agent

import (
	"fmt"
	"os"
	"strings"
)

// This file implements the environment half of the reviewer's containment.
// It is NOT an OS sandbox: the subprocess runs as the invoking user with
// the user's full filesystem permissions. Containment is layered and
// best-effort:
//
//  1. allowlist (keepEnvExact/keepEnvPrefix): only variables the agent
//     runtime demonstrably needs pass through; everything else is dropped
//     (KUBECONFIG, DOCKER_HOST, AWS_*, SSH_AUTH_SOCK, XDG_*, GO_*, ...),
//  2. denylist (sensitiveEnvSubstr): credential-shaped names are stripped
//     even when they would match the allowlist,
//  3. verifyAgentEnv fails closed before the agent starts if a dangerous
//     variable is still present without an explicit opt-in,
//  4. the review worktree is the agent's cwd (RunReview/Discuss set
//     cmd.Dir); publication lives in the separate human-only
//     mr-review-publish binary the agent never invokes,
//  5. the prompt forbids writes, network and git write commands, and all
//     agent output is ingested as inert data (Finding.Validate).
//
// Residual risk (documented in README.md): the agent could read or write
// any file its user can. Only real OS sandboxing would remove that; it is
// out of scope here.

// sensitiveEnvSubstr matches credential-shaped env vars by substring
// (upper-cased). The denylist wins over the allowlist: a var named
// GH_TOKEN_PROXY still contains TOKEN and is stripped. Name-based
// filtering cannot catch custom token names — a known limitation; the
// allowlist bounds the blast radius because anything unrecognized is
// dropped anyway.
var sensitiveEnvSubstr = []string{
	"TOKEN", "SECRET", "PASSWORD", "PASSWD", "API_KEY", "APIKEY",
	"PRIVATE_KEY", "ACCESS_KEY", "CREDENTIALS",
	// hosted credential services
	"AWS_SECRET", "AWS_SESSION", "GCP_APPLICATION", "GCP_SERVICE_ACCOUNT",
	"GITHUB_TOKEN", "GH_TOKEN", "GITLAB_TOKEN", "GITLAB_FEATURES",
	"KIMCHI_API", "OPENAI_API", "ANTHROPIC_API", "CLAUDE_API",
	"SLACK_TOKEN", "NOTION_TOKEN", "GOOGLE_API",
}

// keepEnvExact is the allowlist of exact (upper-cased) env names the
// reviewer runtime demonstrably needs. Anything not listed here — and not
// matched by keepEnvPrefix — is dropped. Notable decisions:
//
//   - HOME is kept because the agent runtime cannot run without it:
//     kimchi reads its LLM configuration from ~/.kimchi, git resolves the
//     user's identity from ~/.gitconfig, and go derives its build cache
//     from the home directory.
//   - KUBECONFIG and DOCKER_HOST are dropped: a review is a local code
//     read, not a cluster or docker operation. They pass only via the
//     explicit opt-ins below (MR_REVIEW_AGENT_ALLOW_KUBE_ENV /
//     MR_REVIEW_AGENT_ALLOW_DOCKER_ENV), which also exempt them from the
//     fail-closed startup check.
//   - SSH_AUTH_SOCK is dropped: no ssh-agent forwarding for a reviewer.
//   - GO*/XDG_* and everything else unrecognized is dropped: PATH+HOME
//     give the tools sane defaults.
var keepEnvExact = []string{
	"PATH", // tool invocation: go, git, glab, jq, the LLM runtime itself
	"HOME", // agent runtime requirement (see comment above)
	"USER", "LOGNAME", "SHELL",
	"TMPDIR", "TMP", "TEMP",
	"LANG", "LANGUAGE", "TZ",
	"TERM", "TERM_PROGRAM", "TERM_PROGRAM_VERSION", "COLORTERM", "NO_COLOR",
	// LLM gateway endpoints (NOT credentials — credential-shaped names are
	// stripped by the denylist): where the reviewer's model API lives.
	"OPENAI_BASE_URL", "ANTHROPIC_BASE_URL",
}

// keepEnvPrefix is the allowlist of (upper-cased) name prefixes.
var keepEnvPrefix = []string{
	"LC_",        // locale subcategories (LC_ALL, LC_CTYPE, ...)
	"LITELLM_",   // LLM gateway endpoint/model configuration
	"MR_REVIEW_", // mr-review's own config surface (MR_REVIEW_AGENT,
	// MR_REVIEW_GLAB, MR_REVIEW_HOME, ...). The denylist still wins: a var
	// like MR_REVIEW_SANDBOX_PARENT_TOKEN is stripped.
}

// Explicit opt-ins (following the established MR_REVIEW_* env-config
// pattern) that re-enable exactly one dangerous variable for the reviewer.
const (
	allowKubeEnv   = "MR_REVIEW_AGENT_ALLOW_KUBE_ENV"
	allowDockerEnv = "MR_REVIEW_AGENT_ALLOW_DOCKER_ENV"
)

// injectedEnv is appended to every sanitized environment: deterministic
// runtime defaults that hold even when the parent shell was bare.
var injectedEnv = []string{
	"MR_REVIEW_SANDBOX=1",
	"GIT_TERMINAL_PROMPT=0", // never prompt for credentials
	"PYTHONDONTWRITEBYTECODE=1",
}

// sandboxEnv returns the sanitized environment for the reviewer subprocess:
// the allowlist above minus the credential denylist, plus the opt-ins and
// the injected defaults. Callers must run the result through verifyAgentEnv
// (fail-closed) before exec.
func sandboxEnv() []string {
	kube := optInEnabled(allowKubeEnv)
	docker := optInEnabled(allowDockerEnv)
	src := os.Environ()
	out := make([]string, 0, len(src)+len(injectedEnv))
	for _, kv := range src {
		eq := strings.IndexByte(kv, '=')
		if eq < 0 {
			continue
		}
		name := kv[:eq]
		upper := strings.ToUpper(name)
		// Denylist wins over everything.
		if matchesAny(upper, sensitiveEnvSubstr) {
			continue
		}
		// An explicit opt-in re-enables exactly one variable.
		if (kube && upper == "KUBECONFIG") || (docker && upper == "DOCKER_HOST") {
			out = append(out, kv)
			continue
		}
		if keepEnvAllowed(upper) {
			out = append(out, kv)
		}
	}
	return append(out, injectedEnv...)
}

func keepEnvAllowed(upper string) bool {
	for _, e := range keepEnvExact {
		if upper == e {
			return true
		}
	}
	for _, p := range keepEnvPrefix {
		if strings.HasPrefix(upper, p) {
			return true
		}
	}
	return false
}

// verifyAgentEnv is the fail-closed startup check: it scans the final
// environment and refuses to start the agent if a dangerous variable is
// present without an explicit opt-in. Credential-shaped names fail even
// with an opt-in. Callers must run this on the exact env they are about
// to hand to the subprocess.
func verifyAgentEnv(env []string) error {
	var bad []string
	for _, kv := range env {
		eq := strings.IndexByte(kv, '=')
		name := kv
		if eq >= 0 {
			name = kv[:eq]
		}
		upper := strings.ToUpper(name)
		switch {
		case matchesAny(upper, sensitiveEnvSubstr):
			bad = append(bad, name+" (credential-shaped, never allowed)")
		case upper == "KUBECONFIG" && !optInEnabled(allowKubeEnv):
			bad = append(bad, name+" (set "+allowKubeEnv+"=1 to opt in)")
		case upper == "DOCKER_HOST" && !optInEnabled(allowDockerEnv):
			bad = append(bad, name+" (set "+allowDockerEnv+"=1 to opt in)")
		case strings.HasPrefix(upper, "AWS_"):
			bad = append(bad, name+" (AWS_* is never passed to the reviewer)")
		}
	}
	if len(bad) > 0 {
		return fmt.Errorf("refusing to start the reviewer with dangerous env vars present: %s",
			strings.Join(bad, ", "))
	}
	return nil
}

// optInEnabled reads an explicit opt-in switch ("1"/"true"/"yes").
func optInEnabled(name string) bool {
	switch strings.ToLower(os.Getenv(name)) {
	case "1", "true", "yes":
		return true
	}
	return false
}

func matchesAny(s string, subs []string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
