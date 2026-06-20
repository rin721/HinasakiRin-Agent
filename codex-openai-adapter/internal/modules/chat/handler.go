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

type Runner interface {
	Complete(ctx context.Context, req codex.Request) (codex.CompletionResult, error)
	Stream(ctx context.Context, req codex.Request, emit func(codex.StreamEvent) error) error
	Models(ctx context.Context) ([]codex.ModelInfo, error)
}

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

func (h *Handler) Register(router gin.IRouter) {
	router.POST("/chat/completions", h.ChatCompletions)
	router.GET("/models", h.Models)

	router.POST("/images/generations", h.UnsupportedEndpoint)
	router.POST("/images/edits", h.UnsupportedEndpoint)
	router.POST("/images/variations", h.UnsupportedEndpoint)
	router.POST("/audio/transcriptions", h.UnsupportedEndpoint)
	router.POST("/audio/speech", h.UnsupportedEndpoint)
	router.POST("/audio/translations", h.UnsupportedEndpoint)
	router.POST("/embeddings", h.UnsupportedEndpoint)
}

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

	promptInput, err := BuildPrompt(req.Messages, ImageLimits{
		MaxImages:     h.options.MaxImages,
		MaxImageBytes: h.options.MaxImageBytes,
	})
	if err != nil {
		writeError(c, http.StatusBadRequest, err.Error(), "invalid_request_error", "invalid_multimodal_content")
		return
	}

	modelForExec, responseModel := h.resolveModel(req.Model)
	runnerReq := codex.Request{
		Prompt: promptInput.Prompt,
		Model:  modelForExec,
		Images: toCodexImages(promptInput.Images),
	}

	if req.Stream {
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

func (h *Handler) StreamChatCompletions(c *gin.Context, req codex.Request, responseModel string) {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Status(http.StatusOK)

	id := "chatcmpl-codex-local"
	created := time.Now().Unix()
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
		_ = writeSSE(c, ErrorResponse{Error: ErrorBody{
			Message: err.Error(),
			Type:    "codex_exec_error",
			Code:    "codex_exec_failed",
		}})
		_ = writeRawSSE(c, "[DONE]")
		return
	}

	finishReason := "stop"
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

func (h *Handler) UnsupportedEndpoint(c *gin.Context) {
	writeError(c, http.StatusNotImplemented, "This endpoint is not supported by the Codex CLI-only adapter.", "unsupported_feature", "unsupported_endpoint")
}

func (h *Handler) resolveModel(requested string) (string, string) {
	if requested != "auto" {
		return requested, requested
	}
	if h.options.DefaultModel != "" {
		return h.options.DefaultModel, h.options.DefaultModel
	}
	return "", "codex-local"
}

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

func writeError(c *gin.Context, status int, message string, errorType string, code string) {
	c.JSON(status, ErrorResponse{
		Error: ErrorBody{
			Message: message,
			Type:    errorType,
			Code:    code,
		},
	})
}

func writeSSE(c *gin.Context, value any) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return writeRawSSE(c, string(payload))
}

func writeRawSSE(c *gin.Context, payload string) error {
	if _, err := fmt.Fprintf(c.Writer, "data: %s\n\n", payload); err != nil {
		return err
	}
	if flusher, ok := c.Writer.(http.Flusher); ok {
		flusher.Flush()
	}
	return nil
}
