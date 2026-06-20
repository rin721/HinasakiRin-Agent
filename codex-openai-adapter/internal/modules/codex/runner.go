package codex

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	appconfig "codex-openai-adapter/internal/modules/config"
)

// Runner 是 Codex CLI 的薄封装。
//
// 它刻意只做三件事：
// 1. 组装安全的 codex exec / codex debug models 命令。
// 2. 把图片附件写成临时文件，交给 --image 参数。
// 3. 把 Codex stdout / JSONL 转成上层可以理解的结果。
//
// 它不理解 HTTP，也不理解 OpenAI JSON 协议；这些属于 chat.Handler。
type Runner struct {
	binary          string
	safeWorkdir     string
	timeout         time.Duration
	serviceTier     string
	reasoningEffort string
}

// Request 是 chat 层传给 Codex CLI 的“已转换请求”。
// Prompt 已经是纯文本；Images 也已经是通过 data URL 解码后的二进制。
type Request struct {
	Prompt          string
	Model           string
	ReasoningEffort string
	Images          []ImageAttachment
}

// ImageAttachment 是 Runner 要落盘后传给 `codex exec --image` 的图片。
type ImageAttachment struct {
	Name string
	Data []byte
}

// CompletionResult 是非流式 codex exec 的输出。
type CompletionResult struct {
	Content string
}

// StreamEvent 是 Runner 对 Codex JSONL 事件的简化。
// 上层 Handler 不需要知道 Codex 的原始事件结构，只需要 content/done 这种概念。
type StreamEvent struct {
	Type  string
	Text  string
	Usage *Usage
}

// Usage 对应 Codex JSONL 中 turn.completed 里的 usage。
// 当前 HTTP 层暂时不回传 usage，但保留它方便后续扩展。
type Usage struct {
	InputTokens           int `json:"input_tokens"`
	CachedInputTokens     int `json:"cached_input_tokens"`
	OutputTokens          int `json:"output_tokens"`
	ReasoningOutputTokens int `json:"reasoning_output_tokens"`
}

// ModelInfo 是 `codex debug models` 返回 catalog 中对 adapter 有用的字段。
type ModelInfo struct {
	Slug             string   `json:"slug"`
	DisplayName      string   `json:"display_name"`
	Visibility       string   `json:"visibility"`
	SupportedInAPI   bool     `json:"supported_in_api"`
	InputModalities  []string `json:"input_modalities"`
	ContextWindow    int      `json:"context_window"`
	MaxContextWindow int      `json:"max_context_window"`
}

var ansiEscapePattern = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)

// NewRunner 根据配置创建 Runner，并提前准备安全工作目录。
// 这里会拒绝非 codex-workdir 目录，避免把 adapter 误指到用户代码仓库。
func NewRunner(cfg appconfig.CodexConfig) (*Runner, error) {
	workdir, err := prepareSafeWorkdir(cfg.SafeWorkdir)
	if err != nil {
		return nil, err
	}

	return &Runner{
		binary:          resolveCodexBinary(cfg.Binary),
		safeWorkdir:     workdir,
		timeout:         time.Duration(cfg.TimeoutSecs) * time.Second,
		serviceTier:     cfg.ServiceTier,
		reasoningEffort: cfg.ModelReasoningEffort,
	}, nil
}

// Complete 执行非流式请求。
//
// 映射关系：
// OpenAI non-streaming request -> codex exec -> stdout -> assistant.content
//
// prompt 通过 stdin 输入，避免出现在命令行参数、shell history 或进程列表中。
func (r *Runner) Complete(ctx context.Context, req Request) (CompletionResult, error) {
	execCtx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	// Codex CLI 的 --image 需要文件路径，所以先把内存图片写到安全临时目录。
	// cleanup 用 defer 保证请求结束后删除附件。
	imagePaths, cleanup, err := r.writeAttachments(req.Images)
	if err != nil {
		return CompletionResult{}, err
	}
	defer cleanup()

	cmd := exec.CommandContext(execCtx, r.binary, r.buildExecArgs(req.Model, req.ReasoningEffort, false, imagePaths)...)
	cmd.Stdin = strings.NewReader(req.Prompt)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// cmd.Run 会等待进程结束。超时由 exec.CommandContext 的 context 控制。
	err = cmd.Run()
	cleanStdout := strings.TrimSpace(stripANSI(stdout.String()))
	cleanStderr := strings.TrimSpace(stripANSI(stderr.String()))

	if errors.Is(execCtx.Err(), context.DeadlineExceeded) {
		return CompletionResult{}, fmt.Errorf("codex exec timed out after %s", r.timeout)
	}

	if err != nil {
		return CompletionResult{}, fmt.Errorf("codex exec failed: %s", safeSummary(cleanStderr, cleanStdout))
	}

	if cleanStdout == "" && cleanStderr != "" {
		return CompletionResult{}, fmt.Errorf("codex exec returned no stdout: %s", truncate(cleanStderr, 1000))
	}

	if cleanStdout == "" {
		return CompletionResult{}, fmt.Errorf("codex exec returned empty stdout")
	}

	return CompletionResult{Content: cleanStdout}, nil
}

