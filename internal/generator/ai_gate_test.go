package generator

import "testing"

// TestAIEnabledGate locks the AI_ENABLED kill-switch: when set to "false", NO
// natural-language entry point may call the LLM (the prompt must never egress),
// even with an API key present — it returns ok=false so callers fall back to
// templates / the keyword router.
func TestAIEnabledGate(t *testing.T) {
	t.Setenv("DEEPSEEK_API_KEY", "sk-fake-present") // key IS present...
	t.Setenv("AI_ENABLED", "false")                 // ...but AI is hard-disabled

	if AIEnabled() {
		t.Fatal("AIEnabled() = true with AI_ENABLED=false, want false")
	}
	if _, ok, err := GenerateShortcut("城市天气"); ok || err != nil {
		t.Errorf("GenerateShortcut (AI off) = ok=%v err=%v, want ok=false,nil (no egress)", ok, err)
	}
	if _, ok, err := GenerateAction("城市天气"); ok || err != nil {
		t.Errorf("GenerateAction (AI off) = ok=%v err=%v, want ok=false,nil (no egress)", ok, err)
	}
	if _, ok, err := generateWithDeepSeek("城市天气"); ok || err != nil {
		t.Errorf("generateWithDeepSeek (AI off) = ok=%v err=%v, want ok=false,nil (no egress)", ok, err)
	}

	// Default (unset) is enabled.
	t.Setenv("AI_ENABLED", "")
	if !AIEnabled() {
		t.Error("AIEnabled() = false when AI_ENABLED unset, want true (default on)")
	}
}
