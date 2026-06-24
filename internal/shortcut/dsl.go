// Package shortcut models a Feishu Bitable "字段捷径" (field shortcut) as a
// constrained, declarative DSL and compiles it into a real, human-auditable
// basekit TypeScript project (@lark-opdev/block-basekit-server-api).
//
// This is the platform's new hard currency (2026-06-22 pivot): the product is a
// natural-language → field-shortcut generator for enterprises that have a Bitable
// + plugin capability but no plugin marketplace and no in-house dev team.
//
// Two runtime targets (see docs/EXECUTE_RUNTIME.md):
//   - Public cloud: emit a standard basekit project for the official
//     opdev/basekit upload+review chain (Feishu's basekit FaaS runs execute).
//   - Private deployment: there is NO Feishu FaaS — internal/execrt INTERPRETS
//     this same DSL at request time on the customer's own k8s. The declarative,
//     allowlisted shape below is exactly what makes that safe to interpret.
//
// The generated source being human-readable + auditable (with provenance) is
// itself the selling point for 信创/政企 customers.
//
// The DSL is the LLM's structured intermediate representation, not a runtime:
// NL → FieldShortcut (this) → src/index.ts → testField → pack.
package shortcut

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// FieldShortcut is one generated field-shortcut plugin.
type FieldShortcut struct {
	ID        string     `json:"id"`                  // project name; opdev/package identity
	Title     I18n       `json:"title"`               // human display name (project meta + readme)
	Domains   []string   `json:"domains"`             // addDomainList outbound allowlist (hard-enforced by SDK)
	Auth      *Auth      `json:"auth,omitempty"`      // optional credential the end-user supplies at config time
	FormItems []FormItem `json:"formItems"`           // shortcut inputs
	Result    Result     `json:"result"`              // output (writeback) shape
	Execute   Execute    `json:"execute"`             // single fetch + map plan (or omit and use Steps)
	Steps     []Step     `json:"steps,omitempty"`     // optional: ordered multi-step pipeline (chaining); mutually exclusive with Execute
	CreatedBy *Creator   `json:"createdBy,omitempty"` // the Feishu user who created this (attribution; carried into the rendered source + dsl.json)
}

// Creator attributes a generated plugin to the Feishu user who created it.
type Creator struct {
	OpenID string `json:"open_id,omitempty"`
	Name   string `json:"name,omitempty"`
}

// attributionComment renders the creator as an auditable source-header line, or "".
func attributionComment(c *Creator) string {
	if c == nil || strings.TrimSpace(c.Name) == "" && strings.TrimSpace(c.OpenID) == "" {
		return ""
	}
	who := strings.TrimSpace(c.Name)
	if who == "" {
		who = c.OpenID
	} else if c.OpenID != "" {
		who = who + " (" + c.OpenID + ")"
	}
	return "// 创建者 / Created by: " + who + "\n"
}

// Auth declares a credential the shortcut's USER fills in (never hardcoded). The
// basekit runtime injects it into outbound requests that pass auth.id to fetch.
// Mapped to the SDK's Authorization types. Phase-0 supports the two that cover
// most APIs: a bearer header and a query-param token (e.g. OpenWeatherMap appid).
type Auth struct {
	ID              string `json:"id"`                  // referenced as fetch(url, init, id)
	Type            string `json:"type"`                // HeaderBearerToken | QueryParamToken
	Label           string `json:"label"`               // shown to the user (what credential to enter)
	Platform        string `json:"platform"`            // which platform the credential is for
	InstructionsURL string `json:"instructionsUrl"`     // where the user learns how to get the credential
	ParamName       string `json:"paramName,omitempty"` // QueryParamToken only: the query key (e.g. appid)
}

// I18n is the minimal three-locale label set basekit ships with.
type I18n struct {
	ZhCN string `json:"zh_CN"`
	EnUS string `json:"en_US,omitempty"`
	JaJP string `json:"ja_JP,omitempty"`
}

func (i I18n) empty() bool { return strings.TrimSpace(i.ZhCN) == "" }