// Stream 执行流式请求。
//
// Codex CLI 的流式模式不是 OpenAI SSE，而是 JSONL：
//
//	{"type":"item.completed", ...}
//	{"type":"turn.completed", ...}
//
// Runner 在这里逐行读取 JSONL，把有用事件翻译成 StreamEvent，再交给 Handler 生成 SSE。
func (r *Runner) Stream(ctx context.Context, req Request, emit func(StreamEvent) error) error {
	execCtx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	imagePaths, cleanup, err := r.writeAttachments(req.Images)
	if err != nil {
		return err
	}
	defer cleanup()

	cmd := exec.CommandContext(execCtx, r.binary, r.buildExecArgs(req.Model, req.ReasoningEffort, true, imagePaths)...)
	cmd.Stdin = strings.NewReader(req.Prompt)

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("open codex stdout pipe: %w", err)
	}

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start codex exec: %w", err)
	}

	// 默认 Scanner token 太小，长消息可能被截断；这里提升到 10MB。
	scanner := bufio.NewScanner(stdoutPipe)
	scanner.Buffer(make([]byte, 64*1024), 10*1024*1024)

	var emitErr error
	for scanner.Scan() {
		events := parseJSONLine(scanner.Text())
		for _, event := range events {
			if err := emit(event); err != nil {
				emitErr = err
				// 如果客户端断开或写 SSE 失败，停止底层 Codex 进程，避免后台继续跑。
				if cmd.Process != nil {
					_ = cmd.Process.Kill()
				}
				break
			}
		}
		if emitErr != nil {
			break
		}
	}

	scanErr := scanner.Err()
	waitErr := cmd.Wait()
	cleanStderr := strings.TrimSpace(stripANSI(stderr.String()))

	if emitErr != nil {
		return emitErr
	}
	if errors.Is(execCtx.Err(), context.DeadlineExceeded) {
		return fmt.Errorf("codex exec timed out after %s", r.timeout)
	}
	if scanErr != nil {
		return fmt.Errorf("read codex stream: %w", scanErr)
	}
	if waitErr != nil {
		return fmt.Errorf("codex exec failed: %s", safeSummary(cleanStderr, ""))
	}

	return nil
}

// Models 读取 Codex CLI 的模型目录。
// 这个命令不会调用模型推理，只是把 CLI 看到的 catalog 转成结构体。
func (r *Runner) Models(ctx context.Context) ([]ModelInfo, error) {
	execCtx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	cmd := exec.CommandContext(execCtx, r.binary, "debug", "models")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("codex debug models failed: %s", safeSummary(stderr.String(), stdout.String()))
	}

	var catalog struct {
		Models []ModelInfo `json:"models"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &catalog); err != nil {
		return nil, fmt.Errorf("parse codex model catalog: %w", err)
	}

	return catalog.Models, nil
}

// buildExecArgs 是安全边界最集中的地方。
//
// 所有 codex exec 请求都必须：
// - 在独立 codex-workdir 中运行，而不是用户项目目录。
// - 使用 read-only sandbox。
// - 使用 approval_policy="never"，避免非交互服务卡在确认提示。
// - 通过 stdin 读取 prompt。
//
// model、reasoning、stream、image 都是可选能力，在这里追加。
func (r *Runner) buildExecArgs(model string, reasoningEffort string, stream bool, imagePaths []string) []string {
	args := []string{
		"exec",
		"--cd", r.safeWorkdir,
		"--sandbox", "read-only",
		"--config", `approval_policy="never"`,
	}
	if r.serviceTier != "" {
		// service_tier 用配置固定下来，避免继承用户本机不兼容的 Codex 配置。
		args = append(args, "--config", fmt.Sprintf("service_tier=%q", r.serviceTier))
	}
	if reasoningEffort == "" {
		// 请求里的 reasoning_effort 优先；没有时才使用 config.yaml 的默认值。
		reasoningEffort = r.reasoningEffort
	}
	if reasoningEffort != "" {
		args = append(args, "--config", fmt.Sprintf("model_reasoning_effort=%q", reasoningEffort))
	}
	args = append(args,
		"--ignore-user-config",
		"--ignore-rules",
		"--skip-git-repo-check",
		"--color", "never",
	)
	if model != "" {
		// model 为空时不传 --model，让 Codex CLI 使用自己的默认模型。
		args = append(args, "--model", model)
	}
	if stream {
		args = append(args, "--json")
	}
	for _, path := range imagePaths {
		args = append(args, "--image", path)
	}
	return append(args, "-")
}

// writeAttachments 把图片写入 codex-workdir/attachments/<request>/。
//
// 为什么不直接让客户端传本地路径？
// 因为 adapter 不应该替客户端读取任意本地文件。我们只接受请求体里的 base64 图片，
// 写入受控目录，再把这个受控路径交给 Codex CLI。
func (r *Runner) writeAttachments(images []ImageAttachment) ([]string, func(), error) {
	if len(images) == 0 {
		return nil, func() {}, nil
	}

	dir := filepath.Join(r.safeWorkdir, "attachments", fmt.Sprintf("%d", time.Now().UnixNano()))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, func() {}, fmt.Errorf("create image attachment directory: %w", err)
	}

	cleanup := func() {
		_ = os.RemoveAll(dir)
	}

	paths := make([]string, 0, len(images))
	for i, image := range images {
		// filepath.Base 防御性地去掉潜在路径片段，确保文件只会写在 attachments 目录下。
		name := filepath.Base(image.Name)
		if name == "." || name == string(filepath.Separator) || name == "" {
			name = fmt.Sprintf("image-%d", i+1)
		}
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, image.Data, 0o600); err != nil {
			cleanup()
			return nil, func() {}, fmt.Errorf("write image attachment: %w", err)
		}
		paths = append(paths, path)
	}

	return paths, cleanup, nil
}

// parseJSONLine 把 Codex CLI --json 输出的一行转换成内部 StreamEvent。
// 当前只关心两类事件：
// - item.completed + agent_message：assistant 文本内容
// - turn.completed：本轮结束和 usage
func parseJSONLine(line string) []StreamEvent {
	var envelope struct {
		Type string `json:"type"`
		Item struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"item"`
		Usage *Usage `json:"usage"`
	}
	if err := json.Unmarshal([]byte(line), &envelope); err != nil {
		return nil
	}

	switch envelope.Type {
	case "item.completed":
		if envelope.Item.Type == "agent_message" && envelope.Item.Text != "" {
			return []StreamEvent{{Type: "content", Text: envelope.Item.Text}}
		}
	case "turn.completed":
		return []StreamEvent{{Type: "done", Usage: envelope.Usage}}
	}

	return nil
}

