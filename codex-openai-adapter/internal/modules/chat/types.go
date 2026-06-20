package chat

import (
	"encoding/json"
	"fmt"
	"strings"
)

type Message struct {
	Role    string         `json:"role"`
	Content MessageContent `json:"content"`
}

type MessageContent struct {
	Text  *string
	Parts []ContentPart
}

type ContentPart struct {
	Type     string        `json:"type"`
	Text     string        `json:"text,omitempty"`
	ImageURL *ImageURLPart `json:"image_url,omitempty"`
}

type ImageURLPart struct {
	URL    string `json:"url"`
	Detail string `json:"detail,omitempty"`
}

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

type ReasoningOptions struct {
	Effort          string `json:"effort,omitempty"`
	Summary         string `json:"summary,omitempty"`
	GenerateSummary string `json:"generate_summary,omitempty"`
}

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

func (r CompletionRequest) ResolvedReasoningEffort() string {
	if strings.TrimSpace(r.ReasoningEffort) != "" {
		return strings.TrimSpace(r.ReasoningEffort)
	}
	if r.Reasoning != nil {
		return strings.TrimSpace(r.Reasoning.Effort)
	}
	return ""
}

func isSupportedReasoningEffort(value string) bool {
	switch value {
	case "low", "medium", "high", "xhigh":
		return true
	default:
		return false
	}
}

type CompletionResponse struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
}

type Choice struct {
	Index        int              `json:"index"`
	Message      AssistantMessage `json:"message"`
	FinishReason string           `json:"finish_reason"`
}

type AssistantMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type StreamChunk struct {
	ID      string         `json:"id"`
	Object  string         `json:"object"`
	Created int64          `json:"created"`
	Model   string         `json:"model"`
	Choices []StreamChoice `json:"choices"`
}

type StreamChoice struct {
	Index        int         `json:"index"`
	Delta        StreamDelta `json:"delta"`
	FinishReason *string     `json:"finish_reason"`
}

type StreamDelta struct {
	Role    string `json:"role,omitempty"`
	Content string `json:"content,omitempty"`
}

type ModelsResponse struct {
	Object string      `json:"object"`
	Data   []ModelData `json:"data"`
}

type ModelData struct {
	ID       string         `json:"id"`
	Object   string         `json:"object"`
	Created  int64          `json:"created"`
	OwnedBy  string         `json:"owned_by"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

type ErrorResponse struct {
	Error ErrorBody `json:"error"`
}

type ErrorBody struct {
	Message string `json:"message"`
	Type    string `json:"type,omitempty"`
	Code    string `json:"code,omitempty"`
}
