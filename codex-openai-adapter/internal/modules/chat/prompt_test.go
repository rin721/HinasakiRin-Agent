package chat

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

func TestBuildPromptIncludesGuardAndStringMessage(t *testing.T) {
	content := "Return JSON only."
	input, err := BuildPrompt([]Message{
		{Role: "user", Content: MessageContent{Text: &content}},
	}, ImageLimits{MaxImages: 10, MaxImageBytes: 1024})
	if err != nil {
		t.Fatalf("BuildPrompt returned error: %v", err)
	}

	if !strings.Contains(input.Prompt, "Do not directly inspect or modify local files from this adapter.") {
		t.Fatalf("prompt did not include local file guard")
	}
	if !strings.Contains(input.Prompt, "If the conversation asks you to output a JSON action or tool call, output that JSON action exactly.") {
		t.Fatalf("prompt did not include JSON action guidance")
	}
	if !strings.Contains(input.Prompt, "<user>\nReturn JSON only.\n</user>") {
		t.Fatalf("prompt did not include user message: %s", input.Prompt)
	}
}

func TestMessageContentAcceptsTextPartsAndImageDataURL(t *testing.T) {
	raw := `{
		"role": "user",
		"content": [
			{ "type": "text", "text": "describe this" },
			{ "type": "image_url", "image_url": { "url": "data:image/png;base64,` + base64.StdEncoding.EncodeToString([]byte("png")) + `" } }
		]
	}`

	var message Message
	if err := json.Unmarshal([]byte(raw), &message); err != nil {
		t.Fatalf("unmarshal message: %v", err)
	}

	input, err := BuildPrompt([]Message{message}, ImageLimits{MaxImages: 10, MaxImageBytes: 1024})
	if err != nil {
		t.Fatalf("BuildPrompt returned error: %v", err)
	}
	if !strings.Contains(input.Prompt, "describe this") {
		t.Fatalf("prompt omitted text part: %s", input.Prompt)
	}
	if !strings.Contains(input.Prompt, "[image attached: image-1.png]") {
		t.Fatalf("prompt omitted image placeholder: %s", input.Prompt)
	}
	if len(input.Images) != 1 || string(input.Images[0].Data) != "png" {
		t.Fatalf("unexpected image attachments: %#v", input.Images)
	}
}

func TestBuildPromptRejectsRemoteImageURL(t *testing.T) {
	message := Message{
		Role: "user",
		Content: MessageContent{Parts: []ContentPart{
			{Type: "image_url", ImageURL: &ImageURLPart{URL: "https://example.com/image.png"}},
		}},
	}

	_, err := BuildPrompt([]Message{message}, ImageLimits{MaxImages: 10, MaxImageBytes: 1024})
	if err == nil || !strings.Contains(err.Error(), "only base64 data image_url values are supported") {
		t.Fatalf("expected remote URL rejection, got %v", err)
	}
}

func TestBuildPromptRejectsTooManyImages(t *testing.T) {
	url := "data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte("png"))
	message := Message{
		Role: "user",
		Content: MessageContent{Parts: []ContentPart{
			{Type: "image_url", ImageURL: &ImageURLPart{URL: url}},
			{Type: "image_url", ImageURL: &ImageURLPart{URL: url}},
		}},
	}

	_, err := BuildPrompt([]Message{message}, ImageLimits{MaxImages: 1, MaxImageBytes: 1024})
	if err == nil || !strings.Contains(err.Error(), "too many images") {
		t.Fatalf("expected too many images error, got %v", err)
	}
}

func TestBuildPromptRejectsOversizedImage(t *testing.T) {
	url := "data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte("png"))
	message := Message{
		Role: "user",
		Content: MessageContent{Parts: []ContentPart{
			{Type: "image_url", ImageURL: &ImageURLPart{URL: url}},
		}},
	}

	_, err := BuildPrompt([]Message{message}, ImageLimits{MaxImages: 10, MaxImageBytes: 1})
	if err == nil || !strings.Contains(err.Error(), "image is too large") {
		t.Fatalf("expected oversized image error, got %v", err)
	}
}
