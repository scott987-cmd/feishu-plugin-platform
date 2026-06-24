package execrt

import (
	"fmt"
	"math"
	"math/big"
	"math/rand"
	"strconv"
	"strings"
)

// evalExpr evaluates an allowlisted result expression over two namespaces:
//
//	in.<formKey>  -> inputs[...]      res.<json.path> -> res[...]
//
// It implements the SAME tiny grammar shortcut/expr.go validates and the SAME
// helper functions shortcut emits as JS (see exprHelperDefs) — but in Go, by
// interpretation. The caller MUST have run shortcut.ValidateExpr first; this
// evaluator is still defensive (errors instead of panicking on bad shapes).
//
// Grammar:
//
//	expr   = addsub
//	addsub = muldiv (('+'|'-') muldiv)*
//	muldiv = unary  (('*'|'/'|'%') unary)*
//	unary  = '-'? primary
//	primary= number | 'string' | rand() | func '(' args? ')' | in.<p> | res.<p> | '(' expr ')'
func evalExpr(expr string, inputs map[string]any, res any) (any, error) {
	toks, err := lexExpr(expr)
	if err != nil {
		return nil, err
	}
	ev := &evaluator{toks: toks, inputs: inputs, res: res}
	v, err := ev.parseAddSub()
	if err != nil {
		return nil, err
	}
	if ev.cur().kind != tkEOF {
		return nil, fmt.Errorf("unexpected token %q", ev.cur().text)
	}
	return v, nil
}

type tokKind int

const (
	tkNum tokKind = iota
	tkStr
	tkIdent
	tkOp
	tkEOF
)

type token struct {
	kind tokKind
	text string
}

func lexExpr(s string) ([]token, error) {
	var out []token
	i := 0
	for i < len(s) {
		c := s[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		case c == '\'':
			j := i + 1
			for j < len(s) && s[j] != '\'' {
				j++
			}
			if j >= len(s) {
				return nil, fmt.Errorf("unterminated string literal")
			}
			out = append(out, token{tkStr, s[i+1 : j]})
			i = j + 1
		case c >= '0' && c <= '9':
			j := i
			for j < len(s) && (s[j] >= '0' && s[j] <= '9' || s[j] == '.') {
				j++
			}
			out = append(out, token{tkNum, s[i:j]})
			i = j
		case c == '_' || c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z':
			j := i
			for j < len(s) && (s[j] == '_' || s[j] == '.' || s[j] >= '0' && s[j] <= '9' || s[j] >= 'A' && s[j] <= 'Z' || s[j] >= 'a' && s[j] <= 'z') {
				j++
			}
			out = append(out, token{tkIdent, s[i:j]})
			i = j
		case strings.IndexByte("+-*/%(),", c) >= 0:
			out = append(out, token{tkOp, string(c)})
			i++
		default:
			return nil, fmt.Errorf("illegal character %q in expression", string(c))
		}
	}
	out = append(out, token{tkEOF, ""})
	return out, nil
}

type evaluator struct {
	toks   []token
	pos    int
	depth  int
	inputs map[string]any
	res    any
}

// maxExprDepth bounds parenthesis/operator nesting. The parser is recursive
// descent (4 frames per nesting level), so without this a deeply-nested
// expression overflows the goroutine stack — a FATAL runtime error that
// recover() CANNOT catch, crashing the whole shared runner. shortcut.Validate
// also length-caps Expr/Template so a 1MiB body can't reach a huge depth.
const maxExprDepth = 64

func (e *evaluator) cur() token { return e.toks[e.pos] }
func (e *evaluator) eat() token { t := e.toks[e.pos]; e.pos++; return t }

func (e *evaluator) parseAddSub() (any, error) {
	if e.depth++; e.depth > maxExprDepth {
		return nil, fmt.Errorf("expression too deeply nested (>%d)", maxExprDepth)
	}
	defer func() { e.depth-- }()
	left, err := e.parseMulDiv()
	if err != nil {
		return nil, err
	}
	for e.cur().kind == tkOp && (e.cur().text == "+" || e.cur().text == "-") {
		op := e.eat().text
		right, err := e.parseMulDiv()
		if err != nil {
			return nil, err
		}
		if op == "+" {
			// JS: a string operand makes `+` concatenate; else numeric add.
			if isNumberLike(left) && isNumberLike(right) {
				l, _ := toNum(left)
				r, _ := toNum(right)
				left = l + r
			} else {
				left = toStr(left) + toStr(right)
			}
		} else {
			l, _ := toNum(left)
			r, _ := toNum(right)
			left = l - r
		}
	}
	return left, nil
}

func (e *evaluator) parseMulDiv() (any, error) {
	left, err := e.parseUnary()
	if err != nil {
		return nil, err
	}
	for e.cur().kind == tkOp && (e.cur().text == "*" || e.cur().text == "/" || e.cur().text == "%") {
		op := e.eat().text
		right, err := e.parseUnary()
		if err != nil {
			return nil, err
		}
		l, _ := toNum(left)
		r, _ := toNum(right)
		switch op {
		case "*":
			left = l * r
		case "/":
			left = l / r
		case "%":
			left = math.Mod(l, r)
		}
	}
	return left, nil
}

