import fs from "node:fs/promises";
import { z } from "zod";
import { resolvePathInsideRepo, toRepoRelativePath } from "../sandbox/paths.js";
import type { ToolDefinition } from "./types.js";

const readFileSchema = z.object({
  path: z.string().min(1),
  maxBytes: z.number().int().positive().max(200_000).default(60_000)
});

export const readFileTool: ToolDefinition<typeof readFileSchema> = {
  name: "read_file",
  description: "Read a text file inside the repository.",
  schema: readFileSchema,
  async execute(args, context) {
    const absolutePath = await resolvePathInsideRepo(context.repoRoot, args.path);
    const stat = await fs.stat(absolutePath);

    if (!stat.isFile()) {
      return {
        ok: false,
        summary: `${args.path} is not a file.`,
        error: `${args.path} is not a file.`
      };
    }

    if (stat.size > args.maxBytes) {
      return {
        ok: false,
        summary: `${args.path} is too large (${stat.size} bytes, max ${args.maxBytes}).`,
        error: "File is too large."
      };
    }

    const content = await fs.readFile(absolutePath, "utf8");

    return {
      ok: true,
      summary: `Read ${toRepoRelativePath(context.repoRoot, absolutePath)} (${content.length} chars).`,
      data: {
        path: toRepoRelativePath(context.repoRoot, absolutePath),
        content
      }
    };
  }
};
