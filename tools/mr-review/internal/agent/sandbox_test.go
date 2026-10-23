package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// toEnvMap parses a "NAME=value" list into a map.
func toEnvMap(env []string) map[string]string {
	m := make(map[string]string, len(env))
	for _, kv := range env {
		for i := 0; i < len(kv); i++ {
			if kv[i] == '=' {
				m[kv[:i]] = kv[i+1:]
				break
			}
		}
	}
	return m
}

// TestSandboxEnvStripsCredentials — review finding P1 #4. The reviewer
// subprocess must not inherit token-shaped env vars from the parent
// shell. Anything containing TOKEN, SECRET, PASSWORD, etc. is stripped,
// regardless of whether the parent had a non-standard name for it (e.g.
// GITLAB_TOKEN, GL_TOKEN, GH_TOKEN, KIMCHI_API_KEY). The denylist wins
// over the allowlist: even an MR_REVIEW_-prefixed credential is stripped.
func TestSandboxEnvStripsCredentials(t *testing.T) {
	// Fixture values are deliberately low-entropy / clearly-fake so they
	// don't trip secret scanners (gitleaks generic-api-key rule has tripped
	// on 'hunter2', 'ghp_...' and other believable-looking fakes in past
	// tests — this strips the suspicion surface entirely). The test cares
	// about the env-var NAME being stripped, not the value.
	t.Setenv("GH_TOKEN", "fake-value-not-a-real-token")
	t.Setenv("GITLAB_TOKEN", "fake-value-not-a-real-token")
	t.Setenv("KIMCHI_API_KEY", "fake-value-not-a-real-token")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "fake-value-not-a-real-token")
	t.Setenv("AWS_ACCESS_KEY_ID", "fake-value-not-a-real-id")
	t.Setenv("ANTHROPIC_API_KEY", "fake-value-not-a-real-token")
	t.Setenv("MY_PASSWORD", "fake-value-not-a-real-password")
	t.Setenv("MR_REVIEW_SANDBOX_PARENT_TOKEN", "fake-value-not-a-real-token") // MR_REVIEW_* but denylist wins
	t.Setenv("PATH", "/usr/local/bin:/usr/bin")                               // keep
	t.Setenv("HOME", "/Users/x")                                              // keep

	env := sandboxEnv()
	joined := toEnvMap(env)

	for _, stripped := range []string{
		"GH_TOKEN", "GITLAB_TOKEN", "KIMCHI_API_KEY",
		"AWS_SECRET_ACCESS_KEY", "AWS_ACCESS_KEY_ID", "ANTHROPIC_API_KEY",
		"MY_PASSWORD", "MR_REVIEW_SANDBOX_PARENT_TOKEN",
	} {
		if _, leaked := joined[stripped]; leaked {
			t.Errorf("sensitive env %q leaked to sandbox: %q", stripped, joined[stripped])
		}
	}
	for _, kept := range []string{"PATH", "HOME", "MR_REVIEW_SANDBOX"} {
		if _, ok := joined[kept]; !ok {
			t.Errorf("allowed env %q must be present in sandbox", kept)
		}
	}
}

// TestSandboxEnvAllowlist — the tightened allowlist keeps exactly the
// runtime needs and drops cluster/docker/cloud/ssh/IDE plumbing.
func TestSandboxEnvAllowlist(t *testing.T) {
	keep := map[string]string{
		"PATH":                  "/usr/bin",
		"HOME":                  "/Users/x",
		"USER":                  "x",
		"TMPDIR":                "/tmp",
		"LANG":                  "en_US.UTF-8",
		"LC_ALL":                "en_US.UTF-8",
		"LC_CTYPE":              "UTF-8",
		"TERM":                  "xterm-256color",
		"TZ":                    "UTC",
		"NO_COLOR":              "1",
		"OPENAI_BASE_URL":       "http://gateway.local/v1",
		"ANTHROPIC_BASE_URL":    "http://gateway.local/v1",
		"LITELLM_BASE_URL":      "http://gateway.local",
		"MR_REVIEW_GLAB":        "/usr/local/bin/glab-stub", // MR_REVIEW_* config passes
		"MR_REVIEW_AGENT":       "pi",
		"MR_REVIEW_SESSION_DIR": "/tmp/sessions",
	}
	drop := []string{
		"KUBECONFIG",          // cluster access: dropped without explicit opt-in
		"DOCKER_HOST",         // docker socket: dropped without explicit opt-in
		"AWS_REGION",          // AWS_* plumbing: dropped
		"AWS_PROFILE",         // AWS_* plumbing: dropped
		"SSH_AUTH_SOCK",       // no ssh-agent forwarding
		"SSH_CONNECTION",      // no ssh plumbing
		"GOPATH",              // PATH+HOME give go sane defaults
		"GOFLAGS",             //
		"GOMODCACHE",          //
		"DISPLAY",             //
		"XDG_DATA_HOME",       //
		"npm_config_registry", // unrecognized shell noise
	}
	for k, v := range keep {
		t.Setenv(k, v)
	}
	for _, k := range drop {
		t.Setenv(k, "/some/value")
	}

	joined := toEnvMap(sandboxEnv())
	for k := range keep {
		if _, ok := joined[k]; !ok {
			t.Errorf("allowlist must keep %q", k)
		}
	}
	for _, k := range drop {
		if v, ok := joined[k]; ok {
			t.Errorf("allowlist must drop %q, got %q", k, v)
		}
	}
}