// FormItem is one input the user configures in the cell's shortcut panel.
type FormItem struct {
	Key         string   `json:"key"`
	Label       I18n     `json:"label"`
	Component   string   `json:"component"`             // FieldComponent enum (Phase-0: FieldSelect)
	SupportType []string `json:"supportType,omitempty"` // for FieldSelect: which host field types may be picked
	Required    bool     `json:"required,omitempty"`
}

// Result is the shortcut's output. Phase-0 supports a single multi-property
// Object result (the common case: write several derived columns at once).
type Result struct {
	Kind       string       `json:"kind"`       // "object" (Phase-0)
	Properties []ResultProp `json:"properties"` // for object
}

// ResultProp is one output column of an Object result.
type ResultProp struct {
	Key        string `json:"key"`
	Type       string `json:"type"`                 // FieldType enum: Number | Text | ...
	Label      I18n   `json:"label,omitempty"`      // empty → label rendered as the literal key
	Primary    bool   `json:"primary,omitempty"`    // the headline value shown in the cell
	Hidden     bool   `json:"hidden,omitempty"`     //
	GroupByKey bool   `json:"groupByKey,omitempty"` // isGroupByKey (the stable id column)
	Formatter  string `json:"formatter,omitempty"`  // NumberFormatter enum name (optional)
	// Expr is the value expression, over two namespaces: in.<formKey> (inputs)
	// and res.<json.path> (fetched response), plus + - * / ( ) and rand().
	// Validated/allowlisted at compile time — never arbitrary code.
	Expr string `json:"expr,omitempty"`
	// Template is an alternative to Expr: a literal string with {formKey}
	// placeholders, rendered as a JS template literal. Used for compute-only /
	// "URL is the result" plugins (QR/image/chart-gen, formatting/concatenation)
	// where there is no fetched response to map. References inputs only.
	Template string `json:"template,omitempty"`
}

// Execute is the (single) outbound request the shortcut performs before mapping.
type Execute struct {
	URL    string `json:"url"`    // may contain {formKey} placeholders
	Method string `json:"method"` // GET | POST | PUT | PATCH | DELETE
	// BodyJSON is a structured/nested body (e.g. AI chat: {"model":…,
	// "messages":[{"role":"user","content":"…{text}…"}]}). String values may hold
	// {formKey} placeholders. Use this instead of Body for non-flat bodies.
	// Valid for body-carrying methods (POST/PUT/PATCH).
	BodyJSON json.RawMessage `json:"bodyJson,omitempty"`
	// Body is the flat JSON body: field name → value. A value that is exactly
	// "{formKey}" injects that input; anything else is a literal string. Only
	// valid for body-carrying methods (POST/PUT/PATCH). Sent as application/json.
	Body map[string]string `json:"body,omitempty"`
	// Headers are extra request headers: name → value. A value of exactly
	// "{formKey}" injects that input; anything else is a literal string. The
	// Content-Type for a body and the runtime-injected auth header are added
	// automatically and take precedence — do not set them here.
	Headers map[string]string `json:"headers,omitempty"`
}

// methodHasBody reports whether m is an HTTP method that may carry a request body.
func methodHasBody(m string) bool { return m == "POST" || m == "PUT" || m == "PATCH" }

// Step is one request in a multi-step pipeline (Steps). Its JSON response is
// bound to the variable s_<ID> and is referenceable by LATER steps (in url,
// headers, body) and — for the final step, aliased to `res` — by result exprs.
// Placeholders may be an input {formKey} or a prior step's value {stepID.json.path}.
// Multi-step is for chaining (e.g. fetch token → use it; lookup id → fetch detail);
// it is mutually exclusive with the single Execute, and (Phase-0) does not combine
// with Auth.
type Step struct {
	ID       string            `json:"id"`     // ascii; later refs use {id.path} / s_<id>
	URL      string            `json:"url"`    // may contain {formKey} and {priorStepID.path}
	Method   string            `json:"method"` // GET | POST | PUT | PATCH | DELETE
	Headers  map[string]string `json:"headers,omitempty"`
	Body     map[string]string `json:"body,omitempty"`
	BodyJSON json.RawMessage   `json:"bodyJson,omitempty"`
}

