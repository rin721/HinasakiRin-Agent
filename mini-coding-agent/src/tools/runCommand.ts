import { execa } from "execa";
import { z } from "zod";
import { commandToExecutable, normalizeCommand } from "../sandbox/commands.js";
import type { ToolDefinition } from "./types.js";

const runCommandSchema = z.object({
  command: z.string().min(1),
  timeoutMs: z.number().int().positive().max(30_000).default(10_000)
});

function outputSnippet(text: string, maxLength = 2000): string {
  if (text.length <= maxLength) {
    return text;
  }

  return `${text.slice(0, maxLength)}\n... output truncated ...`;
}

export const runCommandTool: ToolDefinition<typeof runCommandSchema> = {
  name: "run_cmd",
  description: "Run an allowlisted command in the repository root.",
  schema: runCommandSchema,
  async execute(args, context) {
    const normalized = normalizeCommand(args.command);
    const executable = commandToExecutable(normalized);

    if (!executable) {
      return {
        ok: false,
        summary: `Command is not allowlisted: ${args.command}`,
        error: "Command is not allowlisted."
      };
    }

    try {
      const result = await execa(executable.file, executable.args, {
        cwd: context.repoRoot,
        reject: false,
        timeout: args.timeoutMs,
        preferLocal: true,
        all: true
      });

      const output = outputSnippet(result.all ?? `${result.stdout}\n${result.stderr}`.trim());

      return {
        ok: result.exitCode === 0,
        summary: `Command "${normalized}" exited with ${result.exitCode}.`,
        error: result.exitCode === 0 ? undefined : output,
        data: {
          command: normalized,
          exitCode: result.exitCode,
          output
        }
      };
    } catch (error: unknown) {
      const message = error instanceof Error ? error.message : String(error);

      return {
        ok: false,
        summary: `Command "${normalized}" failed: ${message}`,
        error: message
      };
    }
  }
};
