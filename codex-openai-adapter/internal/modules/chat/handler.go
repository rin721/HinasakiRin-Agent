package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"codex-openai-adapter/internal/modules/codex"

	"github.com/gin-gonic/gin"
)

// Runner 是 chat 层依赖的最小 Codex 能力接口。
//
// 这样写有两个好处：
// 1. Handler 不需要知道 codex.Runner 的内部实现，只知道“能完成、能流式、能列模型”。
// 2. 单元测试可以用 fake runner，不必真的启动 Codex CLI。
type Runner interface {
	Complete(ctx context.Context, req codex.Request) (codex.CompletionResult, error)
	Stream(ctx context.Context, req codex.Request, emit func(codex.StreamEvent) error) error
	Models(ctx context.Context) ([]codex.ModelInfo, error)
}

// HandlerOptions 是 HTTP 层需要的运行时策略。
// 这些值通常来自 config.yaml，但放在 options 里能让测试更容易构造。
type HandlerOptions struct {
	DefaultModel  string
	MaxImages     int
	MaxImageBytes int64
}

type Handler struct {
	runner  Runner
	options HandlerOptions
}

func NewHandler(runner Runner, options HandlerOptions) *Handler {
	return &Handler{runner: runner, options: options}
}

// Register 注册 OpenAI-compatible 的 /v1 子路由。
//
// 注意：调用方 app.New 已经把这个 Handler 挂在 /v1 group 下，
// 所以这里注册的是 /chat/completions，而不是 /v1/chat/completions。
func (h *Handler) Register(router gin.IRouter) {
	router.POST("/chat/completions", h.ChatCompletions)
	router.GET("/models", h.Models)

	// Codex CLI-only 版本不伪造图片生成、音频或 embedding 能力。
	// 注册这些端点是为了让客户端得到明确的 OpenAI 风格错误，而不是 404 HTML。
	router.POST("/images/generations", h.UnsupportedEndpoint)
	router.POST("/images/edits", h.UnsupportedEndpoint)
	router.POST("/images/variations", h.UnsupportedEndpoint)
	router.POST("/audio/transcriptions", h.UnsupportedEndpoint)
	router.POST("/audio/speech", h.UnsupportedEndpoint)
	router.POST("/audio/translations", h.UnsupportedEndpoint)
	router.POST("/embeddings", h.UnsupportedEndpoint)
}

// ChatCompletions 是整个 adapter 的主入口。
//
// 数据流可以按这 5 步读：
// 1. 解析并校验 OpenAI-compatible JSON 请求。
// 2. 把 messages 转成 Codex CLI prompt，并提取图片附件。
// 3. 解析 model/reasoning，把它们映射成 codex.Request。
// 4. 根据 stream 选择普通 stdout 模式或 JSONL streaming 模式。
// 5. 把 Codex 输出包装成 OpenAI Chat Completions 响应。
func (h *Handler) ChatCompletions(c *gin.Context) {
	var req CompletionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, "invalid request body: "+err.Error(), "invalid_request_error", "invalid_request_body")
		return
	}
	if err := req.Validate(); err != nil {
		writeError(c, http.StatusBadRequest, err.Error(), "invalid_request_error", "invalid_request")
		return
	}

	// BuildPrompt 是结构化 OpenAI messages 到纯文本 prompt 的转换层。
	// 图片不会进入 prompt，而是进入 promptInput.Images。
	promptInput, err := BuildPrompt(req.Messages, ImageLimits{
		MaxImages:     h.options.MaxImages,
		MaxImageBytes: h.options.MaxImageBytes,
	})
	if err != nil {
		writeError(c, http.StatusBadRequest, err.Error(), "invalid_request_error", "invalid_multimodal_content")
		return
	}

	// responseModel 是返回给 OpenAI 客户端看的模型名；
	// modelForExec 是真正传给 `codex exec --model` 的模型名。
	// 当请求 model=auto 且没有 default_model 时，modelForExec 为空，表示让 Codex CLI 自选默认模型。
	modelForExec, responseModel := h.resolveModel(req.Model)
	runnerReq := codex.Request{
		Prompt:          promptInput.Prompt,
		Model:           modelForExec,
		ReasoningEffort: req.ResolvedReasoningEffort(),
		Images:          toCodexImages(promptInput.Images),
	}

	if req.Stream {
		// OpenAI streaming 是 HTTP SSE；Codex CLI streaming 是 JSONL。
		// StreamChatCompletions 负责把 JSONL 事件翻译成 SSE chunk。
		h.StreamChatCompletions(c, runnerReq, responseModel)
		return
	}

	result, err := h.runner.Complete(c.Request.Context(), runnerReq)
	if err != nil {
		writeError(c, http.StatusInternalServerError, err.Error(), "codex_exec_error", "codex_exec_failed")
		return
	}

	c.JSON(http.StatusOK, CompletionResponse{
		ID:      "chatcmpl-codex-local",
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   responseModel,
		Choices: []Choice{
			{
				Index: 0,
				Message: AssistantMessage{
					Role:    "assistant",
					Content: result.Content,
				},
				FinishReason: "stop",
			},
		},
	})
}

