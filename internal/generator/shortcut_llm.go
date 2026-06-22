package generator

import (
	"context"
	"encoding/json"
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
		"(2) `result.kind` is \"object\"; each property has key (ascii), type, an optional 3-locale label, and `expr` (its value). " +
		"(3) `expr` uses ONLY this grammar — no other code: atoms = a number, 'single-quoted string', rand(), in.<formItemKey>, res.<dotted.json.path>; operators + - * / % ( ) , ; and these functions: concat(a,b,…), upper(s), lower(s), trim(s), substr(s,start,len), slice(s,a,b), replace(s,from,to), len(s), urlencode(s), round(n,d). Examples: `in.amount * res.rates.USD` · `trim(in.text)` · `substr(in.idcard, 6, 8)` · `concat(in.last, in.first)` · `upper(in.code)`. " +
		"Examples: `in.account * res.rates.USD` , `res.data.title` , `rand()`. " +
		"(4) Include one hidden Text property with isGroupByKey via groupByKey=true and expr `rand()` as a stable row id. " +
		"(5) execute.url may contain {formItemKey} placeholders. For a POST API, set method=POST and execute.body (JSON sent as application/json); a body value of exactly \"{formKey}\" injects that input, anything else is a literal. " +
		"(5b) NO-FETCH plugins (omit `execute` and `domains` entirely): two sub-cases. " +
		"(i) `template` is ONLY for inserting inputs VERBATIM into literal text — URL construction (QR/chart/image generators like api.qrserver.com) or raw concatenation, e.g. template \"https://api.qrserver.com/v1/create-qr-code/?data={text}\". A template does NOT transform its inputs. " +
		"(ii) For any TRANSFORMATION of the input — uppercase, lowercase, trim, substring, replace, length, etc. — DO NOT use a template; use `expr` with the matching function, e.g. expr upper(in.text) / trim(in.text) / substr(in.idcard, 6, 8) / concat(in.last, in.first). Never invent res.<path> for a no-fetch plugin. " +
		"(6) AUTH — default to NONE. Most public APIs (ip-api.com, exchangerate-api.com, api.mymemory.translated.net, open-data APIs) need NO key: you MUST omit `auth` entirely for them — adding auth to a keyless API BREAKS the plugin. Add `auth` ONLY when the API genuinely requires a credential the user must obtain; then pick the type (the END-USER enters it — never hardcode, never put the token in execute.url): QueryParamToken+paramName (key in URL query, e.g. appid); HeaderBearerToken (Authorization: Bearer); CustomHeaderToken+paramName=header name (e.g. X-API-Key); Basic (username+password). When unsure, omit auth. " +
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
			"expr":       d(str, "value EXPRESSION (use when there is a fetch): number | rand() | in.<formKey> | res.<json.path>, with + - * / ( ). Give either expr OR template, not both."),
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
						"description":          "POST only: JSON body, field→value. A value of exactly \"{formKey}\" injects that input; anything else is a literal string.",
						"additionalProperties": map[string]any{"type": "string"},
					},
				},
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
	f, err := generateShortcutViaChat(ctx, httpDeepSeek{apiKey: key, baseURL: base, http: client}, model, prompt)
	if err != nil {
		log.Printf("deepseek shortcut generation failed: %v", err)
		return shortcut.FieldShortcut{}, false, err
	}
	return f, true, nil
}

// generateShortcutViaChat is the forced-call + validate + auto-repair loop for
// field shortcuts. Pure logic — unit-tested with a fake chatAPI.
func generateShortcutViaChat(ctx context.Context, api chatAPI, model, prompt string) (shortcut.FieldShortcut, error) {
	tools := []oaTool{{
		Type: "function",
		Function: oaFunctionDef{
			Name:        emitShortcutTool,
			Description: "Emit the Feishu Bitable field-shortcut definition for the user's request.",
			Parameters:  fieldShortcutSchema(),
		},
	}}
	messages := []oaMessage{
		{Role: "system", Content: shortcutSystemPrompt},
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
		} else {
			return f, nil // success
		}

		messages = append(messages, msg, oaMessage{
			Role:       "tool",
			ToolCallID: call.ID,
			Content:    problem + ". Fix it and call " + emitShortcutTool + " again.",
		})
	}
	return shortcut.FieldShortcut{}, fmt.Errorf("exhausted %d repair rounds without a valid field shortcut", shortcutMaxRepairs)
}
