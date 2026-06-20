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

type Runner struct {
	binary          string
	safeWorkdir     string
	timeout         time.Duration
	serviceTier     string
	reasoningEffort string
}

type Request struct {
	Prompt string
	Model  string
	Images []ImageAttachment
}

type ImageAttachment struct {
	Name string
	Data []byte
}

type CompletionResult struct {
	Content string
}

type StreamEvent struct {
	Type  string
	Text  string
	Usage *Usage
}

type Usage struct {
	InputTokens           int `json:"input_tokens"`
	CachedInputTokens     int `json:"cached_input_tokens"`
	OutputTokens          int `json:"output_tokens"`
	ReasoningOutputTokens int `json:"reasoning_output_tokens"`
}

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

func (r *Runner) Complete(ctx context.Context, req Request) (CompletionResult, error) {
	execCtx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	imagePaths, cleanup, err := r.writeAttachments(req.Images)
	if err != nil {
		return CompletionResult{}, err
	}
	defer cleanup()

	cmd := exec.CommandContext(execCtx, r.binary, r.buildExecArgs(req.Model, false, imagePaths)...)
	cmd.Stdin = strings.NewReader(req.Prompt)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

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

func (r *Runner) Stream(ctx context.Context, req Request, emit func(StreamEvent) error) error {
	execCtx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	imagePaths, cleanup, err := r.writeAttachments(req.Images)
	if err != nil {
		return err
	}
	defer cleanup()

	cmd := exec.CommandContext(execCtx, r.binary, r.buildExecArgs(req.Model, true, imagePaths)...)
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

	scanner := bufio.NewScanner(stdoutPipe)
	scanner.Buffer(make([]byte, 64*1024), 10*1024*1024)

	var emitErr error
	for scanner.Scan() {
		events := parseJSONLine(scanner.Text())
		for _, event := range events {
			if err := emit(event); err != nil {
				emitErr = err
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

func (r *Runner) buildExecArgs(model string, stream bool, imagePaths []string) []string {
	args := []string{
		"exec",
		"--cd", r.safeWorkdir,
		"--sandbox", "read-only",
		"--config", `approval_policy="never"`,
	}
	if r.serviceTier != "" {
		args = append(args, "--config", fmt.Sprintf("service_tier=%q", r.serviceTier))
	}
	if r.reasoningEffort != "" {
		args = append(args, "--config", fmt.Sprintf("model_reasoning_effort=%q", r.reasoningEffort))
	}
	args = append(args,
		"--ignore-user-config",
		"--ignore-rules",
		"--skip-git-repo-check",
		"--color", "never",
	)
	if model != "" {
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

func stripANSI(value string) string {
	return ansiEscapePattern.ReplaceAllString(value, "")
}

func resolveCodexBinary(binary string) string {
	if runtime.GOOS != "windows" || binary != "codex" {
		return binary
	}

	if path, err := exec.LookPath("codex.cmd"); err == nil {
		return path
	}

	return binary
}

func safeSummary(stderr string, stdout string) string {
	if stderr != "" {
		return summarizeDiagnostics(stderr)
	}
	if stdout != "" {
		return "codex exited with a non-zero status; stdout was omitted because it may include the prompt"
	}
	return "no output"
}

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

func truncate(value string, maxLength int) string {
	value = strings.TrimSpace(value)
	if len(value) <= maxLength {
		return value
	}
	return value[:maxLength] + "... truncated ..."
}