func (e *evaluator) parseUnary() (any, error) {
	if e.cur().kind == tkOp && e.cur().text == "-" {
		// Chained unary minus (-----x) recurses on parseUnary itself, bypassing the
		// parseAddSub depth counter — count it here too so it can't overflow.
		if e.depth++; e.depth > maxExprDepth {
			return nil, fmt.Errorf("expression too deeply nested (>%d)", maxExprDepth)
		}
		defer func() { e.depth-- }()
		e.eat()
		v, err := e.parseUnary()
		if err != nil {
			return nil, err
		}
		n, _ := toNum(v)
		return -n, nil
	}
	return e.parsePrimary()
}

func (e *evaluator) parsePrimary() (any, error) {
	t := e.cur()
	switch t.kind {
	case tkNum:
		e.eat()
		f, err := strconv.ParseFloat(t.text, 64)
		if err != nil {
			return nil, fmt.Errorf("bad number %q", t.text)
		}
		return f, nil
	case tkStr:
		e.eat()
		return t.text, nil
	case tkOp:
		if t.text == "(" {
			e.eat()
			v, err := e.parseAddSub()
			if err != nil {
				return nil, err
			}
			if e.cur().text != ")" {
				return nil, fmt.Errorf("expected ) got %q", e.cur().text)
			}
			e.eat()
			return v, nil
		}
		return nil, fmt.Errorf("unexpected operator %q", t.text)
	case tkIdent:
		e.eat()
		// function call?
		if e.cur().kind == tkOp && e.cur().text == "(" {
			return e.parseCall(t.text)
		}
		return e.resolveRef(t.text)
	default:
		return nil, fmt.Errorf("unexpected end of expression")
	}
}

func (e *evaluator) parseCall(name string) (any, error) {
	e.eat() // (
	var args []any
	if !(e.cur().kind == tkOp && e.cur().text == ")") {
		for {
			a, err := e.parseAddSub()
			if err != nil {
				return nil, err
			}
			args = append(args, a)
			if e.cur().kind == tkOp && e.cur().text == "," {
				e.eat()
				continue
			}
			break
		}
	}
	if e.cur().text != ")" {
		return nil, fmt.Errorf("expected ) closing %s()", name)
	}
	e.eat()
	// rand() is a grammar atom (String(Math.random())), not an allowlisted
	// function — mainly used for the stable _id group-by key.
	if name == "rand" {
		return strconv.FormatFloat(rand.Float64(), 'g', -1, 64), nil
	}
	fn, ok := exprFns[name]
	if !ok {
		return nil, fmt.Errorf("unknown function %q", name)
	}
	return fn(args), nil
}

func (e *evaluator) resolveRef(id string) (any, error) {
	switch {
	case id == "res":
		return e.res, nil
	case strings.HasPrefix(id, "res."):
		return getPath(e.res, strings.TrimPrefix(id, "res.")), nil
	case strings.HasPrefix(id, "in."):
		return getPath(e.inputs, strings.TrimPrefix(id, "in.")), nil
	default:
		return nil, fmt.Errorf("identifier %q not allowed (use in.<key>, res.<path>, a function, or rand())", id)
	}
}

// exprFns implements every allowlisted function from shortcut/exprHelperDefs in
// Go. A test (eval_test.go) asserts this set matches shortcut.ExprFuncNames()
// exactly, so the interpreter can never silently drift from the compiler.
var exprFns = map[string]func([]any) any{
	"concat": func(a []any) any { return concatAll(a) },
	"upper":  func(a []any) any { return strings.ToUpper(toStr(arg(a, 0))) },
	"lower":  func(a []any) any { return strings.ToLower(toStr(arg(a, 0))) },
	"trim":   func(a []any) any { return strings.TrimSpace(toStr(arg(a, 0))) },
	"substr": func(a []any) any {
		s := toStr(arg(a, 0))
		start := int(numArg(a, 1))
		length := int(numArg(a, 2))
		return jsSlice(s, start, start+length)
	},
	"slice": func(a []any) any { return jsSlice(toStr(arg(a, 0)), int(numArg(a, 1)), int(numArg(a, 2))) },
	"replace": func(a []any) any {
		return strings.ReplaceAll(toStr(arg(a, 0)), toStr(arg(a, 1)), toStr(arg(a, 2)))
	},
	"len":       func(a []any) any { return float64(len([]rune(toStr(arg(a, 0))))) },
	"urlencode": func(a []any) any { return encodeURIComponent(toStr(arg(a, 0))) },
	"round": func(a []any) any {
		d := 0
		if len(a) > 1 {
			d = int(numArg(a, 1))
		}
		return roundTo(numArg(a, 0), d)
	},
	"floor": func(a []any) any { return math.Floor(numArg(a, 0)) },
	"ceil":  func(a []any) any { return math.Ceil(numArg(a, 0)) },
	"abs":   func(a []any) any { return math.Abs(numArg(a, 0)) },
	"min":   func(a []any) any { return reduceNums(a, math.Inf(1), math.Min) },
	"max":   func(a []any) any { return reduceNums(a, math.Inf(-1), math.Max) },
	"eq":    func(a []any) any { return toStr(arg(a, 0)) == toStr(arg(a, 1)) },
	"ne":    func(a []any) any { return toStr(arg(a, 0)) != toStr(arg(a, 1)) },
	"gt":    func(a []any) any { return numArg(a, 0) > numArg(a, 1) },
	"gte":   func(a []any) any { return numArg(a, 0) >= numArg(a, 1) },
	"lt":    func(a []any) any { return numArg(a, 0) < numArg(a, 1) },
	"lte":   func(a []any) any { return numArg(a, 0) <= numArg(a, 1) },
	"and":   func(a []any) any { return allTruthy(a) },
	"or":    func(a []any) any { return anyTruthy(a) },
	"not":   func(a []any) any { return !truthy(arg(a, 0)) },
	"if": func(a []any) any {
		if truthy(arg(a, 0)) {
			return arg(a, 1)
		}
		return arg(a, 2)
	},
	"coalesce": func(a []any) any {
		for _, v := range a {
			if !isBlank(v) {
				return v
			}
		}
		return ""
	},
	"default": func(a []any) any {
		if isBlank(arg(a, 0)) {
			return arg(a, 1)
		}
		return arg(a, 0)
	},
	"split": func(a []any) any {
		parts := strings.Split(toStr(arg(a, 0)), toStr(arg(a, 1)))
		out := make([]any, 0, len(parts))
		for _, p := range parts {
			if t := strings.TrimSpace(p); t != "" {
				out = append(out, t)
			}
		}
		return out
	},
}

