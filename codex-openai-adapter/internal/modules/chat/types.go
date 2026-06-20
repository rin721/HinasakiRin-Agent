package chat

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Message 是 OpenAI Chat Completions 中 messages 数组里的单条消息。
//
// 学习重点：
// OpenAI 的 message.content 不一定只是字符串。多模态请求里，它也可能是
// content parts 数组，例如 text + image_url。因此这里不能简单写成 string，
// 而是交给 MessageContent 自定义反序列化。
type Message struct {
	Role    string         `json:"role"`
	Content MessageContent `json:"content"`
}

// MessageContent 兼容两种 OpenAI 常见输入形态：
//
//  1. 纯文本：
//     { "role": "user", "content": "hello" }
//
//  2. 多模态 parts：
//     { "role": "user", "content": [
//     { "type": "text", "text": "describe this" },
//     { "type": "image_url", "image_url": { "url": "data:image/png;base64,..." } }
//     ] }
//
// 这样设计可以让上层 Handler 不关心 JSON 的原始形态，只关心“是否有文本和图片”。
type MessageContent struct {
	Text  *string
	Parts []ContentPart
}

// ContentPart 是多模态 content 数组中的一个元素。
// 当前 CLI-only 版本只实现 text 和 image_url；其它 OpenAI content part
// 类型如果出现，会在 BuildPrompt 阶段返回明确错误。
type ContentPart struct {
	Type     string        `json:"type"`
	Text     string        `json:"text,omitempty"`
	ImageURL *ImageURLPart `json:"image_url,omitempty"`
}

// ImageURLPart 保留 OpenAI 的字段形状。
// 本项目第一版只接受 base64 data URL，不下载远程 URL，也不读取本地文件路径。
type ImageURLPart struct {
	URL    string `json:"url"`
	Detail string `json:"detail,omitempty"`
}

// UnmarshalJSON 是多模态兼容的关键点：
// 先尝试把 content 当作 string 解析；如果失败，再尝试解析为 content parts 数组。
// 这比用 interface{} 到处做类型断言更集中，也更适合教学阅读。
func (c *MessageContent) UnmarshalJSON(data []byte) error {
	var text string
	if err := json.Unmarshal(data, &text); err == nil {
		c.Text = &text
		c.Parts = nil
		return nil
	}

	var parts []ContentPart
	if err := json.Unmarshal(data, &parts); err != nil {
		return fmt.Errorf("content must be a string or an array of content parts")
	}

	c.Text = nil
	c.Parts = parts
	return nil
}

func (c MessageContent) IsZero() bool {
	return c.Text == nil && c.Parts == nil
}

// CompletionRequest 是我们支持的 Chat Completions 请求子集。
//
// 注意：这里接收了一些 OpenAI-compatible 常见字段，比如 temperature、top_p、tools。
// 但 Codex CLI 并不一定能原样支持这些参数。教学版会“接受但不强行伪造行为”，
// 真正能映射到 Codex CLI 的是 model、stream、messages、reasoning_effort 和图片输入。
type CompletionRequest struct {
	Model           string            `json:"model"`
	Messages        []Message         `json:"messages"`
	Temperature     *float64          `json:"temperature,omitempty"`
	MaxTokens       *int              `json:"max_tokens,omitempty"`
	TopP            *float64          `json:"top_p,omitempty"`
	Stop            json.RawMessage   `json:"stop,omitempty"`
	Stream          bool              `json:"stream,omitempty"`
	N               *int              `json:"n,omitempty"`
	ReasoningEffort string            `json:"reasoning_effort,omitempty"`
	Reasoning       *ReasoningOptions `json:"reasoning,omitempty"`
	Tools           json.RawMessage   `json:"tools,omitempty"`
	ToolChoice      json.RawMessage   `json:"tool_choice,omitempty"`
	User            string            `json:"user,omitempty"`
}

// ReasoningOptions 兼容 Responses API 风格的 reasoning 对象。
// 在 Chat Completions 请求里也接收它，是为了让不同 OpenAI-compatible 客户端更容易接入。
type ReasoningOptions struct {
	Effort          string `json:"effort,omitempty"`
	Summary         string `json:"summary,omitempty"`
	GenerateSummary string `json:"generate_summary,omitempty"`
}