// Allowed enum values, kept explicit so LLM/DSL output can be validated and
// refused before we render any TypeScript.
var (
	ValidComponents = []string{"FieldSelect", "Input", "SingleSelect"}
	// ValidFieldTypes are the SDK FieldType names usable as result column types and
	// (for FieldSelect) pickable input types. Scalar types (Phone/Email/Currency/
	// Progress/Rating/Barcode) carry the same string/number value as Text/Number —
	// only the column semantics differ. Url is special-cased to a {text,link} cell
	// value in the renderer. MultiSelect takes a string[] value — produce one with
	// split(textField, ',') or a res.<path> that is already an array.
	ValidFieldTypes = []string{"Number", "Text", "DateTime", "SingleSelect", "Checkbox", "Phone", "Email", "Currency", "Progress", "Rating", "Barcode", "Url", "MultiSelect"}
	// primaryFieldTypes: the SDK restricts a PRIMARY result column to Text | Number.
	primaryFieldTypes = []string{"Text", "Number"}
	ValidResultKinds  = []string{"object"}
	ValidMethods      = []string{"GET", "POST", "PUT", "PATCH", "DELETE"}
	// Authorization types we render + verify (subset of the SDK's 8 — the ones
	// that cover the vast majority of real APIs).
	ValidAuthTypes = []string{"HeaderBearerToken", "QueryParamToken", "CustomHeaderToken", "Basic"}
	// NumberFormatter enum NAMES exactly as exposed by @lark-opdev/block-basekit-
	// server-api (verified against dist/index.d.ts, 2026-06-23). The renderer emits
	// `NumberFormatter.<NAME>`, so an off-list name compiles to undefined — keep
	// this in lockstep with the SDK enum. (Previously included a non-existent
	// "PERCENT_ROUNDED_2"; the real percentage formatters are PERCENTAGE_ROUNDED
	// and PERCENTAGE.)
	ValidFormatters = []string{
		"INTEGER",
		"DIGITAL_ROUNDED_1", "DIGITAL_ROUNDED_2", "DIGITAL_ROUNDED_3", "DIGITAL_ROUNDED_4",
		"DIGITAL_THOUSANDS", "DIGITAL_THOUSANDS_DECIMALS",
		"PERCENTAGE_ROUNDED", "PERCENTAGE",
	}
)

const (
	MaxFormItems  = 20
	MaxProperties = 20
	MaxDomains    = 20
	MaxStrLen     = 512
	MaxSteps      = 3 // bound the multi-step pipeline (sandbox time/memory)
)