func arg(a []any, i int) any {
	if i < len(a) {
		return a[i]
	}
	return nil
}
func numArg(a []any, i int) float64 { n, _ := toNum(arg(a, i)); return n }

func concatAll(a []any) string {
	var b strings.Builder
	for _, v := range a {
		b.WriteString(toStr(v))
	}
	return b.String()
}

func reduceNums(a []any, seed float64, f func(x, y float64) float64) float64 {
	acc := seed
	for _, v := range a {
		n, _ := toNum(v)
		acc = f(acc, n)
	}
	return acc
}

func allTruthy(a []any) bool {
	for _, v := range a {
		if !truthy(v) {
			return false
		}
	}
	return true
}
func anyTruthy(a []any) bool {
	for _, v := range a {
		if truthy(v) {
			return true
		}
	}
	return false
}

func isBlank(v any) bool { return v == nil || v == "" }

// roundTo mirrors the compiled JS helper `Number(Number(n).toFixed(d))` so the execrt
// interpreter and the basekit-on-FaaS path write the SAME number for the same DSL.
// toFixed rounds the decimal representation with ties going to the larger magnitude —
// which differs BOTH from math.Round on a scaled float (round(2.675,2)=2.68 there, but
// 2.67 in JS, because 2.675*100 floats up to 267.5000…) AND from strconv's round-half-
// to-even (round(2.5,0)=2 there, but 3 in JS). We emulate toFixed exactly with big.Float:
// round-half-up on |n|·10^d, then restore the sign.
func roundTo(n float64, d int) float64 {
	if d < 0 {
		d = 0
	}
	if math.IsNaN(n) || math.IsInf(n, 0) {
		return n
	}
	const prec = 200
	neg := math.Signbit(n)
	scale := new(big.Float).SetPrec(prec).SetFloat64(math.Pow(10, float64(d)))
	scaled := new(big.Float).SetPrec(prec).SetFloat64(math.Abs(n))
	scaled.Mul(scaled, scale)
	scaled.Add(scaled, big.NewFloat(0.5)) // ties -> larger magnitude (toFixed semantics)
	m, _ := scaled.Int(nil)               // truncate toward zero == floor for non-negative
	res := new(big.Float).SetPrec(prec).SetInt(m)
	res.Quo(res, scale)
	f, _ := res.Float64()
	if neg {
		f = -f
	}
	return f
}

// jsSlice mirrors String.prototype.slice(start, end) including negative indices
// (offset from the end) and clamping.
func jsSlice(s string, start, end int) string {
	r := []rune(s)
	n := len(r)
	norm := func(i int) int {
		if i < 0 {
			i += n
		}
		if i < 0 {
			i = 0
		}
		if i > n {
			i = n
		}
		return i
	}
	a := norm(start)
	b := norm(end)
	if a >= b {
		return ""
	}
	return string(r[a:b])
}

// encodeURIComponent mirrors JS encodeURIComponent: percent-encodes everything
// except the unreserved set A-Za-z0-9 and -_.!~*'() .
func encodeURIComponent(s string) string {
	const keep = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_.!~*'()"
	var b strings.Builder
	for _, by := range []byte(s) {
		if strings.IndexByte(keep, by) >= 0 {
			b.WriteByte(by)
		} else {
			fmt.Fprintf(&b, "%%%02X", by)
		}
	}
	return b.String()
}
