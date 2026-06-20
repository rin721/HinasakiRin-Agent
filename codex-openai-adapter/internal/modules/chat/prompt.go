package chat

import (
	"encoding/base64"
	"fmt"
	"strings"
)

const adapterSystemGuard = `You are being used through a local OpenAI-compatible adapter.
You are acting as the model behind another agent loop.
Do not directly inspect or modify local files from this adapter.
Do not directly run shell commands from this adapter.
If the conversation asks you to output a JSON action or tool call, output that JSON action exactly.
It is okay to describe or request tool actions as JSON; the upstream agent will decide whether to execute them.
Do not refuse only because the task mentions files, patches, repositories, or commands.
Only answer the latest user request.
If the user asks for JSON only, return JSON only.`

type ImageLimits struct {
	MaxImages     int
	MaxImageBytes int64
}

type PromptInput struct {
	Prompt string
	Images []ImageAttachment
}

type ImageAttachment struct {
	Name      string
	MIME      string
	Extension string
	Data      []byte
}

func BuildPrompt(messages []Message, limits ImageLimits) (PromptInput, error) {
	var builder strings.Builder
	images := make([]ImageAttachment, 0)

	builder.WriteString(adapterSystemGuard)
	builder.WriteString("\n\nConversation:\n")

	for _, message := range messages {
		builder.WriteString("\n<")
		builder.WriteString(message.Role)
		builder.WriteString(">\n")

		if message.Content.Text != nil {
			builder.WriteString(*message.Content.Text)
			builder.WriteString("\n")
		} else {
			for _, part := range message.Content.Parts {
				switch part.Type {
				case "text":
					builder.WriteString(part.Text)
					builder.WriteString("\n")
				case "image_url":
					if part.ImageURL == nil {
						return PromptInput{}, fmt.Errorf("image_url content part is missing image_url")
					}
					if limits.MaxImages > 0 && len(images) >= limits.MaxImages {
						return PromptInput{}, fmt.Errorf("too many images: maximum is %d", limits.MaxImages)
					}

					image, err := parseDataImageURL(part.ImageURL.URL, len(images)+1, limits.MaxImageBytes)
					if err != nil {
						return PromptInput{}, err
					}
					images = append(images, image)
					builder.WriteString("[image attached: ")
					builder.WriteString(image.Name)
					builder.WriteString("]\n")
				default:
					return PromptInput{}, fmt.Errorf("unsupported content part type %q", part.Type)
				}
			}
		}

		builder.WriteString("</")
		builder.WriteString(message.Role)
		builder.WriteString(">\n")
	}

	builder.WriteString("\nAnswer the latest user request.")
	return PromptInput{
		Prompt: builder.String(),
		Images: images,
	}, nil
}

func parseDataImageURL(value string, index int, maxBytes int64) (ImageAttachment, error) {
	if !strings.HasPrefix(value, "data:") {
		return ImageAttachment{}, fmt.Errorf("only base64 data image_url values are supported")
	}

	comma := strings.Index(value, ",")
	if comma < 0 {
		return ImageAttachment{}, fmt.Errorf("invalid image data URL")
	}

	metadata := value[len("data:"):comma]
	payload := value[comma+1:]
	parts := strings.Split(metadata, ";")
	mime := strings.ToLower(strings.TrimSpace(parts[0]))
	if !hasBase64Flag(parts[1:]) {
		return ImageAttachment{}, fmt.Errorf("image data URL must be base64 encoded")
	}

	extension, ok := supportedImageExtension(mime)
	if !ok {
		return ImageAttachment{}, fmt.Errorf("unsupported image MIME type %q", mime)
	}

	if maxBytes > 0 && int64(base64.StdEncoding.DecodedLen(len(payload))) > maxBytes {
		return ImageAttachment{}, fmt.Errorf("image is too large: maximum is %d bytes", maxBytes)
	}

	data, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return ImageAttachment{}, fmt.Errorf("invalid base64 image data")
	}
	if maxBytes > 0 && int64(len(data)) > maxBytes {
		return ImageAttachment{}, fmt.Errorf("image is too large: maximum is %d bytes", maxBytes)
	}

	return ImageAttachment{
		Name:      fmt.Sprintf("image-%d.%s", index, extension),
		MIME:      mime,
		Extension: extension,
		Data:      data,
	}, nil
}

func hasBase64Flag(parts []string) bool {
	for _, part := range parts {
		if strings.EqualFold(strings.TrimSpace(part), "base64") {
			return true
		}
	}
	return false
}

func supportedImageExtension(mime string) (string, bool) {
	switch mime {
	case "image/png":
		return "png", true
	case "image/jpeg", "image/jpg":
		return "jpg", true
	case "image/webp":
		return "webp", true
	case "image/gif":
		return "gif", true
	default:
		return "", false
	}
}
