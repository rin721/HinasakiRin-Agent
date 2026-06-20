import type { AgentMessage, ToolObservation } from "./types.js";

export function createInitialMessages(task: string, repoRoot: string): AgentMessage[] {
  return [
    {
      role: "system",
      content:
        "You are a minimal coding agent. Choose one tool call at a time, observe the result, then continue until final_answer."
    },
    {
      role: "user",
      content: `Task: ${task}\nRepo root: ${repoRoot}`
    }
  ];
}

export function observationFromError(error: unknown): ToolObservation {
  const message = error instanceof Error ? error.message : String(error);
  const maybeData =
    typeof error === "object" && error !== null && "observationData" in error
      ? (error as { observationData?: unknown }).observationData
      : undefined;

  return {
    ok: false,
    summary: message,
    data: maybeData,
    error: message
  };
}

export function summarizeArgs(args: Record<string, unknown>): string {
  return JSON.stringify(args, null, 2);
}
