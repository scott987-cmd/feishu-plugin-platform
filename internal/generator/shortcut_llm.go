package generator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/dushibing/feishu-plugin-platform/internal/shortcut"
)

// NL → field-shortcut generation. Reuses the OpenAI-compatible chat plumbing
// (oaRequest/chatAPI/httpDeepSeek) from deepseek.go: the model is forced to call
// one function whose JSON-schema parameters ARE the field-shortcut DSL schema —
// so it can only emit a structured, validatable definition, never code. The
// emitted DSL is then compiled to an auditable basekit TS project elsewhere.

const (
	emitShortcutTool      = "emit_field_shortcut"
	shortcutMaxRepairs    = 2
	shortcutSystemPrompt  = "You generate a Feishu Bitable FIELD SHORTCUT definition (basekit), as JSON. " +
		"You MUST call the " + emitShortcutTool + " tool exactly once, using only fields and enum values allowed by its input schema. " +
		"A field shortcut: takes input field(s) the user picks (formItems, normally component=FieldSelect with supportType), " +
		"calls ONE external HTTP API (execute.url + method), and writes back one or more output columns (result.properties). " +
		"RULES: (1) `domains` MUST list every external host the request hits (e.g. api.exchangerate-api.com); execute.url's host MUST be one of them. " +
		"(2) `result.kind` is \"object\"; each property has key (ascii), type, an optional 3-locale label, and `expr` (its value). Column types: Text/Number/DateTime(ms timestamp)/Checkbox(bool)/SingleSelect plus Phone/Email/Currency/Progress/Rating/Barcode (Currency/Progress/Rating are numbers, Phone/Email/Barcode are strings) and Url (a clickable link — set type=Url and let expr/template produce the URL string; it becomes a `{text,link}` cell automatically) and MultiSelect (a string ARRAY — its expr MUST yield an array: use split(textField, ',') to split a delimited string, or a res.<path> that is already an array; e.g. expr split(in.tags, ',')). IMPORTANT: the `primary` column MUST be Text or Number (SDK rule) — pick a Text/Number field as primary, mark others non-primary. " +
		"(3) `expr` uses ONLY this grammar — no other code: atoms = a number, 'single-quoted string', rand(), in.<formItemKey>, res.<dotted.json.path>; operators + - * / % ( ) , ; and these functions: concat(a,b,…), upper(s), lower(s), trim(s), substr(s,start,len), slice(s,a,b), replace(s,from,to), len(s), urlencode(s), round(n,d), floor(n), ceil(n), abs(n), min(…), max(…). Examples: `in.amount * res.rates.USD` · `trim(in.text)` · `substr(in.idcard, 6, 8)` · `concat(in.last, in.first)` · `upper(in.code)`. " +
		"(3b) CONDITIONALS — there are NO raw comparison operators (you may NOT write <, >, ==, ?, :, &&, ||, !). Use these FUNCTIONS instead: comparison eq(a,b)/ne(a,b)/gt(a,b)/gte(a,b)/lt(a,b)/lte(a,b) (return true/false), boolean and(…)/or(…)/not(x), branching if(cond, thenValue, elseValue), coalesce(a,b,…) (first non-empty), default(v, fallback). Examples: `if(gt(in.score, 90), 'A', 'B')` · `if(eq(substr(in.idcard,16,1) % 2, 1), '男', '女')` (ID-card gender by parity of the 17th digit) · `if(and(gte(in.age,18), lt(in.age,60)), '劳动年龄', '其他')` · `coalesce(res.data.name, '未知')`. " +
		"Examples: `in.account * res.rates.USD` , `res.data.title` , `rand()`. " +
		"(4) Include one hidden Text property with isGroupByKey via groupByKey=true and expr `rand()` as a stable row id. " +
		"(5) execute.url may contain {formItemKey} placeholders. method: GET to read; POST/PUT/PATCH to send a body; DELETE to remove. Flat body → execute.body (field→value, \"{formKey}\" injects input). NESTED/structured body (AI chat completions etc., e.g. messages array) → execute.bodyJson with the full JSON shape, putting \"{formKey}\" where an input goes (e.g. content). Extra request headers (Accept, X-API-Version, etc.) → execute.headers (\"{formKey}\" injects input, else literal); do NOT set Content-Type or the auth header (added automatically). For AI APIs that need a Bearer key, also add auth HeaderBearerToken. " +
		"(5b) NO-FETCH plugins (omit `execute` and `domains` entirely): two sub-cases. " +
		"(i) `template` is ONLY for inserting inputs VERBATIM into literal text — URL construction (QR/chart/image generators like api.qrserver.com) or raw concatenation, e.g. template \"https://api.qrserver.com/v1/create-qr-code/?data={text}\". A template does NOT transform its inputs. " +
		"(ii) For any TRANSFORMATION of the input — uppercase, lowercase, trim, substring, replace, length, etc. — DO NOT use a template; use `expr` with the matching function, e.g. expr upper(in.text) / trim(in.text) / substr(in.idcard, 6, 8) / concat(in.last, in.first). Never invent res.<path> for a no-fetch plugin. " +
		"(6) AUTH — default to NONE. Most public APIs (ip-api.com, exchangerate-api.com, api.mymemory.translated.net, open-data APIs) need NO key: you MUST omit `auth` entirely for them — adding auth to a keyless API BREAKS the plugin. Add `auth` ONLY when the API genuinely requires a credential the user must obtain; then pick the type (the END-USER enters it — never hardcode, never put the token in execute.url): QueryParamToken+paramName (key in URL query, e.g. appid); HeaderBearerToken (Authorization: Bearer); CustomHeaderToken+paramName=header name (e.g. X-API-Key); Basic (username+password). When unsure, omit auth. " +
		"(7) MULTI-STEP (chaining) — if the task needs TWO+ requests where a later call uses an earlier call's response (fetch a token then call an API; look up an id then fetch its detail), use `steps` INSTEAD of `execute`: an ordered array, each step has id/url/method (+ optional headers/body/bodyJson). In a later step reference a prior step's response value with {stepId.json.path} and inputs with {inputKey} (e.g. header Authorization=\"Bearer {auth.access_token}\"). Result exprs map the LAST step's response via res.<path>. Max 3 steps; never set both execute and steps; no auth with steps (make step 1 fetch the credential). " +
		"Reuse names implied by the user's request; pick sensible field types and number formatters. id is a lowercase ascii slug like exchange-rate."
)

