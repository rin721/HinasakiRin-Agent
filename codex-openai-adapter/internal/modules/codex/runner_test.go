package codex

import (
	"strings"
	"testing"
	"time"
)

func TestStripANSI(t *testing.T) {
	got := stripANSI("\x1b[31mhello\x1b[0m")
	if got != "hello" {
		t.Fatalf("expected ANSI to be stripped, got %q", got)
	}
}

func TestTruncate(t *testing.T) {
	got := truncate("abcdef", 3)
	if got != "abc... truncated ..." {
		t.Fatalf("unexpected truncation: %q", got)
	}
}

func TestSafeSummaryOmitsStdout(t *testing.T) {
	got := safeSummary("", "user prompt should not be echoed")
	if got == "user prompt should not be echoed" {
		t.Fatalf("safe summary leaked stdout")
	}
}

func TestSafeSummaryFiltersPromptFromStderr(t *testing.T) {
	got := safeSummary("user\nsecret prompt\nERROR: useful diagnostic", "")
	if strings.Contains(got, "secret prompt") {
		t.Fatalf("safe summary leaked prompt: %q", got)
	}
	if !strings.Contains(got, "ERROR: useful diagnostic") {
		t.Fatalf("safe summary dropped diagnostic: %q", got)
	}
}

func TestBuildExecArgsIncludesModelStreamAndImages(t *testing.T) {
	runner := &Runner{
		safeWorkdir:     "C:/tmp/codex-workdir",
		timeout:         time.Second,
		serviceTier:     "fast",
		reasoningEffort: "high",
	}

	args := runner.buildExecArgs("gpt-5.4-mini", "", true, []string{"C:/tmp/codex-workdir/attachments/1/image-1.png"})
	joined := strings.Join(args, "\n")

	for _, expected := range []string{
		"exec",
		"--cd\nC:/tmp/codex-workdir",
		"--sandbox\nread-only",
		"--config\napproval_policy=\"never\"",
		"--config\nservice_tier=\"fast\"",
		"--config\nmodel_reasoning_effort=\"high\"",
		"--model\ngpt-5.4-mini",
		"--json",
		"--image\nC:/tmp/codex-workdir/attachments/1/image-1.png",
		"-",
	} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("args omitted %q: %#v", expected, args)
		}
	}
}

func TestBuildExecArgsRequestReasoningOverridesDefault(t *testing.T) {
	runner := &Runner{
		safeWorkdir:     "C:/tmp/codex-workdir",
		timeout:         time.Second,
		reasoningEffort: "medium",
	}

	args := runner.buildExecArgs("gpt-5.4-mini", "xhigh", false, nil)
	joined := strings.Join(args, "\n")
	if !strings.Contains(joined, "--config\nmodel_reasoning_effort=\"xhigh\"") {
		t.Fatalf("request reasoning effort did not override default: %#v", args)
	}
	if strings.Contains(joined, "model_reasoning_effort=\"medium\"") {
		t.Fatalf("default reasoning effort leaked after request override: %#v", args)
	}
}

func TestParseJSONLineEmitsAgentMessage(t *testing.T) {
	events := parseJSONLine(`{"type":"item.completed","item":{"type":"agent_message","text":"hello"}}`)
	if len(events) != 1 || events[0].Type != "content" || events[0].Text != "hello" {
		t.Fatalf("unexpected events: %#v", events)
	}
}

func TestParseJSONLineEmitsDone(t *testing.T) {
	events := parseJSONLine(`{"type":"turn.completed","usage":{"input_tokens":1,"output_tokens":2}}`)
	if len(events) != 1 || events[0].Type != "done" || events[0].Usage == nil || events[0].Usage.OutputTokens != 2 {
		t.Fatalf("unexpected events: %#v", events)
	}
}
