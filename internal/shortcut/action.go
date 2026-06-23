package shortcut

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// ZipAction renders an action project as a downloadable .zip archive.
func ZipAction(a Action) ([]byte, error) {
	files, err := a.files()
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, rel := range sortedKeys(files) {
		fw, err := zw.Create(rel)
		if err != nil {
			return nil, err
		}
		if _, err := fw.Write([]byte(files[rel])); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// RenderActionRegisterTS exposes the rendered src/register.ts (auditable source)
// for callers that want to show it (e.g. the web platform).
func RenderActionRegisterTS(a Action) (string, error) { return RenderActionTS(a) }

// ScaffoldAction writes a full action project under dir.
func ScaffoldAction(a Action, dir string) error {
	files, err := a.files()
	if err != nil {
		return err
	}
	for _, rel := range sortedKeys(files) {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(p, []byte(files[rel]), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// Action models a Feishu Bitable "自动化操作" (automation action) — the other
// basekit server-side extension (addAction). Verified against the real SDK
// (block-basekit-server-api 1.0.6) by tsc + testAction:
//   - formItems use the Component enum + itemId (NOT the field's key/props/validator)
//   - execute returns a PLAIN object (not {code, data}); args keyed by itemId
//   - resultType is { type:'object', properties: { <key>: { label, type:'number'|'string'|... } } }
//   - the SDK's testAction entry must be src/register.ts; layout needs app.json
//     (parent) + block.json + config.json
//
// v1 scope: config inputs (Component.Input), one outbound request (GET/POST+body),
// expr-mapped plain-object result. Auth is deferred (action auth shape differs
// from fields and is under-documented) — left for a verified follow-up.
type Action struct {
	ID      string         `json:"id"`
	Title   I18n           `json:"title"`
	Domains []string       `json:"domains"`
	Auth    *ActionAuth    `json:"auth,omitempty"` // optional credential the user supplies at config time
	Inputs  []ActionInput  `json:"inputs"`
	Result  []ActionOutput `json:"result"`
	Execute Execute        `json:"execute"`
}

// ActionAuth maps to the SDK's Action.authorization. Unlike fields (which expose
// 8 typed authorizations injected via fetch(authId)), actions declare a single
// authorization of type APIKey or OAuth2; the Feishu FaaS runtime injects the
// user-supplied credential into outbound requests. Declarable + compile-verified
// here; runtime injection (as for field auth) only happens in the real tenant.
type ActionAuth struct {
	Type        string `json:"type"`                  // APIKey | OAuth2
	Label       string `json:"label"`                 // what credential the user enters
	Placeholder string `json:"placeholder,omitempty"` // optional input placeholder
}

// ValidActionAuthTypes are the action authorization types the SDK accepts.
var ValidActionAuthTypes = []string{"APIKey", "OAuth2"}

// ActionInput is one config input the user fills when setting up the automation.
type ActionInput struct {
	Key      string `json:"key"`   // itemId; referenced in expressions as in.<key>
	Label    string `json:"label"` // shown to the user (action labels are plain strings)
	Required bool   `json:"required,omitempty"`
}

// ActionOutput is one field of the action's result object (consumed by next steps).
type ActionOutput struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Type  string `json:"type"` // FieldType enum name (mapped to the SDK's string prop type)
	Expr  string `json:"expr"` // value expression over in.<inputKey> and res.<json.path>
}

// actionPropType maps a FieldType enum name to the action resultType's string type.
func actionPropType(fieldType string) string {
	switch fieldType {
	case "Number":
		return "number"
	case "Checkbox":
		return "boolean"
	default:
		return "string"
	}
}

// Validate gates an action before any TS is rendered.
func (a Action) Validate() error {
	var errs []error
	if !idRe.MatchString(a.ID) {
		errs = append(errs, fmt.Errorf("id %q invalid (want ^[a-z0-9][a-z0-9-]{0,63}$)", a.ID))
	}
	if a.Title.empty() {
		errs = append(errs, errors.New("title.zh_CN is required"))
	}
	if len(a.Domains) == 0 {
		errs = append(errs, errors.New("domains must not be empty"))
	}
	for _, d := range a.Domains {
		if !domainRe.MatchString(d) {
			errs = append(errs, fmt.Errorf("domain %q invalid", d))
		}
	}
	if a.Auth != nil {
		if !slices.Contains(ValidActionAuthTypes, a.Auth.Type) {
			errs = append(errs, fmt.Errorf("auth.type %q invalid (want %s)", a.Auth.Type, strings.Join(ValidActionAuthTypes, ", ")))
		}
		if strings.TrimSpace(a.Auth.Label) == "" {
			errs = append(errs, errors.New("auth.label is required"))
		}
	}
	if len(a.Inputs) == 0 {
		errs = append(errs, errors.New("at least one input is required"))
	}
	inputKeys := map[string]bool{}
	for i, in := range a.Inputs {
		if !keyRe.MatchString(in.Key) {
			errs = append(errs, fmt.Errorf("inputs[%d].key %q invalid", i, in.Key))
		}
		inputKeys[in.Key] = true
		if strings.TrimSpace(in.Label) == "" {
			errs = append(errs, fmt.Errorf("inputs[%d].label is required", i))
		}
	}
	if len(a.Result) == 0 {
		errs = append(errs, errors.New("at least one result property is required"))
	}
	for i, p := range a.Result {
		if !keyRe.MatchString(p.Key) {
			errs = append(errs, fmt.Errorf("result[%d].key %q invalid", i, p.Key))
		}
		if !slices.Contains(ValidFieldTypes, p.Type) {
			errs = append(errs, fmt.Errorf("result[%d].type %q invalid", i, p.Type))
		}
		if strings.TrimSpace(p.Label) == "" {
			errs = append(errs, fmt.Errorf("result[%d].label is required", i))
		}
		if err := validateExpr(p.Expr, inputKeys); err != nil {
			errs = append(errs, fmt.Errorf("result[%d].expr: %w", i, err))
		}
	}
	if strings.TrimSpace(a.Execute.URL) == "" {
		errs = append(errs, errors.New("execute.url is required"))
	} else if err := validateURLTemplate(a.Execute.URL, a.Domains, inputKeys); err != nil {
		errs = append(errs, fmt.Errorf("execute.url: %w", err))
	}
	if !slices.Contains(ValidMethods, a.Execute.Method) {
		errs = append(errs, fmt.Errorf("execute.method %q invalid (want %s)", a.Execute.Method, strings.Join(ValidMethods, ", ")))
	}
	if len(a.Execute.Body) > 0 {
		if !methodHasBody(a.Execute.Method) {
			errs = append(errs, errors.New("execute.body is only valid for POST/PUT/PATCH"))
		}
		for k, v := range a.Execute.Body {
			if !keyRe.MatchString(k) {
				errs = append(errs, fmt.Errorf("execute.body key %q invalid", k))
			}
			if ref := bodyRef(v); ref != "" && !inputKeys[ref] {
				errs = append(errs, fmt.Errorf("execute.body[%s] references unknown input %q", k, ref))
			}
		}
	}
	if len(a.Execute.BodyJSON) > 0 {
		if !methodHasBody(a.Execute.Method) {
			errs = append(errs, errors.New("execute.bodyJson is only valid for POST/PUT/PATCH"))
		}
		if len(a.Execute.Body) > 0 {
			errs = append(errs, errors.New("set either execute.body or execute.bodyJson, not both"))
		}
		if err := validateBodyJSON(a.Execute.BodyJSON, inputKeys); err != nil {
			errs = append(errs, fmt.Errorf("execute.bodyJson: %w", err))
		}
	}
	for k, v := range a.Execute.Headers {
		if !paramNameRe.MatchString(k) {
			errs = append(errs, fmt.Errorf("execute.headers key %q invalid (header-name chars only)", k))
		}
		if ref := bodyRef(v); ref != "" && !inputKeys[ref] {
			errs = append(errs, fmt.Errorf("execute.headers[%s] references unknown input %q", k, ref))
		}
	}
	return errors.Join(errs...)
}

// translateActionExpr is translateExpr but binding inputs to `args` (the action
// execute parameter) instead of `inp`.
func translateActionExpr(expr string) string {
	return strings.ReplaceAll(translateExpr(expr), "inp.", "args.")
}

// RenderActionTS compiles an Action into basekit src/register.ts source.
func RenderActionTS(a Action) (string, error) {
	if err := a.Validate(); err != nil {
		return "", err
	}
	var b strings.Builder
	w := func(format string, args ...any) { fmt.Fprintf(&b, format, args...) }

	w("// Generated by feishu-plugin-platform action generator. Human-auditable.\n")
	w("// Title: %s\n", a.Title.ZhCN)
	w("import { basekit, Component } from '@lark-opdev/block-basekit-server-api';\n\n")

	quoted := make([]string, len(a.Domains))
	for i, d := range a.Domains {
		quoted[i] = jsStr(d)
	}
	w("// 出网域名白名单(只能调用一次)\n")
	w("basekit.addDomainList([%s]);\n\n", strings.Join(quoted, ", "))

	w("basekit.addAction({\n")
	if a.Auth != nil {
		w("  authorization: {\n")
		w("    type: '%s',\n", a.Auth.Type)
		w("    formItem: {\n      label: %s,\n", jsStr(a.Auth.Label))
		if a.Auth.Placeholder != "" {
			w("      componentProps: { placeholder: %s },\n", jsStr(a.Auth.Placeholder))
		}
		w("    },\n  },\n")
	}
	w("  formItems: [\n")
	for _, in := range a.Inputs {
		w("    { itemId: %s, label: %s, required: %t, component: Component.Input },\n", jsStr(in.Key), jsStr(in.Label), in.Required)
	}
	w("  ],\n")

	w("  execute: async (args: Record<string, any>, context) => {\n")
	// Pre-render output values; emit only the expression helpers they use.
	actVals := make([]string, len(a.Result))
	for i, p := range a.Result {
		actVals[i] = translateActionExpr(p.Expr)
	}
	emitExprHelpers(&b, "    ", actVals)
	b.WriteString(actionHelpers)
	w("    try {\n")
	// Reuse the shared fetch-init renderer (method + headers + flat/structured body),
	// then rebind input refs from `inp.` to `args.` (the action execute param).
	initObj := strings.ReplaceAll(renderFetchInit(a.Execute), "inp.", "args.")
	// URL placeholders bind to `args` (the action execute param), not `inp`.
	actionURL := strings.ReplaceAll(renderURLTemplate(a.Execute.URL), "${inp.", "${args.")
	w("      const res: any = await fetch(`%s`, %s);\n", actionURL, initObj)
	w("      return {\n")
	for i, p := range a.Result {
		w("        %s: %s,\n", p.Key, actVals[i])
	}
	w("      };\n")
	w("    } catch (e) {\n")
	w("      debugLog({ '===error': String(e) });\n")
	w("      return {};\n")
	w("    }\n")
	w("  },\n")

	w("  resultType: {\n    type: 'object',\n    properties: {\n")
	for _, p := range a.Result {
		w("      %s: { label: %s, type: '%s' },\n", p.Key, jsStr(p.Label), actionPropType(p.Type))
	}
	w("    },\n  },\n")

	w("});\nexport default basekit;\n")
	return b.String(), nil
}

const actionHelpers = `    function debugLog(arg: any) {
      console.log(JSON.stringify({ arg }), '\n');
    }
    const fetch = async (url: string, init?: any): Promise<any> => {
      const r = await context.fetch(url, init);
      const text = await r.text();
      return JSON.parse(text);
    };
`

// ScaffoldAction writes a buildable + testAction-able basekit action project.
// Note: the basekit toolchain (CLI 1.0.5) packs fields (pack:field) but has no
// pack:action; this scaffold targets testAction verification + manual upload.
// app.json (with appId) is supplied by opdev in the PARENT directory.
func (a Action) files() (map[string]string, error) {
	src, err := RenderActionTS(a)
	if err != nil {
		return nil, err
	}
	// Test inputs: every input gets a sample string value.
	testArgs := make([]string, len(a.Inputs))
	for i, in := range a.Inputs {
		testArgs[i] = fmt.Sprintf("    %s: \"100\",", in.Key)
	}
	test := fmt.Sprintf(`import { testAction, createActionContext } from "@lark-opdev/block-basekit-server-api";
async function run() {
  const context = await createActionContext();
  await testAction({
%s
  }, context as any);
}
run();
`, strings.Join(testArgs, "\n"))

	return map[string]string{
		"package.json":     RenderActionPackageJSON(a),
		"tsconfig.json":    tsconfigJSON,
		"config.json":      "{\n  \"authorizations\": []\n}\n",
		"block.json":       fmt.Sprintf("{\n  \"projectName\": %s,\n  \"blockTypeID\": \"REPLACE_WITH_ACTION_BLOCK_TYPE_ID\"\n}\n", jsonStr(a.ID)),
		"src/register.ts":  src, // SDK testAction entry
		"src/index.ts":     src, // mirror, for tooling that expects index
		"test/index.ts":    test,
		"README.md":        fmt.Sprintf("# %s\n\nGenerated automation action (basekit addAction), human-auditable source.\nEntry: src/register.ts. Needs app.json (with appId) in the parent directory.\n", a.Title.ZhCN),
	}, nil
}

// RenderActionPackageJSON builds package.json for a generated action project.
func RenderActionPackageJSON(a Action) string {
	return fmt.Sprintf(`{
  "name": %s,
  "version": "1.0.0",
  "main": "index.js",
  "scripts": {
    "test": "block-basekit-cli test"
  },
  "license": "ISC",
  "devDependencies": {
    "@lark-opdev/block-basekit-cli": "%s"
  },
  "dependencies": {
    "@lark-opdev/block-basekit-server-api": "%s"
  }
}
`, jsonStr(a.ID), cliVersion, serverAPIVersion)
}
