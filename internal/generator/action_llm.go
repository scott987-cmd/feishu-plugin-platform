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

// NL → automation Action generation. Same forced-tool-call + validate + repair
// loop as the field-shortcut path, reusing the OpenAI-compatible plumbing; the
// tool's schema is the Action DSL, so the model can only emit a validatable
// automation definition that compiles to an auditable basekit addAction project.

const (
	emitActionTool     = "emit_action"
	actionSystemPrompt = "You generate a Feishu Bitable AUTOMATION ACTION definition (basekit addAction), as JSON. " +
		"You MUST call the " + emitActionTool + " tool exactly once, using only fields and enum values allowed by its schema. " +
		"An automation action runs when a record event/timer/button fires: it takes config `inputs` the user fills, calls ONE external HTTP API (execute), " +
		"and returns a `result` object whose fields downstream automation steps can consume. " +
		"RULES: (1) `domains` MUST list every external host hit; execute.url's host MUST be one of them. " +
		"(2) each `inputs` item has key (ascii), a plain-string label, and required. (3) each `result` item has key (ascii), a plain-string label, a type, and `expr`. " +
		"(4) `expr` uses ONLY: a number, 'single-quoted string', rand(), in.<inputKey>, res.<dotted.json.path>, with + - * / % ( ) , and functions concat/upper/lower/trim/substr/slice/replace/len/urlencode/round/floor/ceil/abs/min/max. For conditionals use FUNCTIONS (no raw < > == ? :): comparison eq/ne/gt/gte/lt/lte, boolean and/or/not, branching if(cond,a,b)/coalesce/default. Examples: `in.amount * res.rates.USD` · `trim(in.text)` · `if(gt(res.code,400), 'error', 'ok')`. " +
		"(5) method: GET to read; POST/PUT/PATCH to write to an external system (e.g. update a ticket/CRM record); DELETE to remove. Flat body → execute.body (\"{inputKey}\" injects that input, else literal); NESTED body → execute.bodyJson (full JSON shape, \"{inputKey}\" where an input goes); extra headers → execute.headers (do NOT set Content-Type or the auth header). " +
		"(6) If the API needs a key/token, add `auth` { type:'APIKey', label } (the user enters it; the runtime injects it). Omit for open APIs. " +
		"(7) MULTI-STEP — for chained calls (a later request uses an earlier response), use `steps` INSTEAD of `execute`: ordered id/url/method steps; reference a prior step with {stepId.json.path} and inputs with {inputKey}; result exprs map the LAST step's response (res.<path>); max 3; no auth with steps. " +
		"Reuse names from the request; pick sensible result types. id is a lowercase ascii slug."
)

func actionSchema() map[string]any {
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
			"zh_CN": d(str, "Chinese title"),
			"en_US": d(str, "English title (optional)"),
			"ja_JP": d(str, "Japanese title (optional)"),
		},
	}
	input := map[string]any{
		"type":     "object",
		"required": []string{"key", "label"},
		"properties": map[string]any{
			"key":      d(str, "ascii identifier, referenced as in.<key>"),
			"label":    d(str, "what the user enters (plain string)"),
			"required": boolean,
		},
	}
	output := map[string]any{
		"type":     "object",
		"required": []string{"key", "label", "type", "expr"},
		"properties": map[string]any{
			"key":   d(str, "ascii output key"),
			"label": d(str, "plain-string label"),
			"type":  enum(shortcut.ValidFieldTypes),
			"expr":  d(str, "value expression: number | 'string' | rand() | in.<key> | res.<json.path> with + - * / % ( ), functions (concat/upper/.../round/floor/ceil/abs/min/max), and conditionals as functions eq/ne/gt/gte/lt/lte/and/or/not/if/coalesce/default (NO raw < > == ? :)"),
		},
	}
	return map[string]any{
		"type":     "object",
		"required": []string{"id", "title", "domains", "inputs", "result", "execute"},
		"properties": map[string]any{
			"id":      d(str, "lowercase ascii slug"),
			"title":   i18nObj,
			"domains": d(arr(str), "every external host the request hits"),
			"auth": map[string]any{
				"type":        "object",
				"description": "optional; include ONLY if the API needs a key/token",
				"required":    []string{"type", "label"},
				"properties": map[string]any{
					"type":        d(enum(shortcut.ValidActionAuthTypes), "APIKey (key/token) or OAuth2"),
					"label":       d(str, "what credential the user enters"),
					"placeholder": d(str, "optional input placeholder"),
				},
			},
			"inputs": arr(input),
			"result":  arr(output),
			"execute": map[string]any{
				"type":     "object",
				"required": []string{"url", "method"},
				"properties": map[string]any{
					"url":    d(str, "external API URL; host MUST be in domains; may contain {inputKey} placeholders"),
					"method": d(enum(shortcut.ValidMethods), "GET to read; POST/PUT/PATCH to write to an external system; DELETE to remove"),
					"body": map[string]any{
						"type":                 "object",
						"description":          "POST/PUT/PATCH only, FLAT body; value \"{inputKey}\" injects that input, else literal string. For nested bodies use bodyJson.",
						"additionalProperties": map[string]any{"type": "string"},
					},
					"bodyJson": map[string]any{
						"type":        "object",
						"description": "POST/PUT/PATCH only, STRUCTURED/NESTED body (e.g. {\"items\":[{\"id\":\"{recordId}\"}]}); string values may contain {inputKey} placeholders. Use instead of `body` when not flat.",
					},
					"headers": map[string]any{
						"type":                 "object",
						"description":          "optional extra request headers (e.g. Accept, X-API-Version); value \"{inputKey}\" injects an input, else literal. Do NOT set Content-Type or the auth header — those are added automatically.",
						"additionalProperties": map[string]any{"type": "string"},
					},
				},
			},
			"steps": stepSchema(),
		},
	}
}