// fieldShortcutSchema is the JSON schema the model must satisfy. Built from the
// shortcut package's enums so it auto-syncs with the validator.
func fieldShortcutSchema() map[string]any {
	str := map[string]any{"type": "string"}
	boolean := map[string]any{"type": "boolean"}
	enum := func(vals []string) map[string]any { return map[string]any{"type": "string", "enum": vals} }
	arr := func(items map[string]any) map[string]any { return map[string]any{"type": "array", "items": items} }
	d := func(base map[string]any, desc string) map[string]any {
		out := map[string]any{"description": desc}
		for k, v := range base {
			out[k] = v
		}
		return out
	}

	i18nObj := map[string]any{
		"type":     "object",
		"required": []string{"zh_CN"},
		"properties": map[string]any{
			"zh_CN": d(str, "Chinese label"),
			"en_US": d(str, "English label (optional)"),
			"ja_JP": d(str, "Japanese label (optional)"),
		},
	}
	formItem := map[string]any{
		"type":     "object",
		"required": []string{"key", "label", "component"},
		"properties": map[string]any{
			"key":         d(str, "stable ascii identifier, e.g. account"),
			"label":       i18nObj,
			"component":   d(enum(shortcut.ValidComponents), "FieldSelect lets the user pick a host column"),
			"supportType": d(arr(enum(shortcut.ValidFieldTypes)), "for FieldSelect: which host field types may be picked"),
			"required":    boolean,
		},
	}
	resultProp := map[string]any{
		"type":     "object",
		"required": []string{"key", "type"},
		"properties": map[string]any{
			"key":        d(str, "ascii output column key"),
			"type":       enum(shortcut.ValidFieldTypes),
			"label":      i18nObj,
			"primary":    d(boolean, "the headline value shown in the cell"),
			"hidden":     boolean,
			"groupByKey": d(boolean, "set true on a hidden Text id column whose expr is rand()"),
			"formatter":  d(enum(shortcut.ValidFormatters), "optional number formatter"),
			"expr":       d(str, "value EXPRESSION: number | 'string' | rand() | in.<formKey> | res.<json.path>, with + - * / % ( ) and functions concat/upper/lower/trim/substr/slice/replace/len/urlencode/round/floor/ceil/abs/min/max plus comparison eq/ne/gt/gte/lt/lte, boolean and/or/not, and if(cond,a,b)/coalesce/default for conditionals (NO raw < > == ? :). Give either expr OR template, not both."),
			"template":   d(str, "value TEMPLATE (use for compute-only / 'the URL is the result' plugins, NO fetch): a literal string with {formKey} placeholders, e.g. https://api.qrserver.com/v1/create-qr-code/?size=200x200&data={text}. References inputs only."),
		},
	}
	return map[string]any{
		"type":     "object",
		"required": []string{"id", "title", "formItems", "result"},
		"properties": map[string]any{
			"id":        d(str, "lowercase ascii slug, e.g. exchange-rate"),
			"title":     i18nObj,
			"domains":   d(arr(str), "every external host the request hits, e.g. api.exchangerate-api.com"),
			"auth": map[string]any{
				"type":        "object",
				"description": "optional; include ONLY if the API needs a key/token (omit for open APIs)",
				"required":    []string{"id", "type", "label", "platform", "instructionsUrl"},
				"properties": map[string]any{
					"id":              d(str, "ascii id referenced by the request, e.g. apiToken"),
					"type":            d(enum(shortcut.ValidAuthTypes), "QueryParamToken (key in URL query) or HeaderBearerToken (Authorization: Bearer header)"),
					"label":           d(str, "what the user enters, e.g. OpenWeatherMap API Key"),
					"platform":        d(str, "platform name, e.g. OpenWeatherMap"),
					"instructionsUrl": d(str, "URL where the user learns how to get the credential"),
					"paramName":       d(str, "QueryParamToken only: the query parameter name, e.g. appid"),
				},
			},
			"formItems": arr(formItem),
			"result": map[string]any{
				"type":     "object",
				"required": []string{"kind", "properties"},
				"properties": map[string]any{
					"kind":       enum(shortcut.ValidResultKinds),
					"properties": arr(resultProp),
				},
			},
			"execute": map[string]any{
				"type":     "object",
				"required": []string{"url", "method"},
				"properties": map[string]any{
					"url":    d(str, "external API URL; host MUST be in domains; may contain {formKey} placeholders"),
					"method": enum(shortcut.ValidMethods),
					"body": map[string]any{
						"type":                 "object",
						"description":          "POST only, FLAT body: field→value. A value of exactly \"{formKey}\" injects that input; anything else is a literal string. For nested bodies use bodyJson instead.",
						"additionalProperties": map[string]any{"type": "string"},
					},
					"bodyJson": map[string]any{
						"type":        "object",
						"description": "POST/PUT/PATCH only, STRUCTURED/NESTED body (use for AI chat APIs etc., e.g. {\"model\":\"deepseek-chat\",\"messages\":[{\"role\":\"user\",\"content\":\"…{text}…\"}]}). Give the full JSON shape; string values may contain {inputKey} placeholders. Use this instead of `body` when the body is not flat.",
					},
					"headers": map[string]any{
						"type":                 "object",
						"description":          "optional extra request headers (e.g. Accept, X-API-Version); value \"{formKey}\" injects an input, else literal. Do NOT set Content-Type or the auth header — those are added automatically.",
						"additionalProperties": map[string]any{"type": "string"},
					},
				},
			},
			"steps": stepSchema(),
		},
	}
}

