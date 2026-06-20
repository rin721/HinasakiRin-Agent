import path from "node:path";
import { Command } from "commander";
import { runAgent } from "./agent/loop.js";
import { MockModel } from "./model/MockModel.js";
import { OpenAICompatibleModel } from "./model/OpenAICompatibleModel.js";
import type { ModelClient } from "./model/ModelClient.js";
import { createToolRegistry } from "./tools/index.js";

type CliOptions = {
  repo: string;
  model: "mock" | "real";
  maxSteps: string;
};

function createModel(name: CliOptions["model"]): ModelClient {
  if (name === "real") {
    return new OpenAICompatibleModel();
  }

  return new MockModel();
}

export async function main(argv = process.argv): Promise<void> {
  const program = new Command();

  program
    .name("mini-coding-agent")
    .description("A teaching-oriented minimal coding agent CLI.")
    .version("0.1.0");

  program
    .command("run")
    .description("Run the agent against a repository.")
    .argument("<task...>", "task for the agent")
    .option("--repo <path>", "repository root to operate on", ".")
    .option("--model <model>", "model adapter to use: mock or real", "mock")
    .option("--max-steps <number>", "maximum agent loop steps", "20")
    .action(async (taskParts: string[], options: CliOptions) => {
      const task = taskParts.join(" ");
      const repoRoot = path.resolve(process.cwd(), options.repo);
      const maxSteps = Number.parseInt(options.maxSteps, 10);

      if (!Number.isFinite(maxSteps) || maxSteps <= 0) {
        throw new Error("--max-steps must be a positive number.");
      }

      if (options.model !== "mock" && options.model !== "real") {
        throw new Error("--model must be either mock or real.");
      }

      const result = await runAgent({
        task,
        repoRoot,
        maxSteps,
        model: createModel(options.model),
        tools: createToolRegistry()
      });

      process.exitCode = result.ok ? 0 : 1;
    });

  await program.parseAsync(argv);
}
