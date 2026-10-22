package agent

import "testing"

// TestSandboxEnvStripsCredentials — review finding P1 #4. The reviewer
// subprocess must not inherit token-shaped env vars from the parent
// shell. Anything containing TOKEN, SECRET, PASSWORD, etc. is stripped,
// regardless of whether the parent had a non-standard name for it (e.g.
// GITLAB_TOKEN, GL_TOKEN, GH_TOKEN, KIMCHI_API_KEY).
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
	t.Setenv("ANTHROPIC_API_KEY", "fake-value-not-a-real-token")
	t.Setenv("MY_PASSWORD", "fake-value-not-a-real-password")
	t.Setenv("PATH", "/usr/local/bin:/usr/bin")           // keep
	t.Setenv("HOME", "/Users/x")                          // keep
	t.Setenv("MR_REVIEW_SANDBOX_PARENT_TOKEN", "fake-value-not-a-real-token") // sensitive — strip

	env := sandboxEnv()
	joined := map[string]string{}
	for _, kv := range env {
		for i := 0; i < len(kv); i++ {
			if kv[i] == '=' {
				joined[kv[:i]] = kv[i+1:]
				break
			}
		}
	}

	for _, stripped := range []string{
		"GH_TOKEN", "GITLAB_TOKEN", "KIMCHI_API_KEY",
		"AWS_SECRET_ACCESS_KEY", "ANTHROPIC_API_KEY",
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

// TestSandboxEnvInjectsSafe — review finding P1 #4. The sandbox adds
// non-secret helpers (GIT_TERMINAL_PROMPT=0, MR_REVIEW_SANDBOX=1) so
// the agent can never hang on a credential prompt and downstream code
// can detect when it's running inside the sandbox.
func TestSandboxEnvInjectsSafe(t *testing.T) {
	t.Setenv("MR_REVIEW_SANDBOX", "") // override to ensure injected regardless
	env := sandboxEnv()
	joined := map[string]string{}
	for _, kv := range env {
		for i := 0; i < len(kv); i++ {
			if kv[i] == '=' {
				joined[kv[:i]] = kv[i+1:]
				break
			}
		}
	}
	if joined["MR_REVIEW_SANDBOX"] != "1" {
		t.Errorf("sandbox must inject MR_REVIEW_SANDBOX=1, got %q", joined["MR_REVIEW_SANDBOX"])
	}
	if joined["GIT_TERMINAL_PROMPT"] != "0" {
		t.Errorf("sandbox must inject GIT_TERMINAL_PROMPT=0 to prevent credential prompts")
	}
}
