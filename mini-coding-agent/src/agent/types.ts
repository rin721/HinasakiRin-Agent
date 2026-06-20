import type { ModelClient } from "../model/ModelClient.js";
import type { ExecutableToolName, ToolRegistry } from "../tools/types.js";

export type AgentMessageRole = "system" | "user" | "assistant" | "tool";

export type ToolObservation = {
  ok: boolean;
  summary: string;
  data?: unknown;
  error?: string;
};

export type ToolCallAction = {
  type: "tool_call";
  tool: ExecutableToolName;
  args: Record<string, unknown>;
  reasoning?: string;
};

export type FinalAnswerAction = {
  type: "final_answer";
  answer: string;
};

export type AgentAction = ToolCallAction | FinalAnswerAction;

export type AgentMessage = {
  role: AgentMessageRole;
  content: string;
  toolName?: string;
  action?: AgentAction;
  observation?: ToolObservation;
};

export type AgentRunOptions = {
  task: string;
  repoRoot: string;
  maxSteps?: number;
  model: ModelClient;
  tools: ToolRegistry;
};

export type AgentRunResult = {
  ok: boolean;
  answer: string;
  steps: number;
  messages: AgentMessage[];
};
