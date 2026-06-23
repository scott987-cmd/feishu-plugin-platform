package shortcut

// This file exposes a few internals to the self-hosted execute runtime
// (internal/execrt), which INTERPRETS a FieldShortcut at runtime rather than
// compiling it to TypeScript. The runtime must reuse the exact same
// allowlist/validation as the compiler so the no-eval / domain-allowlist
// guarantees hold identically on both paths. These are thin, behavior-preserving
// wrappers — no logic lives here.

// ValidateExpr reports whether expr is inside the allowlisted expression grammar
// (numbers, 'string' literals, in.<formKey>, res.<path>, rand(), the allowlisted
// functions, and + - * / % ( ) , — nothing else). The runtime calls this before
// evaluating, so a malformed/unsafe expression is rejected, never evaluated.
func ValidateExpr(expr string, formKeys map[string]bool) error {
	return validateExpr(expr, formKeys)
}

// CheckURLHost verifies the URL template's host is covered by the domains
// allowlist. The runtime enforces this on every outbound request (defense in
// depth alongside the k8s egress allowlist).
func CheckURLHost(u string, domains []string) error { return checkURLHost(u, domains) }

// URLHost extracts the host from a URL template (scheme/path/query/{placeholder}
// stripped); "" if undeterminable.
func URLHost(u string) string { return urlHost(u) }

// ExprFuncNames returns the allowlisted expression function names. The runtime
// evaluator must implement every one of these (a test asserts parity).
func ExprFuncNames() []string {
	out := make([]string, len(exprFuncs))
	copy(out, exprFuncs)
	return out
}