// StreamChatCompletions 把 Codex 的 JSONL 事件流包装成 OpenAI-compatible SSE。
//
// 这里的“真实流式”指 HTTP 层会持续发送 SSE chunk；
// 但 Codex CLI 当前的 JSONL 事件粒度通常是 agent message 完成后才出现，
// 所以它不保证 token-by-token。
func (h *Handler) StreamChatCompletions(c *gin.Context, req codex.Request, responseModel string) {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Status(http.StatusOK)

	id := "chatcmpl-codex-local"
	created := time.Now().Unix()
	// OpenAI SSE 的第一帧通常声明 assistant role，后续帧再发送 content。
	_ = writeSSE(c, StreamChunk{
		ID:      id,
		Object:  "chat.completion.chunk",
		Created: created,
		Model:   responseModel,
		Choices: []StreamChoice{
			{
				Index:        0,
				Delta:        StreamDelta{Role: "assistant"},
				FinishReason: nil,
			},
		},
	})

	err := h.runner.Stream(c.Request.Context(), req, func(event codex.StreamEvent) error {
		// Runner 已经把 Codex JSONL 过滤成少量内部事件。
		// Handler 只关心 content，把它放进 delta.content。
		if event.Type != "content" || event.Text == "" {
			return nil
		}
		return writeSSE(c, StreamChunk{
			ID:      id,
			Object:  "chat.completion.chunk",
			Created: created,
			Model:   responseModel,
			Choices: []StreamChoice{
				{
					Index:        0,
					Delta:        StreamDelta{Content: event.Text},
					FinishReason: nil,
				},
			},
		})
	})
	if err != nil {
		// SSE 已经开始后不能再改 HTTP 状态码，所以把错误作为 data chunk 发给客户端，
		// 然后仍用 [DONE] 结束，避免客户端一直等待。
		_ = writeSSE(c, ErrorResponse{Error: ErrorBody{
			Message: err.Error(),
			Type:    "codex_exec_error",
			Code:    "codex_exec_failed",
		}})
		_ = writeRawSSE(c, "[DONE]")
		return
	}

	finishReason := "stop"
	// 最后一帧用 finish_reason=stop 表示正常结束，再发送 OpenAI 约定的 [DONE]。
	_ = writeSSE(c, StreamChunk{
		ID:      id,
		Object:  "chat.completion.chunk",
		Created: created,
		Model:   responseModel,
		Choices: []StreamChoice{
			{
				Index:        0,
				Delta:        StreamDelta{},
				FinishReason: &finishReason,
			},
		},
	})
	_ = writeRawSSE(c, "[DONE]")
}

// Models 将 `codex debug models` 的 catalog 包装成 OpenAI-compatible 的 /v1/models。
// metadata 里保留 Codex 特有信息，便于学习和调试模型能力。
func (h *Handler) Models(c *gin.Context) {
	models, err := h.runner.Models(c.Request.Context())
	if err != nil {
		writeError(c, http.StatusInternalServerError, err.Error(), "codex_exec_error", "codex_models_failed")
		return
	}

	data := make([]ModelData, 0, len(models))
	for _, model := range models {
		if model.Slug == "" {
			continue
		}
		data = append(data, ModelData{
			ID:      model.Slug,
			Object:  "model",
			Created: 0,
			OwnedBy: "codex",
			Metadata: map[string]any{
				"display_name":       model.DisplayName,
				"visibility":         model.Visibility,
				"supported_in_api":   model.SupportedInAPI,
				"input_modalities":   model.InputModalities,
				"context_window":     model.ContextWindow,
				"max_context_window": model.MaxContextWindow,
			},
		})
	}

	c.JSON(http.StatusOK, ModelsResponse{
		Object: "list",
		Data:   data,
	})
}

// UnsupportedEndpoint 明确告诉客户端：这个端点不是 CLI-only adapter 能力的一部分。
// 这比沉默 404 更友好，也避免“看起来兼容但实际伪造”的误解。
func (h *Handler) UnsupportedEndpoint(c *gin.Context) {
	writeError(c, http.StatusNotImplemented, "This endpoint is not supported by the Codex CLI-only adapter.", "unsupported_feature", "unsupported_endpoint")
}

// resolveModel 处理 OpenAI-compatible 世界里的 model=auto。
//
// - 显式模型：直接传给 Codex CLI。
// - auto + default_model：使用配置里的默认模型。
// - auto + 无默认：不传 --model，让 Codex CLI 按自己的默认配置选择。
func (h *Handler) resolveModel(requested string) (string, string) {
	if requested != "auto" {
		return requested, requested
	}
	if h.options.DefaultModel != "" {
		return h.options.DefaultModel, h.options.DefaultModel
	}
	return "", "codex-local"
}

// toCodexImages 把 chat 模块的图片附件类型转换成 codex 模块的输入类型。
// 这层转换能保持模块边界清晰：chat 负责协议解析，codex 负责执行 CLI。
func toCodexImages(images []ImageAttachment) []codex.ImageAttachment {
	result := make([]codex.ImageAttachment, 0, len(images))
	for _, image := range images {
		result = append(result, codex.ImageAttachment{
			Name: image.Name,
			Data: image.Data,
		})
	}
	return result
}

// writeError 统一错误响应形状，尽量贴近 OpenAI 的 error object。
func writeError(c *gin.Context, status int, message string, errorType string, code string) {
	c.JSON(status, ErrorResponse{
		Error: ErrorBody{
			Message: message,
			Type:    errorType,
			Code:    code,
		},
	})
}

// writeSSE 将任意 payload 序列化成 OpenAI SSE 的 `data: ...` 格式。
func writeSSE(c *gin.Context, value any) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return writeRawSSE(c, string(payload))
}

// writeRawSSE 写入一帧 SSE，并尽量 flush 到客户端。
// Gin 的 ResponseWriter 支持 http.Flusher 时，客户端能更快收到 chunk。
func writeRawSSE(c *gin.Context, payload string) error {
	if _, err := fmt.Fprintf(c.Writer, "data: %s\n\n", payload); err != nil {
		return err
	}
	if flusher, ok := c.Writer.(http.Flusher); ok {
		flusher.Flush()
	}
	return nil
}
