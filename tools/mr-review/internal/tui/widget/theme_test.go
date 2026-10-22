package widget

import (
	"reflect"
	"testing"

	"charm.land/lipgloss/v2"
)

func TestDefaultMatchesLegacyPalette(t *testing.T) {
	// The default theme must byte-match the pre-widget hardcoded colors.
	want := map[Token]string{
		TokFocus: "39", TokOK: "78", TokWarn: "214", TokErr: "196",
		TokPub: "213", TokHeld: "243", TokAccent: "39", TokSection: "63",
		TokBorder: "240", TokBorderFocus: "39", TokMarker: "39", TokSel: "39",
		TokBranch: "39", TokSevP0: "196", TokSevP1: "203", TokSevP2: "214",
		TokSevP3: "250", TokDecisionPending: "185", TokTabActiveBg: "39",
		TokTabActiveFg: "235",
	}
	got := Default()
	if got.Name != "default" {
		t.Fatalf("default theme name = %q", got.Name)
	}
	for tok, wantVal := range want {
		if gotVal := got.Raw(tok); gotVal != wantVal {
			t.Errorf("token %s = %q, want %q", tok, gotVal, wantVal)
		}
	}
	if len(want) != len(got.colors) {
		t.Errorf("default theme has %d tokens, want %d", len(got.colors), len(want))
	}
}

func TestColorResolvesToLipgloss(t *testing.T) {
	th := Default()
	if c := th.Color(TokOK); c != lipgloss.Color("78") {
		t.Errorf("Color(OK) = %v, want 78", c)
	}
	if c := Dracula().Color(TokOK); c != lipgloss.Color("#50fa7b") {
		t.Errorf("Dracula Color(OK) = %v, want #50fa7b", c)
	}
}

func TestResolveUnknownName(t *testing.T) {
	_, err := Resolve("solarized", nil)
	if err == nil {
		t.Fatal("unknown theme must error")
	}
	if !contains(err.Error(), "solarized") || !contains(err.Error(), "dracula") {
		t.Errorf("error should name the theme and available ones, got: %v", err)
	}
}

func TestResolveEmptyNameDefaults(t *testing.T) {
	th, err := Resolve("", nil)
	if err != nil {
		t.Fatalf("empty name: %v", err)
	}
	if th.Name != "default" {
		t.Errorf("empty name resolved to %q, want default", th.Name)
	}
}

func TestResolveOverrides(t *testing.T) {
	th, err := Resolve("default", map[string]string{"OK": "#00ff00", "Err": "9"})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if th.Raw(TokOK) != "#00ff00" || th.Raw(TokErr) != "9" {
		t.Errorf("overrides not applied: OK=%q Err=%q", th.Raw(TokOK), th.Raw(TokErr))
	}
	if th.Raw(TokWarn) != "214" {
		t.Errorf("non-overridden token changed: Warn=%q", th.Raw(TokWarn))
	}
}

func TestResolveUnknownToken(t *testing.T) {
	_, err := Resolve("default", map[string]string{"NotAToken": "9"})
	if err == nil || !contains(err.Error(), "NotAToken") {
		t.Fatalf("unknown token must error naming it, got: %v", err)
	}
}

func TestResolveEmptyColor(t *testing.T) {
	_, err := Resolve("default", map[string]string{"OK": ""})
	if err == nil {
		t.Fatal("empty color must error")
	}
}

func TestOverrideDoesNotMutateBuiltin(t *testing.T) {
	if _, err := Resolve("dracula", map[string]string{"OK": "#123123"}); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if Dracula().Raw(TokOK) != "#50fa7b" {
		t.Error("builtin theme was mutated by an override")
	}
}

func TestAllTokensComplete(t *testing.T) {
	if want := len(Default().colors); len(AllTokens()) != want {
		t.Errorf("AllTokens = %d entries, default theme has %d", len(AllTokens()), want)
	}
	if !reflect.DeepEqual(sortedCopy(AllTokens()), AllTokens()) {
		t.Error("AllTokens must be sorted")
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func sortedCopy(in []string) []string {
	out := append([]string(nil), in...)
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	return out
}