// stepSchema is the JSON schema for a multi-step pipeline (shared by field +
// action). Plain literals (no closures) so it can live outside the schema funcs.
func stepSchema() map[string]any {
	str := map[string]any{"type": "string"}
	strMap := map[string]any{"type": "object", "additionalProperties": str}
	return map[string]any{
		"type":        "array",
		"description": "OPTIONAL multi-step pipeline (chaining): ordered requests where a LATER step uses an EARLIER step's response. Use INSTEAD of execute (never both). In any later step's url/headers/body, reference a prior step's value with {stepId.json.path} and an input with {inputKey}. Result exprs map the LAST step's response via res.<path>. Max 3 steps; no auth with steps (fetch the credential in step 1).",
		"items": map[string]any{
			"type":     "object",
			"required": []string{"id", "url", "method"},
			"properties": map[string]any{
				"id":       map[string]any{"type": "string", "description": "ascii id; referenced by later steps as {id.path}; the last step is `res`"},
				"url":      map[string]any{"type": "string", "description": "request URL; host MUST be in domains; may embed {inputKey} and {priorStepId.path}"},
				"method":   map[string]any{"type": "string", "enum": shortcut.ValidMethods},
				"headers":  strMap,
				"body":     strMap,
				"bodyJson": map[string]any{"type": "object", "description": "POST/PUT/PATCH nested body; string values may embed {inputKey}/{priorStepId.path}"},
			},
		},
	}
}

