import fg from "fast-glob";
import { z } from "zod";
import { DEFAULT_IGNORE_GLOBS } from "../sandbox/paths.js";
import type { ToolDefinition } from "./types.js";

const listFilesSchema = z.object({
  pattern: z.string().default("**/*"),
  limit: z.number().int().positive().max(1000).default(200)
});

export const listFilesTool: ToolDefinition<typeof listFilesSchema> = {
  name: "list_files",
  description: "List files under the repository root.",
  schema: listFilesSchema,
  async execute(args, context) {
    const files = await fg(args.pattern, {
      cwd: context.repoRoot,
      dot: true,
      onlyFiles: true,
      followSymbolicLinks: false,
      ignore: DEFAULT_IGNORE_GLOBS
    });

    const sorted = files.sort();
    const shown = sorted.slice(0, args.limit);

    return {
      ok: true,
      summary: `Found ${sorted.length} file(s). Showing ${shown.length}.`,
      data: {
        files: shown,
        total: sorted.length
      }
    };
  }
};