// TestSandboxEnvInjectsSafe — review finding P1 #4. The sandbox adds
// non-secret helpers (GIT_TERMINAL_PROMPT=0, MR_REVIEW_SANDBOX=1) so
// the agent can never hang on a credential prompt and downstream code
// can detect when it's running inside the sandbox.
func TestSandboxEnvInjectsSafe(t *testing.T) {
	t.Setenv("MR_REVIEW_SANDBOX", "") // override to ensure injected regardless
	env := sandboxEnv()
	joined := toEnvMap(env)
	if joined["MR_REVIEW_SANDBOX"] != "1" {
		t.Errorf("sandbox must inject MR_REVIEW_SANDBOX=1, got %q", joined["MR_REVIEW_SANDBOX"])
	}
	if joined["GIT_TERMINAL_PROMPT"] != "0" {
		t.Errorf("sandbox must inject GIT_TERMINAL_PROMPT=0 to prevent credential prompts")
	}
}

// TestSandboxEnvOptIns — KUBECONFIG/DOCKER_HOST pass only when the
// matching MR_REVIEW_AGENT_ALLOW_* opt-in is set (the established
// MR_REVIEW_* env-config pattern).
func TestSandboxEnvOptIns(t *testing.T) {
	t.Setenv("KUBECONFIG", "/home/u/.kube/config")
	t.Setenv("DOCKER_HOST", "unix:///var/run/docker.sock")

	joined := toEnvMap(sandboxEnv())
	if _, ok := joined["KUBECONFIG"]; ok {
		t.Error("KUBECONFIG must be dropped without MR_REVIEW_AGENT_ALLOW_KUBE_ENV")
	}
	if _, ok := joined["DOCKER_HOST"]; ok {
		t.Error("DOCKER_HOST must be dropped without MR_REVIEW_AGENT_ALLOW_DOCKER_ENV")
	}

	t.Setenv("MR_REVIEW_AGENT_ALLOW_KUBE_ENV", "1")
	joined = toEnvMap(sandboxEnv())
	if joined["KUBECONFIG"] != "/home/u/.kube/config" {
		t.Errorf("opt-in must pass KUBECONFIG through, got %q", joined["KUBECONFIG"])
	}
	if _, ok := joined["DOCKER_HOST"]; ok {
		t.Error("kube opt-in must not enable DOCKER_HOST")
	}

	t.Setenv("MR_REVIEW_AGENT_ALLOW_DOCKER_ENV", "true")
	joined = toEnvMap(sandboxEnv())
	if joined["DOCKER_HOST"] != "unix:///var/run/docker.sock" {
		t.Errorf("opt-in must pass DOCKER_HOST through, got %q", joined["DOCKER_HOST"])
	}
}