// GenerateShortcut turns a natural-language request into a validated
// FieldShortcut via DeepSeek. Returns ok=false when DEEPSEEK_API_KEY is absent
// or the call fails (caller can decide how to handle — there is no deterministic
// fallback for arbitrary shortcuts).
func GenerateShortcut(prompt string) (shortcut.FieldShortcut, bool, error) {
	key := os.Getenv("DEEPSEEK_API_KEY")
	if key == "" {
		return shortcut.FieldShortcut{}, false, nil
	}
	model := os.Getenv("MODEL")
	if model == "" {
		model = deepseekDefaultModel
	}
	base := os.Getenv("DEEPSEEK_BASE_URL")
	if base == "" {
		base = deepseekDefaultBase
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	client := &http.Client{Timeout: 45 * time.Second, Transport: &http.Transport{Proxy: nil}}
	f, err := generateShortcutViaChat(ctx, httpDeepSeek{apiKey: key, baseURL: base, http: client}, model, prompt, newBuildVerifierFromEnv())
	if err != nil {
		log.Printf("deepseek shortcut generation failed: %v", err)
		return shortcut.FieldShortcut{}, false, err
	}
	return f, true, nil
}

// verifyField returns nil when there is no verifier or it could not run; only a
// real compile failure (against the real SDK) is returned, to drive a repair round.
func verifyField(ctx context.Context, v Verifier, f shortcut.FieldShortcut) error {
	if v == nil {
		return nil
	}
	if err := v.VerifyField(ctx, f); err != nil && !errors.Is(err, errVerifyUnavailable) {
		return fmt.Errorf("the DSL is valid but the rendered plugin failed to compile — fix the expr/types/shape: %w", err)
	}
	return nil
}

// generateShortcutViaChat is the forced-call + validate (+ optional build) +
// auto-repair loop for field shortcuts. Pure logic — unit-tested with fakes.
func generateShortcutViaChat(ctx context.Context, api chatAPI, model, prompt string, v Verifier) (shortcut.FieldShortcut, error) {
	tools := []oaTool{{
		Type: "function",
		Function: oaFunctionDef{
			Name:        emitShortcutTool,
			Description: "Emit the Feishu Bitable field-shortcut definition for the user's request.",
			Parameters:  fieldShortcutSchema(),
		},
	}}
	// Ground generation on the 3 most relevant verified exemplars (few-shot).
	system := shortcutSystemPrompt + fewShotBlock(retrieveExemplars(prompt, fieldExemplars, 3))
	messages := []oaMessage{
		{Role: "system", Content: system},
		{Role: "user", Content: prompt},
	}

	for round := 0; round <= shortcutMaxRepairs; round++ {
		resp, err := api.create(ctx, oaRequest{
			Model:      model,
			Messages:   messages,
			Tools:      tools,
			ToolChoice: map[string]any{"type": "function", "function": map[string]any{"name": emitShortcutTool}},
			MaxTokens:  1500,
		})
		if err != nil {
			return shortcut.FieldShortcut{}, err
		}
		if len(resp.Choices) == 0 {
			return shortcut.FieldShortcut{}, fmt.Errorf("deepseek returned no choices")
		}
		msg := resp.Choices[0].Message
		var call *oaToolCall
		for i := range msg.ToolCalls {
			if msg.ToolCalls[i].Function.Name == emitShortcutTool {
				call = &msg.ToolCalls[i]
			}
		}
		if call == nil {
			return shortcut.FieldShortcut{}, fmt.Errorf("model did not call %s", emitShortcutTool)
		}

		var f shortcut.FieldShortcut
		decodeErr := json.Unmarshal([]byte(call.Function.Arguments), &f)
		var problem string
		if decodeErr != nil {
			problem = "tool arguments were not valid FieldShortcut JSON: " + decodeErr.Error()
		} else if verr := f.Validate(); verr != nil {
			problem = "validation failed: " + verr.Error()
		} else if berr := verifyField(ctx, v, f); berr != nil {
			problem = berr.Error()
		} else {
			return f, nil // valid and (if enabled) compiles
		}

		messages = append(messages, msg, oaMessage{
			Role:       "tool",
			ToolCallID: call.ID,
			Content:    problem + ". Fix it and call " + emitShortcutTool + " again.",
		})
	}
	return shortcut.FieldShortcut{}, fmt.Errorf("exhausted %d repair rounds without a valid field shortcut", shortcutMaxRepairs)
}
