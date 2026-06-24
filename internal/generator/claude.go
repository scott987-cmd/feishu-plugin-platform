package generator

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/dushibing/feishu-plugin-platform/internal/dsl"
)

// Claude (AI) generation track. The model is forced to call a single tool whose
// input_schema IS the DSL schema, so it can only emit structured DSL — never
// arbitrary code. The result is validated; on failure the validation error is
// fed back and the model is asked to fix it (auto-repair), up to maxRepairRounds.
//
// Anthropic's API is reached over the network, so the default HTTP transport
// (which honors HTTPS_PROXY) is used on purpose — the api.anthropic.com host is
// overseas and is expected to go through the proxy.

const (
	anthropicURL     = "https://api.anthropic.com/v1/messages"
	anthropicVersion = "2023-06-01"
	emitToolName     = "emit_app_definition"
	maxRepairRounds  = 2
	defaultModel     = "claude-opus-4-8"
)

type anthropicMessage struct {
	Role    string         `json:"role"`
	Content []contentBlock `json:"content"`
}

// contentBlock is a union over the block types we use: text, tool_use, tool_result.
type contentBlock struct {
	Type string `json:"type"`
	// text
	Text string `json:"text,omitempty"`
	// tool_use
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
	// tool_result
	ToolUseID string `json:"tool_use_id,omitempty"`
	Content   string `json:"content,omitempty"`
	IsError   bool   `json:"is_error,omitempty"`
}

type toolDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"input_schema"`
}

type anthropicRequest struct {
	Model      string             `json:"model"`
	MaxTokens  int                `json:"max_tokens"`
	System     string             `json:"system,omitempty"`
	Messages   []anthropicMessage `json:"messages"`
	Tools      []toolDef          `json:"tools,omitempty"`
	ToolChoice any                `json:"tool_choice,omitempty"`
}

type anthropicResponse struct {
	Content    []contentBlock `json:"content"`
	StopReason string         `json:"stop_reason"`
	Error      *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

// messagesAPI is the seam: one Messages API round-trip. Tests substitute a fake.
type messagesAPI interface {
	create(ctx context.Context, req anthropicRequest) (anthropicResponse, error)
}

type httpMessages struct {
	apiKey string
	http   *http.Client
}

func (h httpMessages) create(ctx context.Context, req anthropicRequest) (anthropicResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return anthropicResponse{}, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, anthropicURL, bytes.NewReader(body))
	if err != nil {
		return anthropicResponse{}, err
	}
	httpReq.Header.Set("content-type", "application/json")
	httpReq.Header.Set("x-api-key", h.apiKey)
	httpReq.Header.Set("anthropic-version", anthropicVersion)

	resp, err := h.http.Do(httpReq)
	if err != nil {
		return anthropicResponse{}, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return anthropicResponse{}, fmt.Errorf("anthropic %d: %s", resp.StatusCode, string(raw))
	}
	var out anthropicResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return anthropicResponse{}, fmt.Errorf("decode anthropic response: %w", err)
	}
	if out.Error != nil {
		return anthropicResponse{}, fmt.Errorf("anthropic error %s: %s", out.Error.Type, out.Error.Message)
	}
	return out, nil
}

// generateWithAnthropic is the opt-in Claude provider (LLM_PROVIDER=anthropic).
// Returns ok=false when the key is absent or the call fails — so the caller falls
// back to the deterministic keyword router rather than erroring the whole request.
func generateWithAnthropic(prompt string) (dsl.AppDefinition, bool, error) {
	if !AIEnabled() { // AI hard-disabled: never egress the prompt
		return dsl.AppDefinition{}, false, nil
	}
	key := os.Getenv("ANTHROPIC_API_KEY")
	if key == "" {
		return dsl.AppDefinition{}, false, nil
	}
	model := os.Getenv("MODEL")
	if model == "" {
		model = defaultModel
	}
	// ctx bounds the whole repair loop; the client timeout bounds each single call.
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	def, err := generateViaMessages(ctx, httpMessages{apiKey: key, http: &http.Client{Timeout: 45 * time.Second}}, model, prompt)
	if err != nil {
		log.Printf("claude generation failed, falling back to keyword router: %v", err)
		return dsl.AppDefinition{}, false, nil
	}
	return def, true, nil
}

// generateViaMessages runs the forced-tool call + validate + auto-repair loop
// against any messagesAPI. Pure logic — unit-tested with a fake.
func generateViaMessages(ctx context.Context, api messagesAPI, model, prompt string) (dsl.AppDefinition, error) {
	system := genSystemPrompt
	tools := []toolDef{{
		Name:        emitToolName,
		Description: "Emit the Feishu Bitable app definition (DSL) for the user's request.",
		InputSchema: appDefinitionSchema(),
	}}
	messages := []anthropicMessage{{Role: "user", Content: []contentBlock{{Type: "text", Text: prompt}}}}

	for round := 0; round <= maxRepairRounds; round++ {
		resp, err := api.create(ctx, anthropicRequest{
			Model:      model,
			MaxTokens:  1024,
			System:     system,
			Messages:   messages,
			Tools:      tools,
			ToolChoice: map[string]any{"type": "tool", "name": emitToolName},
		})
		if err != nil {
			return dsl.AppDefinition{}, err
		}
		var input json.RawMessage
		var toolUseID string
		for _, c := range resp.Content {
			if c.Type == "tool_use" && c.Name == emitToolName {
				input, toolUseID = c.Input, c.ID
			}
		}
		if len(input) == 0 {
			return dsl.AppDefinition{}, fmt.Errorf("model did not call %s", emitToolName)
		}

		var def dsl.AppDefinition
		decodeErr := json.Unmarshal(input, &def)
		if decodeErr == nil && def.Bind.BaseID == "" {
			def.Bind.BaseID = "current"
		}
		var problem string
		if decodeErr != nil {
			problem = "tool input was not valid AppDefinition JSON: " + decodeErr.Error()
		} else if verr := def.Validate(); verr != nil {
			problem = "validation failed: " + verr.Error()
		} else {
			return def, nil // success
		}

		// Feed the problem back for repair on the next round.
		messages = append(messages,
			anthropicMessage{Role: "assistant", Content: []contentBlock{{Type: "tool_use", ID: toolUseID, Name: emitToolName, Input: input}}},
			anthropicMessage{Role: "user", Content: []contentBlock{{Type: "tool_result", ToolUseID: toolUseID, Content: problem + ". Fix it and call " + emitToolName + " again.", IsError: true}}},
		)
	}
	return dsl.AppDefinition{}, fmt.Errorf("exhausted %d repair rounds without a valid definition", maxRepairRounds)
}

// appDefinitionSchema builds the tool input schema from the same enum sources as
// dsl.Validate, so the model's constraints and the validator never drift apart.
func appDefinitionSchema() map[string]any {
	str := map[string]any{"type": "string"}
	enum := func(vals []string) map[string]any { return map[string]any{"type": "string", "enum": vals} }
	// d returns a copy of base with a description — guides the model on field usage.
	d := func(base map[string]any, desc string) map[string]any {
		out := map[string]any{"description": desc}
		for k, v := range base {
			out[k] = v
		}
		return out
	}
	aggSpec := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"agg":   d(enum(dsl.ValidAggs), "how to aggregate"),
			"field": d(str, "the column name to aggregate"),
		},
	}
	component := map[string]any{
		"type":        "object",
		"required":    []string{"type"},
		"description": "a renderable unit; set only the fields relevant to its type",
		"properties": map[string]any{
			"type":      enum(dsl.ValidComponents),
			"title":     d(str, "display title"),
			"agg":       d(enum(dsl.ValidAggs), "STAT only: how to aggregate `field` (distinct = count of distinct values; median = middle value)"),
			"field":     d(str, "STAT only: the column to aggregate — do NOT use for a chart's category column"),
			"filter":    d(str, "optional filter expression over the table"),
			"chartType": d(enum(dsl.ValidCharts), "CHART only: required"),
			"x":         d(str, "CHART only: the category / group-by column name (x-axis)"),
			"y":         d(aggSpec, "CHART only: the aggregated measure (y-axis)"),
			"text":      d(str, "TEXT only: the text content"),
			"columns":   d(map[string]any{"type": "array", "items": str}, "TABLE only: column names to show (omit = all)"),
			"target":    d(map[string]any{"type": "number"}, "GAUGE only: target value; progress = aggregated value / target"),
			"col":       d(str, "PIVOT only: the column group-by field (row group uses x)"),
			"sort":      d(enum(dsl.ValidSorts), "CHART only: sort groups asc/desc (use with limit for TopN)"),
			"limit":     d(map[string]any{"type": "integer"}, "CHART only: keep top N groups"),
		},
	}
	action := map[string]any{
		"type":     "object",
		"required": []string{"id", "trigger", "do"},
		"properties": map[string]any{
			"id":      str,
			"trigger": enum(dsl.ValidTriggers),
			"label":   str,
			"do":      enum(dsl.ValidActions),
			"scope":   str,
		},
	}
	return map[string]any{
		"type":     "object",
		"required": []string{"id", "name", "type", "ui"},
		"properties": map[string]any{
			"id":   d(str, "stable url-safe id, e.g. sales_dashboard"),
			"name": d(str, "short human-readable name"),
			"type": enum(dsl.ValidTypes),
			"bind": map[string]any{
				"type":       "object",
				"properties": map[string]any{"baseId": d(str, "host base; use \"current\""), "tableId": str},
			},
			"ui": map[string]any{
				"type":     "object",
				"required": []string{"components"},
				"properties": map[string]any{
					"layout":     d(str, "dashboard | list | form"),
					"components": map[string]any{"type": "array", "items": component},
				},
			},
			"actions": map[string]any{"type": "array", "items": action},
		},
	}
}
