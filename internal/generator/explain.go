package generator

import "strings"

// Explain turns a developer-grade generation/validation error into a short, plain-
// language hint the author can act on, plus the raw detail for debugging. Authors
// drive the model with words, not a TS compiler, so the hint avoids jargon. Callers
// surface `hint` as the message and `detail` as secondary text.
//
// Cases are ordered specific→general: the repair-exhaustion error carries the last
// concrete reason (see the *_llm repair loops), so a specific match (TS / URL /
// type / missing field) should win over the generic "exhausted" / "valid" catch-alls.
func Explain(err error) (hint, detail string) {
	if err == nil {
		return "", ""
	}
	detail = err.Error()
	low := strings.ToLower(detail)
	has := func(s string) bool { return strings.Contains(low, s) }
	switch {
	case strings.Contains(detail, "TS2554") || (has("expected") && has("argument")):
		return "某个输出表达式少给了参数(例如 substr/slice 需要『起始位置』和『长度』两个参数)。换种说法描述这一列怎么算,或先去掉它再试。", detail
	case strings.Contains(detail, "error TS") || has("tsc ") || has("does not exist") || has("compile"):
		return "生成的代码没通过真实 SDK 编译——通常是某个输出表达式写法不被支持。简化输出逻辑、减少输出列后再试。", detail
	case has("not in domains") || has("execute.url") || (has("domain") && has("invalid")) || has("host not"):
		return "接口地址和『出网域名白名单』对不上。请在需求里写清要调用的完整接口 URL(带域名),例如 https://api.example.com/…。", detail
	case has("primary") && (has("text") || has("number")):
		return "主输出列只能是文本或数字类型。把主输出改成文本/数字,或换一个主列再试。", detail
	case has("required") || has("at least one") || has("must not be empty"):
		return "需求缺少必要信息——输入字段、输出列、接口地址至少各要有一个。补全后再试。", detail
	case has("did not call") || has("no choices") || (has("not valid") && has("json")) || has("unmarshal"):
		return "AI 没能返回可用的结构化结果。把需求说得更短、更具体(输入哪个字段、调用哪个接口、输出什么)后重试。", detail
	case has("deepseek") || has("anthropic") || has("unreachable") || has("connection") || has("timeout") || has("rate limit") || has("quota") || has("balance") || has("insufficient") || has("api key") || has("api_key"):
		return "调用 AI 模型失败(网络或额度问题)。稍后重试;若持续失败,请管理员检查模型额度 / 网络。", detail
	case has("exhausted") || has("repair"):
		return "AI 试了几轮仍没生成出能通过校验的捷径。多半是需求缺关键信息——把『输入哪个字段、调用哪个接口 URL、输出什么』讲得更具体些再试。", detail
	case has("valid"):
		return "生成的捷径没通过校验。参考下面的细节调整需求(常见:接口 URL、输出列类型、必填项)后重试。", detail
	default:
		return "生成没成功。请把需求描述得更具体些(输入字段 / 接口 URL / 输出列),然后再试一次。", detail
	}
}