var (
	keyRe       = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	idRe        = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)
	domainRe    = regexp.MustCompile(`^[A-Za-z0-9.-]+$`)
	paramNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*$`) // query key or header name (hyphens ok)
)

// Validate gates a definition before any TS is rendered.
func (f FieldShortcut) Validate() error {
	var errs []error

	if !idRe.MatchString(f.ID) {
		errs = append(errs, fmt.Errorf("id %q invalid (want ^[a-z0-9][a-z0-9-]{0,63}$)", f.ID))
	}
	if f.Title.empty() {
		errs = append(errs, errors.New("title.zh_CN is required"))
	}
	// Two modes: fetch (calls execute.url, may map res.*) vs compute-only (no
	// outbound request — outputs are templates / input-only expressions, e.g.
	// QR/image/chart "URL is the result", or local formatting).
	multiStep := len(f.Steps) > 0
	fetchMode := strings.TrimSpace(f.Execute.URL) != "" || multiStep
	if fetchMode && len(f.Domains) == 0 {
		errs = append(errs, errors.New("domains must not be empty in fetch mode (addDomainList allowlist)"))
	}
	if multiStep && strings.TrimSpace(f.Execute.URL) != "" {
		errs = append(errs, errors.New("set either execute (single request) or steps (multi-step), not both"))
	}
	if multiStep && f.Auth != nil {
		errs = append(errs, errors.New("auth is not supported with steps yet — for chained auth, make the first step fetch the token and reference it in a later step"))
	}
	if len(f.Domains) > MaxDomains {
		errs = append(errs, fmt.Errorf("too many domains (%d > %d)", len(f.Domains), MaxDomains))
	}
	for _, d := range f.Domains {
		if !domainRe.MatchString(d) {
			errs = append(errs, fmt.Errorf("domain %q invalid", d))
		}
	}

	if f.Auth != nil {
		a := f.Auth
		if !keyRe.MatchString(a.ID) {
			errs = append(errs, fmt.Errorf("auth.id %q invalid", a.ID))
		}
		if !slices.Contains(ValidAuthTypes, a.Type) {
			errs = append(errs, fmt.Errorf("auth.type %q invalid (want %s)", a.Type, strings.Join(ValidAuthTypes, ", ")))
		}
		if strings.TrimSpace(a.Label) == "" {
			errs = append(errs, errors.New("auth.label is required"))
		}
		if strings.TrimSpace(a.Platform) == "" {
			errs = append(errs, errors.New("auth.platform is required"))
		}
		if strings.TrimSpace(a.InstructionsURL) == "" {
			errs = append(errs, errors.New("auth.instructionsUrl is required"))
		}
		if (a.Type == "QueryParamToken" || a.Type == "CustomHeaderToken") && !paramNameRe.MatchString(a.ParamName) {
			errs = append(errs, fmt.Errorf("auth.paramName %q invalid (required for %s: query key or header name)", a.ParamName, a.Type))
		}
	}

	if len(f.FormItems) == 0 {
		errs = append(errs, errors.New("at least one formItem is required"))
	}
	if len(f.FormItems) > MaxFormItems {
		errs = append(errs, fmt.Errorf("too many formItems (%d > %d)", len(f.FormItems), MaxFormItems))
	}
	formKeys := map[string]bool{}
	for i, it := range f.FormItems {
		if !keyRe.MatchString(it.Key) {
			errs = append(errs, fmt.Errorf("formItems[%d].key %q invalid", i, it.Key))
		}
		formKeys[it.Key] = true
		if it.Label.empty() {
			errs = append(errs, fmt.Errorf("formItems[%d].label.zh_CN is required", i))
		}
		if !slices.Contains(ValidComponents, it.Component) {
			errs = append(errs, fmt.Errorf("formItems[%d].component %q invalid (want %s)", i, it.Component, strings.Join(ValidComponents, ", ")))
		}
		for _, st := range it.SupportType {
			if !slices.Contains(ValidFieldTypes, st) {
				errs = append(errs, fmt.Errorf("formItems[%d].supportType %q invalid", i, st))
			}
		}
	}

	if !slices.Contains(ValidResultKinds, f.Result.Kind) {
		errs = append(errs, fmt.Errorf("result.kind %q invalid (want %s)", f.Result.Kind, strings.Join(ValidResultKinds, ", ")))
	}
	if len(f.Result.Properties) == 0 {
		errs = append(errs, errors.New("result.properties must not be empty"))
	}
	if len(f.Result.Properties) > MaxProperties {
		errs = append(errs, fmt.Errorf("too many properties (%d > %d)", len(f.Result.Properties), MaxProperties))
	}
	for i, p := range f.Result.Properties {
		if !keyRe.MatchString(p.Key) {
			errs = append(errs, fmt.Errorf("result.properties[%d].key %q invalid", i, p.Key))
		}
		if !slices.Contains(ValidFieldTypes, p.Type) {
			errs = append(errs, fmt.Errorf("result.properties[%d].type %q invalid", i, p.Type))
		}
		if p.Primary && !slices.Contains(primaryFieldTypes, p.Type) {
			errs = append(errs, fmt.Errorf("result.properties[%d]: a primary column must be Text or Number (SDK constraint), got %q", i, p.Type))
		}
		if p.Formatter != "" && !slices.Contains(ValidFormatters, p.Formatter) {
			errs = append(errs, fmt.Errorf("result.properties[%d].formatter %q invalid", i, p.Formatter))
		}
		// Each property is either a template (string with {key} placeholders) or
		// an expression. res.* is only valid in fetch mode.
		// Length-cap expr/template: the evaluator is a recursive-descent parser, so
		// an unbounded expression can overflow the goroutine stack (a fatal,
		// recover()-proof crash of the shared runner). Capping length bounds the
		// reachable nesting depth on BOTH the api validation path and the runtime.
		if len(p.Template) > MaxStrLen {
			errs = append(errs, fmt.Errorf("result.properties[%d].template too long (%d > %d)", i, len(p.Template), MaxStrLen))
		}
		if len(p.Expr) > MaxStrLen {
			errs = append(errs, fmt.Errorf("result.properties[%d].expr too long (%d > %d)", i, len(p.Expr), MaxStrLen))
		}
		hasTpl := strings.TrimSpace(p.Template) != ""
		hasExpr := strings.TrimSpace(p.Expr) != ""
		switch {
		case hasTpl && hasExpr:
			errs = append(errs, fmt.Errorf("result.properties[%d]: set either expr or template, not both", i))
		case hasTpl:
			if err := validatePlaceholders(p.Template, formKeys); err != nil {
				errs = append(errs, fmt.Errorf("result.properties[%d].template: %w", i, err))
			}
		case hasExpr:
			if err := validateExprMode(p.Expr, formKeys, fetchMode); err != nil {
				errs = append(errs, fmt.Errorf("result.properties[%d].expr: %w", i, err))
			}
		default:
			errs = append(errs, fmt.Errorf("result.properties[%d]: needs an expr or a template", i))
		}
	}

	// Single execute: present = fetch mode (validate it); absent = compute-only.
	// In multi-step mode the single Execute is unused (steps are validated below).
	if fetchMode && !multiStep {
		if err := validateURLTemplate(f.Execute.URL, f.Domains, formKeys); err != nil {
			errs = append(errs, fmt.Errorf("execute.url: %w", err))
		}
		if !slices.Contains(ValidMethods, f.Execute.Method) {
			errs = append(errs, fmt.Errorf("execute.method %q invalid (want %s)", f.Execute.Method, strings.Join(ValidMethods, ", ")))
		}
	}
	if len(f.Execute.Body) > 0 {
		if !methodHasBody(f.Execute.Method) {
			errs = append(errs, errors.New("execute.body is only valid for POST/PUT/PATCH"))
		}
		for k, v := range f.Execute.Body {
			if !keyRe.MatchString(k) {
				errs = append(errs, fmt.Errorf("execute.body key %q invalid", k))
			}
			if len(v) > MaxStrLen {
				errs = append(errs, fmt.Errorf("execute.body[%s] too long", k))
			}
			// A value of the exact form "{formKey}" injects that input; check it exists.
			if ref := bodyRef(v); ref != "" && !formKeys[ref] {
				errs = append(errs, fmt.Errorf("execute.body[%s] references unknown form item %q", k, ref))
			}
		}
	}
	if len(f.Execute.BodyJSON) > 0 {
		if !methodHasBody(f.Execute.Method) {
			errs = append(errs, errors.New("execute.bodyJson is only valid for POST/PUT/PATCH"))
		}
		if len(f.Execute.Body) > 0 {
			errs = append(errs, errors.New("set either execute.body or execute.bodyJson, not both"))
		}
		if err := validateBodyJSON(f.Execute.BodyJSON, formKeys); err != nil {
			errs = append(errs, fmt.Errorf("execute.bodyJson: %w", err))
		}
	}
	for k, v := range f.Execute.Headers {
		if !paramNameRe.MatchString(k) {
			errs = append(errs, fmt.Errorf("execute.headers key %q invalid (header-name chars only)", k))
		}
		if len(v) > MaxStrLen {
			errs = append(errs, fmt.Errorf("execute.headers[%s] too long", k))
		}
		if ref := bodyRef(v); ref != "" && !formKeys[ref] {
			errs = append(errs, fmt.Errorf("execute.headers[%s] references unknown form item %q", k, ref))
		}
	}
	if multiStep {
		errs = append(errs, validateSteps(f.Steps, formKeys, f.Domains))
	}

	return errors.Join(errs...)
}
