package chat

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"codex-openai-adapter/internal/modules/codex"

	"github.com/gin-gonic/gin"
)

type fakeRunner struct {
	lastRequest  codex.Request
	models       []codex.ModelInfo
	streamEvents []codex.StreamEvent
}

func (f *fakeRunner) Complete(_ context.Context, req codex.Request) (codex.CompletionResult, error) {
	f.lastRequest = req
	return codex.CompletionResult{Content: "ok"}, nil
}

func (f *fakeRunner) Stream(_ context.Context, req codex.Request, emit func(codex.StreamEvent) error) error {
	f.lastRequest = req
	for _, event := range f.streamEvents {
		if err := emit(event); err != nil {
			return err
		}
	}
	return nil
}

func (f *fakeRunner) Models(context.Context) ([]codex.ModelInfo, error) {
	return f.models, nil
}

func TestChatCompletionsAcceptsImageDataURL(t *testing.T) {
	gin.SetMode(gin.TestMode)
	runner := &fakeRunner{}
	engine := newTestEngine(runner)

	image := base64.StdEncoding.EncodeToString([]byte("png"))
	body := `{
		"model": "gpt-5.4-mini",
		"messages": [
			{
				"role": "user",
				"content": [
					{ "type": "text", "text": "describe this" },
					{ "type": "image_url", "image_url": { "url": "data:image/png;base64,` + image + `" } }
				]
			}
		]
	}`

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", recorder.Code, recorder.Body.String())
	}
	if runner.lastRequest.Model != "gpt-5.4-mini" {
		t.Fatalf("expected explicit model to be passed through, got %q", runner.lastRequest.Model)
	}
	if len(runner.lastRequest.Images) != 1 || string(runner.lastRequest.Images[0].Data) != "png" {
		t.Fatalf("unexpected images: %#v", runner.lastRequest.Images)
	}
	if strings.Contains(runner.lastRequest.Prompt, image) {
		t.Fatalf("prompt leaked base64 image data")
	}
	if !strings.Contains(runner.lastRequest.Prompt, "[image attached: image-1.png]") {
		t.Fatalf("prompt omitted image placeholder: %s", runner.lastRequest.Prompt)
	}
}

func TestChatCompletionsUsesDefaultModelForAuto(t *testing.T) {
	runner := &fakeRunner{}
	engine := newTestEngineWithOptions(runner, HandlerOptions{
		DefaultModel:  "gpt-5.4-mini",
		MaxImages:     10,
		MaxImageBytes: 1024,
	})

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/chat/completions", strings.NewReader(`{
		"model": "auto",
		"messages": [{ "role": "user", "content": "hello" }]
	}`))
	req.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", recorder.Code, recorder.Body.String())
	}
	if runner.lastRequest.Model != "gpt-5.4-mini" {
		t.Fatalf("expected default model, got %q", runner.lastRequest.Model)
	}
	if !strings.Contains(recorder.Body.String(), `"model":"gpt-5.4-mini"`) {
		t.Fatalf("response did not include resolved model: %s", recorder.Body.String())
	}
}

func TestChatCompletionsRejectsNGreaterThanOne(t *testing.T) {
	engine := newTestEngine(&fakeRunner{})

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/chat/completions", strings.NewReader(`{
		"model": "auto",
		"messages": [{ "role": "user", "content": "hello" }],
		"n": 2
	}`))
	req.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "n values other than 1 are not supported") {
		t.Fatalf("unexpected error: %s", recorder.Body.String())
	}
}

func TestStreamingChatCompletions(t *testing.T) {
	runner := &fakeRunner{streamEvents: []codex.StreamEvent{{Type: "content", Text: "hello"}}}
	engine := newTestEngine(runner)

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/chat/completions", strings.NewReader(`{
		"model": "auto",
		"messages": [{ "role": "user", "content": "hello" }],
		"stream": true
	}`))
	req.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", recorder.Code, recorder.Body.String())
	}
	body := recorder.Body.String()
	if !strings.Contains(body, `"role":"assistant"`) || !strings.Contains(body, `"content":"hello"`) || !strings.Contains(body, "data: [DONE]") {
		t.Fatalf("unexpected SSE body: %s", body)
	}
}

func TestModels(t *testing.T) {
	runner := &fakeRunner{models: []codex.ModelInfo{{
		Slug:            "gpt-5.4-mini",
		DisplayName:     "GPT-5.4-Mini",
		Visibility:      "list",
		SupportedInAPI:  true,
		InputModalities: []string{"text", "image"},
	}}}
	engine := newTestEngine(runner)

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/models", nil)
	engine.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), `"id":"gpt-5.4-mini"`) || !strings.Contains(recorder.Body.String(), `"input_modalities":["text","image"]`) {
		t.Fatalf("unexpected models response: %s", recorder.Body.String())
	}
}

func TestUnsupportedEndpoint(t *testing.T) {
	engine := newTestEngine(&fakeRunner{})

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/images/generations", nil)
	engine.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNotImplemented {
		t.Fatalf("expected 501, got %d: %s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "unsupported_endpoint") {
		t.Fatalf("unexpected unsupported response: %s", recorder.Body.String())
	}
}

func newTestEngine(runner Runner) *gin.Engine {
	return newTestEngineWithOptions(runner, HandlerOptions{
		MaxImages:     10,
		MaxImageBytes: 1024,
	})
}

func newTestEngineWithOptions(runner Runner, options HandlerOptions) *gin.Engine {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	NewHandler(runner, options).Register(engine)
	return engine
}
