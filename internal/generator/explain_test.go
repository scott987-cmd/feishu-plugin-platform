package generator

import (
	"errors"
	"strings"
	"testing"
)

func TestExplain(t *testing.T) {
	cases := []struct {
		name    string
		err     string
		wantSub string // a distinctive substring of the expected hint
	}{
		{"ts2554", "build: src/index.ts(12,5): error TS2554: Expected 2 arguments, but got 1.", "少给了参数"},
		{"tsc generic", "build failed: error TS2339: Property 'x' does not exist", "没通过真实 SDK 编译"},
		{"domain", "validation failed: execute.url: host not in domains", "出网域名白名单"},
		{"primary type", "exhausted 3 repair rounds; last problem: validation failed: a primary column must be Text or Number", "主输出列只能是文本或数字"},
		{"missing", "validation failed: at least one form item is required", "缺少必要信息"},
		{"model", "model did not call emit_shortcut", "结构化结果"},
		{"network", "deepseek: connection refused", "调用 AI 模型失败"},
		{"exhausted generic", "exhausted 3 repair rounds; last problem: ", "试了几轮"},
		{"fallback", "something totally unexpected happened", "更具体些"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			hint, detail := Explain(errors.New(c.err))
			if hint == "" {
				t.Fatalf("hint is empty for %q", c.err)
			}
			if detail != c.err {
				t.Errorf("detail = %q, want the raw error %q", detail, c.err)
			}
			if !strings.Contains(hint, c.wantSub) {
				t.Errorf("hint = %q, want it to contain %q", hint, c.wantSub)
			}
		})
	}
}

func TestExplainNil(t *testing.T) {
	if h, d := Explain(nil); h != "" || d != "" {
		t.Errorf("Explain(nil) = (%q,%q), want empty", h, d)
	}
}
