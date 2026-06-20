import { createInitialMessages, observationFromError } from "./context.js";
import type { AgentMessage, AgentRunOptions, AgentRunResult, ToolObservation } from "./types.js";
import { logger } from "../utils/logger.js";

function appendAssistantAction(messages: AgentMessage[], action: AgentMessage["action"]): void {
  messages.push({
    role: "assistant",
    content: JSON.stringify(action),
    action
  });
}

function appendToolObservation(
  messages: AgentMessage[],
  toolName: string,
  observation: ToolObservation
): void {
  messages.push({
    role: "tool",
    toolName,
    content: observation.summary,
    observation
  });
}

export async function runAgent(options: AgentRunOptions): Promise<AgentRunResult> {
  const maxSteps = options.maxSteps ?? 20;
  const messages = createInitialMessages(options.task, options.repoRoot);

  logger.info(`Task: ${options.task}`);
  logger.info(`Repo: ${options.repoRoot}`);
  logger.info(`Model: ${options.model.name}`);

  for (let step = 1; step <= maxSteps; step += 1) {
    let action: Awaited<ReturnType<typeof options.model.nextAction>>;

    try {
      action = await options.model.nextAction({
        task: options.task,
        repoRoot: options.repoRoot,
        step,
        messages
      });
    } catch (error: unknown) {
      const observation = observationFromError(error);
      appendToolObservation(messages, "model_error", observation);
      logger.step(step, "model_error", {}, observation.summary);
      logger.info("Model error was added to history. The agent will try again.");
      continue;
    }

    appendAssistantAction(messages, action);

    if (action.type === "final_answer") {
      logger.step(step, "final_answer", { answer: action.answer }, "Agent finished.");
      logger.final(action.answer);

      return {
        ok: true,
        answer: action.answer,
        steps: step,
        messages
      };
    }

    const tool = options.tools[action.tool];
    let observation: ToolObservation;

    if (!tool) {
      observation = {
        ok: false,
        summary: `Unknown tool: ${action.tool}`,
        error: `Unknown tool: ${action.tool}`
      };
    } else {
      const parsed = tool.schema.safeParse(action.args);

      if (!parsed.success) {
        observation = {
          ok: false,
          summary: `Invalid arguments for ${action.tool}: ${parsed.error.message}`,
          error: parsed.error.message
        };
      } else {
        observation = await tool
          .execute(parsed.data, {
            repoRoot: options.repoRoot
          })
          .catch(observationFromError);
      }
    }

    appendToolObservation(messages, action.tool, observation);
    logger.step(step, action.tool, action.args, observation.summary);

    if (!observation.ok) {
      logger.info(`Tool returned a recoverable error. The model will see it in history.`);
    }
  }

  const answer = `Stopped after reaching maxSteps=${maxSteps} before final_answer.`;
  logger.final(answer);

  return {
    ok: false,
    answer,
    steps: maxSteps,
    messages
  };
}