// Validate 做“协议层”的校验：请求是否像一个可执行的 OpenAI-compatible 请求。
// 它不调用 Codex，也不检查模型是否真实存在；模型存在性留给 Codex CLI 自己决定。
func (r CompletionRequest) Validate() error {
	if strings.TrimSpace(r.Model) == "" {
		return fmt.Errorf("model is required")
	}
	if len(r.Messages) == 0 {
		return fmt.Errorf("messages must contain at least one item")
	}
	if r.N != nil && *r.N != 1 {
		return fmt.Errorf("n values other than 1 are not supported")
	}
	if effort := r.ResolvedReasoningEffort(); effort != "" && !isSupportedReasoningEffort(effort) {
		return fmt.Errorf("reasoning effort %q is not supported by the Codex CLI adapter; use low, medium, high, or xhigh", effort)
	}

	for i, message := range r.Messages {
		switch message.Role {
		case "system", "developer", "user", "assistant", "tool":
		default:
			return fmt.Errorf("messages[%d].role must be one of system, developer, user, assistant, or tool", i)
		}
		if message.Content.IsZero() {
			return fmt.Errorf("messages[%d].content is required", i)
		}
	}

	return nil
}

// ResolvedReasoningEffort 规定优先级：
// reasoning_effort 是 Chat Completions 生态里常见的扁平字段；
// reasoning.effort 是 Responses API 的对象式字段。
// 如果两者同时出现，扁平字段优先，便于旧客户端覆盖默认值。
func (r CompletionRequest) ResolvedReasoningEffort() string {
	if strings.TrimSpace(r.ReasoningEffort) != "" {
		return strings.TrimSpace(r.ReasoningEffort)
	}
	if r.Reasoning != nil {
		return strings.TrimSpace(r.Reasoning.Effort)
	}
	return ""
}

// isSupportedReasoningEffort 只允许 Codex CLI 当前能稳定映射的值。
// OpenAI Responses API 还有 none/minimal 等值，但 Codex CLI 的
// model_reasoning_effort 这里按 low/medium/high/xhigh 教学实现。
func isSupportedReasoningEffort(value string) bool {
	switch value {
	case "low", "medium", "high", "xhigh":
		return true
	default:
		return false
	}
}

// CompletionResponse 是非流式 Chat Completions 的 OpenAI-compatible 响应形状。
type CompletionResponse struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
}

// Choice 对应 OpenAI choices[0]。
// 当前 adapter 只支持 n=1，因此总是返回一个 choice。
type Choice struct {
	Index        int              `json:"index"`
	Message      AssistantMessage `json:"message"`
	FinishReason string           `json:"finish_reason"`
}

// AssistantMessage 是最终 assistant 文本。Codex CLI stdout 会被放到 content 里。
type AssistantMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// StreamChunk 是 stream=true 时的 SSE payload。
// 每个 chunk 会以 `data: {...}\n\n` 的格式写给客户端。
type StreamChunk struct {
	ID      string         `json:"id"`
	Object  string         `json:"object"`
	Created int64          `json:"created"`
	Model   string         `json:"model"`
	Choices []StreamChoice `json:"choices"`
}

// StreamChoice 用 delta 表示增量内容，这是 OpenAI streaming 的基本约定。
type StreamChoice struct {
	Index        int         `json:"index"`
	Delta        StreamDelta `json:"delta"`
	FinishReason *string     `json:"finish_reason"`
}

// StreamDelta 中第一次通常只发 role，后续发 content。
type StreamDelta struct {
	Role    string `json:"role,omitempty"`
	Content string `json:"content,omitempty"`
}

// ModelsResponse 是 /v1/models 的列表响应。
// 数据来自 `codex debug models`，再包装成 OpenAI-compatible 的 model list。
type ModelsResponse struct {
	Object string      `json:"object"`
	Data   []ModelData `json:"data"`
}

// ModelData 的 metadata 保留 Codex catalog 中对学习最有用的信息：
// 输入模态、上下文窗口、可见性、是否支持 API 等。
type ModelData struct {
	ID       string         `json:"id"`
	Object   string         `json:"object"`
	Created  int64          `json:"created"`
	OwnedBy  string         `json:"owned_by"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// ErrorResponse 统一错误形状，方便 OpenAI SDK 或 OpenAI-compatible 客户端处理。
type ErrorResponse struct {
	Error ErrorBody `json:"error"`
}

// ErrorBody 尽量贴近 OpenAI 风格：message 给人看，type/code 给程序判断。
type ErrorBody struct {
	Message string `json:"message"`
	Type    string `json:"type,omitempty"`
	Code    string `json:"code,omitempty"`
}
