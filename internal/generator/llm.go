package generator

import (
	"os"

	"github.com/dushibing/feishu-plugin-platform/internal/dsl"
)

// genSystemPrompt is shared by every LLM provider so the instructions (and the
// "must call the tool / stay on-schema" contract) never drift between them.
const genSystemPrompt = "You generate a Feishu Bitable view-extension application definition (a JSON 'DSL'). " +
	"You MUST call the " + emitToolName + " tool exactly once. Use only the fields and enum values allowed by the tool's input schema. " +
	"Reuse field names implied by the user's request. Never invent component types, aggregations, chart types, or actions outside the schema. " +
	"For a 'chart' component: set chartType, put the group/category column name in `x`, and the aggregated measure in `y` (agg+field). " +
	"The top-level `field`/`agg` are for 'stat' components only — do NOT put a chart's category column in `field`. " +
	"An optional `filter` on a stat/chart uses ONLY this mini-syntax (the renderer parses nothing else): conditions `LHS OP RHS` joined by AND/OR; " +
	"OP ∈ =, !=, >, >=, <, <=, contains, in; LHS is a real column name or month()/year()/day() of a date column; " +
	"RHS is a number, a bare word/quoted string, a list `[a,b]` (for `in`), or a macro THIS_MONTH/THIS_YEAR/TODAY. " +
	"Examples: `订单状态=已完成` · `month(下单时间)=THIS_MONTH` · `金额>=1000 AND 订单状态 in [已完成,已付款]`. Omit `filter` if not needed."

// generateWithLLM is the AI track's provider switch, called by fromNL.
//
//   - default / "deepseek": DeepSeek (OpenAI-compatible). Set DEEPSEEK_API_KEY.
//   - "anthropic": Claude. Set ANTHROPIC_API_KEY. (Opt-in; not the default.)
//
// Each provider returns ok=false when its key is absent or the call fails, so
// fromNL falls back to the deterministic keyword router instead of erroring.
func generateWithLLM(prompt string) (dsl.AppDefinition, bool, error) {
	switch os.Getenv("LLM_PROVIDER") {
	case "anthropic":
		return generateWithAnthropic(prompt)
	default: // "" or "deepseek"
		return generateWithDeepSeek(prompt)
	}
}
