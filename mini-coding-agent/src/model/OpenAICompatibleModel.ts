import OpenAI from "openai";
import type { ChatCompletionMessageParam } from "openai/resources/chat/completions";
import { z } from "zod";
import type { AgentAction, AgentMessage } from "../agent/types.js";
import type { ExecutableToolName } from "../tools/types.js";
import { ModelOutputError, type ModelClient, type ModelInput } from "./ModelClient.js";

const actionSchema = z
  .object({
    tool: z.enum(["list_files", "read_file", "search", "apply_patch", "run_cmd", "final_answer"]),
    args: z.record(z.unknown())
  })
  .strict();

const finalAnswerArgsSchema = z
  .object({
    answer: z.string().min(1)
  })
  .strict();

const executableTools = new Set<string>([
  "list_files",
  "read_file",
  "search",
  "apply_patch",
  "run_cmd"
]);

const SYSTEM_PROMPT = `You are the model inside a minimal coding agent.

You must output exactly one raw JSON object and nothing else.
Do not wrap JSON in Markdown fences.
Do not explain your reasoning outside JSON.

The JSON format is:
{
  "tool": "list_files" | "read_file" | "search" | "apply_patch" | "run_cmd" | "final_answer",
  "args": {}
}

Tool args:
- list_files: { "pattern"?: string, "limit"?: number }
- read_file: { "path": string }
- search: { "query": string, "caseSensitive"?: boolean, "limit"?: number }
- apply_patch: { "patch": string }
- run_cmd: { "command": string, "timeoutMs"?: number }
- final_answer: { "answer": string }

Use one tool at a time. After each tool result, choose the next JSON action.`;

function requireEnv(name: string): string {
  const value = process.env[name]?.trim();

  if (!value) {
    throw new Error(`Missing required environment variable: ${name}`);
  }

  return value;
}

function truncate(value: string, maxLength: number): string {
  if (value.length <= maxLength) {
    return value;
  }

  return `${value.slice(0, maxLength)}\n... truncated ...`;
}

function safeJson(value: unknown, maxLength: number): string {
  try {
    return truncate(JSON.stringify(value, null, 2), maxLength);
  } catch {
    return "[unserializable]";
  }
}

function formatHistoryMessage(message: AgentMessage): string {
  if (message.role === "assistant" && message.action) {
    return `assistant_action:\n${safeJson(message.action, 4000)}`;
  }

  if (message.role === "tool") {
    return `tool_observation (${message.toolName ?? "unknown"}):
summary: ${message.observation?.summary ?? message.content}
ok: ${message.observation?.ok ?? "unknown"}
data: ${safeJson(message.observation?.data, 6000)}
error: ${message.observation?.error ?? ""}`;
  }

  return `${message.role}:\n${message.content}`;
}

function buildChatMessages(input: ModelInput): ChatCompletionMessageParam[] {
  const history = input.messages.map(formatHistoryMessage).join("\n\n---\n\n");

  return [
    {
      role: "system",
      content: SYSTEM_PROMPT
    },
    {
      role: "user",
      content: `Task: ${input.task}
Repo root: ${input.repoRoot}
Current step: ${input.step}

History:
${truncate(history, 30_000)}

Choose the next action as strict JSON only.`
    }
  ];
}

function parseModelAction(rawContent: string): AgentAction {
  let parsedJson: unknown;

  try {
    parsedJson = JSON.parse(rawContent.trim());
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error);
    throw new ModelOutputError(`Model returned invalid JSON: ${message}`, {
      rawResponse: truncate(rawContent, 1000)
    });
  }

  const parsedAction = actionSchema.safeParse(parsedJson);

  if (!parsedAction.success) {
    throw new ModelOutputError("Model JSON did not match the action schema.", {
      rawResponse: truncate(rawContent, 1000),
      issues: parsedAction.error.issues
    });
  }

  if (parsedAction.data.tool === "final_answer") {
    const parsedArgs = finalAnswerArgsSchema.safeParse(parsedAction.data.args);

    if (!parsedArgs.success) {
      throw new ModelOutputError("final_answer requires args.answer to be a non-empty string.", {
        rawResponse: truncate(rawContent, 1000),
        issues: parsedArgs.error.issues
      });
    }

    return {
      type: "final_answer",
      answer: parsedArgs.data.answer
    };
  }

  if (!executableTools.has(parsedAction.data.tool)) {
    throw new ModelOutputError(`Unsupported tool: ${parsedAction.data.tool}`, {
      rawResponse: truncate(rawContent, 1000)
    });
  }

  return {
    type: "tool_call",
    tool: parsedAction.data.tool as ExecutableToolName,
    args: parsedAction.data.args
  };
}

export class OpenAICompatibleModel implements ModelClient {
  name = "real";

  private readonly client: OpenAI;
  private readonly model: string;

  constructor() {
    const baseURL = requireEnv("MODEL_BASE_URL");
    const apiKey = process.env.MODEL_API_KEY?.trim() || "dummy-key";

    this.model = requireEnv("MODEL_NAME");
    this.client = new OpenAI({
      baseURL,
      apiKey
    });
  }

  async nextAction(input: ModelInput): Promise<AgentAction> {
    const completion = await this.client.chat.completions.create({
      model: this.model,
      messages: buildChatMessages(input),
      temperature: 0
    });

    const rawContent = completion.choices[0]?.message?.content;

    if (!rawContent) {
      throw new ModelOutputError("Model response did not include message content.", {
        responseId: completion.id
      });
    }

    return parseModelAction(rawContent);
  }
}