// TestVerifyAgentEnvFailsClosed — the startup check must refuse to start
// the agent when a dangerous variable survived without an explicit opt-in.
func TestVerifyAgentEnvFailsClosed(t *testing.T) {
	cases := []struct {
		name string
		env  []string
		want string // substring of the expected error ("" = must pass)
	}{
		{"clean", []string{"PATH=/usr/bin", "HOME=/u", "LANG=C"}, ""},
		{"kubeconfig", []string{"PATH=/usr/bin", "KUBECONFIG=/u/.kube/config"}, "KUBECONFIG"},
		{"dockerhost", []string{"PATH=/usr/bin", "DOCKER_HOST=unix:///d.sock"}, "DOCKER_HOST"},
		{"aws", []string{"PATH=/usr/bin", "AWS_SECRET_ACCESS_KEY=fake"}, "AWS_SECRET_ACCESS_KEY"},
		{"aws-region", []string{"PATH=/usr/bin", "AWS_REGION=eu-central-1"}, "AWS_REGION"},
		{"gh-token", []string{"PATH=/usr/bin", "GH_TOKEN=fake"}, "GH_TOKEN"},
		{"multiple", []string{"KUBECONFIG=/k", "DOCKER_HOST=/d", "AWS_PROFILE=p"}, "KUBECONFIG"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := verifyAgentEnv(tc.env)
			if tc.want == "" {
				if err != nil {
					t.Fatalf("clean env must pass, got: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected fail-closed error mentioning %q, got: %v", tc.want, err)
			}
		})
	}

	// Opt-ins exempt exactly their variable.
	t.Setenv("MR_REVIEW_AGENT_ALLOW_KUBE_ENV", "1")
	if err := verifyAgentEnv([]string{"PATH=/usr/bin", "KUBECONFIG=/k"}); err != nil {
		t.Fatalf("kube opt-in must allow KUBECONFIG, got: %v", err)
	}
	if err := verifyAgentEnv([]string{"PATH=/usr/bin", "DOCKER_HOST=/d"}); err == nil {
		t.Fatal("kube opt-in must not exempt DOCKER_HOST")
	}
	t.Setenv("MR_REVIEW_AGENT_ALLOW_DOCKER_ENV", "1")
	if err := verifyAgentEnv([]string{"PATH=/usr/bin", "KUBECONFIG=/k", "DOCKER_HOST=/d"}); err != nil {
		t.Fatalf("both opt-ins must allow both vars, got: %v", err)
	}
	// Credentials fail even with every opt-in set.
	if err := verifyAgentEnv([]string{"PATH=/usr/bin", "GH_TOKEN=fake"}); err == nil {
		t.Fatal("credential-shaped vars must fail even with opt-ins set")
	}
}

// TestSandboxEnvPassesVerification — invariant: whatever the parent shell
// contains, the sanitized environment must satisfy the fail-closed check.
func TestSandboxEnvPassesVerification(t *testing.T) {
	t.Setenv("KUBECONFIG", "/home/u/.kube/config")
	t.Setenv("DOCKER_HOST", "unix:///var/run/docker.sock")
	t.Setenv("AWS_SESSION_TOKEN", "fake-value-not-a-real-token")
	t.Setenv("GH_TOKEN", "fake-value-not-a-real-token")
	t.Setenv("SSH_AUTH_SOCK", "/tmp/launchd-ssh")
	if err := verifyAgentEnv(sandboxEnv()); err != nil {
		t.Fatalf("sanitized env must pass the fail-closed check: %v", err)
	}
}

// TestDocsHonestContainmentClaims — the docs must not overclaim the
// containment (no "read-only contract", no "restricted filesystem", no
// bare "sandbox" promises) and must state the residual risk plainly
// (user-level filesystem permissions apply).
func TestDocsHonestContainmentClaims(t *testing.T) {
	docs := []string{
		filepath.Join("..", "..", "README.md"),
		filepath.Join("..", "..", "docs", "mr-review.md"),
	}
	forbidden := []string{
		"read-only contract",    // env filtering does not enforce read-only
		"restricted filesystem", // false: the worktree has no write restrictions
		"sandboxed",             // no OS sandbox exists
		"fully isolated",        // overclaim
	}
	required := []string{"filesystem permissions"}
	for _, doc := range docs {
		b, err := os.ReadFile(doc)
		if err != nil {
			t.Fatalf("read %s: %v", doc, err)
		}
		lower := strings.ToLower(string(b))
		for _, f := range forbidden {
			if strings.Contains(lower, f) {
				t.Errorf("%s must not overclaim containment: found %q", doc, f)
			}
		}
		for _, r := range required {
			if !strings.Contains(lower, r) {
				t.Errorf("%s must state the residual risk (%q)", doc, r)
			}
		}
	}
}
