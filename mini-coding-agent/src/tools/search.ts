import fs from "node:fs/promises";
import path from "node:path";
import fg from "fast-glob";
import { z } from "zod";
import { DEFAULT_IGNORE_GLOBS } from "../sandbox/paths.js";
import type { ToolDefinition } from "./types.js";

const searchSchema = z.object({
  query: z.string().min(1),
  caseSensitive: z.boolean().default(false),
  limit: z.number().int().positive().max(500).default(50)
});

export const searchTool: ToolDefinition<typeof searchSchema> = {
  name: "search",
  description: "Search for plain text inside repository files.",
  schema: searchSchema,
  async execute(args, context) {
    const files = await fg("**/*", {
      cwd: context.repoRoot,
      dot: true,
      onlyFiles: true,
      followSymbolicLinks: false,
      ignore: DEFAULT_IGNORE_GLOBS
    });

    const needle = args.caseSensitive ? args.query : args.query.toLowerCase();
    const matches: Array<{ path: string; line: number; text: string }> = [];

    for (const file of files.sort()) {
      if (matches.length >= args.limit) {
        break;
      }

      let content: string;
      try {
        content = await fs.readFile(path.join(context.repoRoot, file), "utf8");
      } catch {
        continue;
      }

      const lines = content.split(/\r?\n/);
      for (let index = 0; index < lines.length; index += 1) {
        const line = lines[index] ?? "";
        const haystack = args.caseSensitive ? line : line.toLowerCase();

        if (haystack.includes(needle)) {
          matches.push({
            path: file,
            line: index + 1,
            text: line
          });
        }

        if (matches.length >= args.limit) {
          break;
        }
      }
    }

    return {
      ok: true,
      summary: `Found ${matches.length} match(es) for "${args.query}".`,
      data: {
        matches
      }
    };
  }
};
