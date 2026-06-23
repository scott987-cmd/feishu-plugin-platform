package generator

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/dushibing/feishu-plugin-platform/internal/shortcut"
)

// Test→generation feedback: static Validate() proves the DSL is well-formed, but
// not that the rendered TS actually compiles against the real basekit SDK (wrong
// result-type shape, a string used in arithmetic, etc.). A Verifier closes that
// gap by building the scaffolded project and feeding any compiler error back into
// the SAME repair loop the model already uses for Validate() errors.

// errVerifyUnavailable means the check could not run (no toolchain / not wired);
// the caller accepts the DSL rather than treating it as a generation failure.
var errVerifyUnavailable = errors.New("build verification unavailable")

// Verifier compiles a rendered project against the real SDK. A nil error means it
// built; errVerifyUnavailable means the check was skipped; any other error is a
// real compile failure whose message is suitable to feed back to the model.
type Verifier interface {
	VerifyField(ctx context.Context, f shortcut.FieldShortcut) error
	VerifyAction(ctx context.Context, a shortcut.Action) error
}

// buildVerifier runs `block-basekit-cli build:field` on the scaffolded project.
// It needs a node_modules tree carrying the basekit CLI + server-api; point
// nodeModules at a warm one (e.g. a template project) so each build is fast.
type buildVerifier struct {
	nodeModules string
	timeout     time.Duration
}

// newBuildVerifierFromEnv enables build verification only when VERIFY_BUILD=1 and
// BASEKIT_NODE_MODULES points at a usable tree; otherwise returns nil (off), so
// the default behaviour is unchanged.
func newBuildVerifierFromEnv() Verifier {
	if os.Getenv("VERIFY_BUILD") != "1" {
		return nil
	}
	nm := strings.TrimSpace(os.Getenv("BASEKIT_NODE_MODULES"))
	if nm == "" {
		return nil
	}
	log.Printf("build verification ENABLED (compiles each candidate against the real SDK at %s)", nm)
	return buildVerifier{nodeModules: nm, timeout: 120 * time.Second}
}

func (v buildVerifier) VerifyField(ctx context.Context, f shortcut.FieldShortcut) error {
	dir, err := os.MkdirTemp("", "fppverify-")
	if err != nil {
		return errVerifyUnavailable
	}
	defer os.RemoveAll(dir)
	if err := shortcut.Scaffold(f, dir); err != nil {
		return fmt.Errorf("scaffold: %w", err)
	}
	if err := os.Symlink(v.nodeModules, filepath.Join(dir, "node_modules")); err != nil {
		return errVerifyUnavailable
	}
	return runBasekitBuild(ctx, dir, v.timeout, "build:field")
}

// VerifyAction is intentionally a skip: CLI 1.0.5 ships no build:action, so there
// is no first-class compile step for the action track yet. Returning unavailable
// lets action generation proceed (it still goes through Validate()).
func (v buildVerifier) VerifyAction(ctx context.Context, a shortcut.Action) error {
	return errVerifyUnavailable
}

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// runBasekitBuild runs the CLI build script in dir and maps the outcome to:
// nil (built), errVerifyUnavailable (toolchain missing / timed out — never blame
// the model), or a trimmed compile error otherwise.
func runBasekitBuild(ctx context.Context, dir string, timeout time.Duration, script string) error {
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(cctx, "npx", "block-basekit-cli", script)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if errors.Is(err, exec.ErrNotFound) || cctx.Err() == context.DeadlineExceeded {
		return errVerifyUnavailable
	}
	s := ansiRe.ReplaceAllString(string(out), "")
	hasError := strings.Contains(s, "error TS") ||
		strings.Contains(s, "Compile Error") ||
		strings.Contains(s, "编译发生错误")
	if err == nil && !hasError {
		return nil
	}
	msg := strings.TrimSpace(s)
	if len(msg) > 1200 { // keep the tail — TS errors are listed there
		msg = "…" + msg[len(msg)-1200:]
	}
	return fmt.Errorf("rendered project did not compile against the real SDK:\n%s", msg)
}
