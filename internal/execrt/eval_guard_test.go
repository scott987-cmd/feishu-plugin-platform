package execrt

import (
	"strings"
	"testing"
)

// TestEvalExprDepthGuard ensures deeply-nested expressions error out instead of
// overflowing the goroutine stack (a fatal, recover()-proof crash of the shared
// runner). Without the depth counter, a few hundred-thousand parens — well within
// the 1MiB request-body cap — would kill the process.
func TestEvalExprDepthGuard(t *testing.T) {
	// Comfortably within the cap: must evaluate fine.
	shallow := strings.Repeat("(", 30) + "1" + strings.Repeat(")", 30)
	if _, err := evalExpr(shallow, nil, nil); err != nil {
		t.Fatalf("shallow nesting should evaluate, got %v", err)
	}

	// Past the cap via nested parens: must return a normal error, NOT crash.
	deep := strings.Repeat("(", 5000) + "1" + strings.Repeat(")", 5000)
	_, err := evalExpr(deep, nil, nil)
	if err == nil {
		t.Fatal("deeply-nested expression should be rejected, got nil error")
	}
	if !strings.Contains(err.Error(), "deeply nested") {
		t.Errorf("expected a depth error, got %v", err)
	}

	// Past the cap via chained unary minus: parseUnary recurses on itself and must
	// also be depth-counted (otherwise -----…1 overflows the stack uncaught).
	unary := strings.Repeat("-", 5000) + "1"
	if _, err := evalExpr(unary, nil, nil); err == nil || !strings.Contains(err.Error(), "deeply nested") {
		t.Errorf("chained unary minus should hit the depth guard, got %v", err)
	}
}
