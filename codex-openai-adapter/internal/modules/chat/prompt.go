package chat

import (
	"encoding/base64"
	"fmt"
	"strings"
)

// adapterSystemGuard 是每次交给 Codex CLI 的“适配器系统提示词”。
//
// 它有两个目的：
//  1. 安全边界：Codex CLI 在这个 adapter 中只应该充当模型后端，不应该自己读写项目文件。
//  2. Agent 兼容：如果上游 mini-coding-agent 要求输出 JSON tool action，Codex 不应该拒绝，
//     而是按协议输出 JSON，让上游 agent loop 去决定是否执行工具。
const adapterSystemGuard = `You are being used through a local OpenAI-compatible adapter.
You are acting as the model behind another agent loop.
Do not directly inspect or modify local files from this adapter.
Do not directly run shell commands from this adapter.
If the conversation asks you to output a JSON action or tool call, output that JSON action exactly.
It is okay to describe or request tool actions as JSON; the upstream agent will decide whether to execute them.
Do not refuse only because the task mentions files, patches, repositories, or commands.
Only answer the latest user request.
If the user asks for JSON only, return JSON only.`

// ImageLimits 是多模态输入的安全阀。
// 不限制图片数量和体积的话，base64 请求体很容易让本地服务内存暴涨。
type ImageLimits struct {
	MaxImages     int
	MaxImageBytes int64
}

// PromptInput 是 OpenAI 请求转换后的 Codex CLI 输入。
//
// Prompt 会从 messages 和 text parts 拼出来；
// Images 会写成临时文件，并通过 `codex exec --image <file>` 传给 Codex。
type PromptInput struct {
	Prompt string
	Images []ImageAttachment
}

// ImageAttachment 是“还没落盘”的图片附件。
// chat 模块只负责解析和校验，真正写入临时文件由 codex.Runner 负责。
type ImageAttachment struct {
	Name      string
	MIME      string
	Extension string
	Data      []byte
}

// BuildPrompt 把 OpenAI messages 转成 Codex CLI 更容易理解的一段文本。
//
// 学习重点：
// OpenAI API 是结构化 messages；Codex CLI 的 stdin 是一段 prompt。
// adapter 的核心职责就是在这两种世界之间做“保守转换”：
// - role 被保留成 <user>...</user> 这样的标签，避免上下文丢失。
// - text part 直接写入 prompt。
// - image_url 不把 base64 塞进 prompt，只留下占位文字，真实图片通过 --image 传递。
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

					// OpenAI 的 image_url 可以是远程 URL，也可以是 data URL。
					// 为了本地安全和可复现，第一版只接受 data URL，不做任何网络下载。
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

// parseDataImageURL 解析 `data:image/png;base64,...` 这类内嵌图片。
//
// 这里故意不支持：
// - https://... 远程图片，因为那会引入网络访问和下载安全问题。
// - file://... 或本地路径，因为 adapter 不应该替客户端读取任意本地文件。
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

	// 先用 DecodedLen 做一次“解码前估算”，可以尽早拒绝明显超大的 payload。
	// 解码后再用真实长度复查一次，避免 base64 padding 造成估算误差。
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

// hasBase64Flag 检查 data URL metadata 中是否声明了 base64。
func hasBase64Flag(parts []string) bool {
	for _, part := range parts {
		if strings.EqualFold(strings.TrimSpace(part), "base64") {
			return true
		}
	}
	return false
}

// supportedImageExtension 把允许的 MIME 映射到临时文件扩展名。
// Codex CLI 的 --image 参数接收文件路径，扩展名能帮助下游更稳地识别格式。
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