// prepareSafeWorkdir 准备 Codex CLI 的工作目录。
//
// 这里故意要求目录名必须是 codex-workdir，并且不能是 git repo。
// 这个 adapter 的目标是“让 Codex 充当模型后端”，不是让 Codex 在用户项目里改文件。
func prepareSafeWorkdir(rawPath string) (string, error) {
	absolutePath, err := filepath.Abs(rawPath)
	if err != nil {
		return "", fmt.Errorf("resolve codex.safe_workdir: %w", err)
	}

	if filepath.Base(absolutePath) != "codex-workdir" {
		return "", fmt.Errorf("codex.safe_workdir must point to a directory named codex-workdir")
	}

	if _, err := os.Stat(filepath.Join(absolutePath, ".git")); err == nil {
		return "", fmt.Errorf("codex.safe_workdir must not be a git repository")
	}

	if err := os.MkdirAll(absolutePath, 0o755); err != nil {
		return "", fmt.Errorf("create codex.safe_workdir: %w", err)
	}

	return absolutePath, nil
}

// stripANSI 清理 Codex CLI 输出中的颜色控制符，避免 HTTP 响应里混入终端转义字符。
func stripANSI(value string) string {
	return ansiEscapePattern.ReplaceAllString(value, "")
}

// resolveCodexBinary 处理 Windows 上的一个细节：
// `codex.ps1` 可能吞掉或改写参数，`codex.cmd` 对 Go exec 更稳定。
func resolveCodexBinary(binary string) string {
	if runtime.GOOS != "windows" || binary != "codex" {
		return binary
	}

	if path, err := exec.LookPath("codex.cmd"); err == nil {
		return path
	}

	return binary
}

// safeSummary 生成安全错误摘要。
// stdout 可能包含用户 prompt 或模型输出，因此非 0 退出时默认不回显 stdout。
func safeSummary(stderr string, stdout string) string {
	if stderr != "" {
		return summarizeDiagnostics(stderr)
	}
	if stdout != "" {
		return "codex exited with a non-zero status; stdout was omitted because it may include the prompt"
	}
	return "no output"
}

// summarizeDiagnostics 只保留 stderr 中看起来像 WARN/ERROR 的行。
// 这样能给调用方足够的诊断信息，同时降低泄露 prompt 或本地环境信息的概率。
func summarizeDiagnostics(output string) string {
	lines := strings.Split(stripANSI(output), "\n")
	kept := make([]string, 0, len(lines))

	for _, line := range lines {
		line = strings.TrimSpace(line)
		lower := strings.ToLower(line)
		if line == "" {
			continue
		}
		if strings.Contains(lower, "error") || strings.Contains(lower, "warn") {
			kept = append(kept, line)
		}
	}

	if len(kept) == 0 {
		return "codex returned diagnostics, but they were omitted because they may include the prompt"
	}

	return truncate(strings.Join(kept, "\n"), 1000)
}

// truncate 控制错误信息长度，避免把过长诊断直接塞进 HTTP 响应。
func truncate(value string, maxLength int) string {
	value = strings.TrimSpace(value)
	if len(value) <= maxLength {
		return value
	}
	return value[:maxLength] + "... truncated ..."
}
