import type { z } from "zod";
import type { ToolObservation } from "../agent/types.js";

export type ExecutableToolName = "list_files" | "read_file" | "search" | "apply_patch" | "run_cmd";

export type ToolContext = {
  repoRoot: string;
};

export type ToolDefinition<TSchema extends z.ZodTypeAny = z.ZodTypeAny> = {
  name: ExecutableToolName;
  description: string;
  schema: TSchema;
  execute: (args: z.infer<TSchema>, context: ToolContext) => Promise<ToolObservation>;
};

export type AnyToolDefinition = {
  name: ExecutableToolName;
  description: string;
  schema: z.ZodTypeAny;
  execute: (args: any, context: ToolContext) => Promise<ToolObservation>;
};

export type ToolRegistry = Record<ExecutableToolName, AnyToolDefinition>;