// GenerateAction turns a natural-language request into a validated Action.
func GenerateAction(prompt string) (shortcut.Action, bool, error) {
	key := os.Getenv("DEEPSEEK_API_KEY")
	if key == "" {
		return shortcut.Action{}, false, nil
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
	a, err := generateActionViaChat(ctx, httpDeepSeek{apiKey: key, baseURL: base, http: client}, model, prompt)
	if err != nil {
		log.Printf("deepseek action generation failed: %v", err)
		return shortcut.Action{}, false, err
	}
	return a, true, nil
}

func generateActionViaChat(ctx context.Context, api chatAPI, model, prompt string) (shortcut.Action, error) {
	tools := []oaTool{{
		Type: "function",
		Function: oaFunctionDef{
			Name:        emitActionTool,
			Description: "Emit the Feishu Bitable automation action definition for the user's request.",
			Parameters:  actionSchema(),
		},
	}}
	// Ground generation on the most relevant verified action exemplar(s).
	system := actionSystemPrompt + fewShotBlock(retrieveExemplars(prompt, actionExemplars, 2))
	messages := []oaMessage{
		{Role: "system", Content: system},
		{Role: "user", Content: prompt},
	}
	for round := 0; round <= shortcutMaxRepairs; round++ {
		resp, err := api.create(ctx, oaRequest{
			Model:      model,
			Messages:   messages,
			Tools:      tools,
			ToolChoice: map[string]any{"type": "function", "function": map[string]any{"name": emitActionTool}},
			MaxTokens:  1500,
		})
		if err != nil {
			return shortcut.Action{}, err
		}
		if len(resp.Choices) == 0 {
			return shortcut.Action{}, fmt.Errorf("deepseek returned no choices")
		}
		msg := resp.Choices[0].Message
		var call *oaToolCall
		for i := range msg.ToolCalls {
			if msg.ToolCalls[i].Function.Name == emitActionTool {
				call = &msg.ToolCalls[i]
			}
		}
		if call == nil {
			return shortcut.Action{}, fmt.Errorf("model did not call %s", emitActionTool)
		}
		var a shortcut.Action
		decodeErr := json.Unmarshal([]byte(call.Function.Arguments), &a)
		var problem string
		if decodeErr != nil {
			problem = "tool arguments were not valid Action JSON: " + decodeErr.Error()
		} else if verr := a.Validate(); verr != nil {
			problem = "validation failed: " + verr.Error()
		} else {
			return a, nil
		}
		messages = append(messages, msg, oaMessage{
			Role:       "tool",
			ToolCallID: call.ID,
			Content:    problem + ". Fix it and call " + emitActionTool + " again.",
		})
	}
	return shortcut.Action{}, fmt.Errorf("exhausted %d repair rounds without a valid action", shortcutMaxRepairs)
}
