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

// DeepSeek (default) AI track. DeepSeek's API is OpenAI-compatible, so the model
// is forced to call a single function whose JSON-schema parameters ARE the DSL
// schema (shared with the validator) — it can only emit structured DSL, never
// arbitrary code. Output is validated; on failure the error is fed back as a tool
// result and the model retries (auto-repair), up to maxRepairRounds.
//
// DeepSeek (api.deepseek.com) is a domestic endpoint, so the client bypasses any
// HTTPS_PROXY (the same domestic-DIRECT rule used for Feishu).

const (
	deepseekDefaultBase  = "https://api.deepseek.com"
	deepseekDefaultModel = "deepseek-chat"
)

// ─── OpenAI-compatible chat types ───

type oaMessage struct {
	Role       string       `json:"role"`
	Content    string       `json:"content,omitempty"`
	ToolCalls  []oaToolCall `json:"tool_calls,omitempty"`
	ToolCallID string       `json:"tool_call_id,omitempty"`
}

type oaToolCall struct {
	ID       string         `json:"id"`
	Type     string         `json:"type"`
	Function oaFunctionCall `json:"function"`
}

type oaFunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // a JSON string
}

type oaTool struct {
	Type     string        `json:"type"`
	Function oaFunctionDef `json:"function"`
}

type oaFunctionDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

type oaRequest struct {
	Model      string      `json:"model"`
	Messages   []oaMessage `json:"messages"`
	Tools      []oaTool    `json:"tools,omitempty"`
	ToolChoice any         `json:"tool_choice,omitempty"`
	MaxTokens  int         `json:"max_tokens,omitempty"`
}

type oaChoice struct {
	Message      oaMessage `json:"message"`
	FinishReason string    `json:"finish_reason"`
}

type oaError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
}

type oaResponse struct {
	Choices []oaChoice `json:"choices"`
	Error   *oaError   `json:"error"`
}

// chatAPI is the seam: one chat-completions round-trip. Tests substitute a fake.
type chatAPI interface {
	create(ctx context.Context, req oaRequest) (oaResponse, error)
}

type httpDeepSeek struct {
	apiKey  string
	baseURL string
	http    *http.Client
}

func (h httpDeepSeek) create(ctx context.Context, req oaRequest) (oaResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return oaResponse{}, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, h.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return oaResponse{}, err
	}
	httpReq.Header.Set("content-type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+h.apiKey)

	resp, err := h.http.Do(httpReq)
	if err != nil {
		return oaResponse{}, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return oaResponse{}, fmt.Errorf("deepseek %d: %s", resp.StatusCode, string(raw))
	}
	var out oaResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return oaResponse{}, fmt.Errorf("decode deepseek response: %w", err)
	}
	if out.Error != nil {
		return oaResponse{}, fmt.Errorf("deepseek error %s: %s", out.Error.Type, out.Error.Message)
	}
	return out, nil
}

// generateWithDeepSeek is the seam used by generateWithLLM. Returns ok=false when
// DEEPSEEK_API_KEY is absent or the call fails (→ fall back to keyword router).
func generateWithDeepSeek(prompt string) (dsl.AppDefinition, bool, error) {
	if !AIEnabled() { // AI hard-disabled: never egress the prompt
		return dsl.AppDefinition{}, false, nil
	}
	key := os.Getenv("DEEPSEEK_API_KEY")
	if key == "" {
		return dsl.AppDefinition{}, false, nil
	}
	model := os.Getenv("MODEL")
	if model == "" {
		model = deepseekDefaultModel
	}
	base := os.Getenv("DEEPSEEK_BASE_URL")
	if base == "" {
		base = deepseekDefaultBase
	}
	// ctx bounds the whole repair loop (up to maxRepairRounds+1 calls); the client
	// timeout bounds each single call.
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	client := &http.Client{Timeout: 45 * time.Second, Transport: &http.Transport{Proxy: nil}}
	def, err := generateViaChat(ctx, httpDeepSeek{apiKey: key, baseURL: base, http: client}, model, prompt)
	if err != nil {
		// Surfaces e.g. balance exhaustion / rate limits without failing the request.
		log.Printf("deepseek generation failed, falling back to keyword router: %v", err)
		return dsl.AppDefinition{}, false, nil
	}
	return def, true, nil
}

// generateViaChat runs the forced-function call + validate + auto-repair loop
// against any chatAPI. Pure logic — unit-tested with a fake.
func generateViaChat(ctx context.Context, api chatAPI, model, prompt string) (dsl.AppDefinition, error) {
	tools := []oaTool{{
		Type: "function",
		Function: oaFunctionDef{
			Name:        emitToolName,
			Description: "Emit the Feishu Bitable app definition (DSL) for the user's request.",
			Parameters:  appDefinitionSchema(),
		},
	}}
	messages := []oaMessage{
		{Role: "system", Content: genSystemPrompt},
		{Role: "user", Content: prompt},
	}

	for round := 0; round <= maxRepairRounds; round++ {
		resp, err := api.create(ctx, oaRequest{
			Model:      model,
			Messages:   messages,
			Tools:      tools,
			ToolChoice: map[string]any{"type": "function", "function": map[string]any{"name": emitToolName}},
			MaxTokens:  1024,
		})
		if err != nil {
			return dsl.AppDefinition{}, err
		}
		if len(resp.Choices) == 0 {
			return dsl.AppDefinition{}, fmt.Errorf("deepseek returned no choices")
		}
		msg := resp.Choices[0].Message
		var call *oaToolCall
		for i := range msg.ToolCalls {
			if msg.ToolCalls[i].Function.Name == emitToolName {
				call = &msg.ToolCalls[i]
			}
		}
		if call == nil {
			return dsl.AppDefinition{}, fmt.Errorf("model did not call %s", emitToolName)
		}

		var def dsl.AppDefinition
		decodeErr := json.Unmarshal([]byte(call.Function.Arguments), &def)
		if decodeErr == nil && def.Bind.BaseID == "" {
			def.Bind.BaseID = "current"
		}
		var problem string
		if decodeErr != nil {
			problem = "tool arguments were not valid AppDefinition JSON: " + decodeErr.Error()
		} else if verr := def.Validate(); verr != nil {
			problem = "validation failed: " + verr.Error()
		} else {
			return def, nil // success
		}

		// Feed the problem back for repair: echo the assistant turn, then a tool result.
		messages = append(messages, msg, oaMessage{
			Role:       "tool",
			ToolCallID: call.ID,
			Content:    problem + ". Fix it and call " + emitToolName + " again.",
		})
	}
	return dsl.AppDefinition{}, fmt.Errorf("exhausted %d repair rounds without a valid definition", maxRepairRounds)
}
