import fs from "node:fs/promises";
import { createRequire } from "node:module";
import { z } from "zod";
import { resolvePathInsideRepo, toRepoRelativePath } from "../sandbox/paths.js";
import { logger } from "../utils/logger.js";
import type { ToolDefinition } from "./types.js";

type ParsedFilePatch = {
  oldFileName?: string;
  newFileName?: string;
};

const require = createRequire(import.meta.url);
const { applyPatch: applyTextPatch, parsePatch } = require("diff") as {
  applyPatch: (source: string, patch: ParsedFilePatch) => string | false;
  parsePatch: (patch: string) => ParsedFilePatch[];
};

const applyPatchSchema = z.object({
  patch: z.string().min(1)
});

function cleanPatchPath(fileName: string | undefined): string | undefined {
  if (!fileName || fileName === "/dev/null") {
    return undefined;
  }

  return fileName.replace(/^[ab]\//, "");
}

export const applyPatchTool: ToolDefinition<typeof applyPatchSchema> = {
  name: "apply_patch",
  description: "Apply a unified diff to existing text files in the repository.",
  schema: applyPatchSchema,
  async execute(args, context) {
    logger.info("apply_patch: parsing unified diff.");

    const parsedPatches = parsePatch(args.patch);

    if (parsedPatches.length === 0) {
      return {
        ok: false,
        summary: "Patch parsing failed: no file patches found.",
        error: "No file patches found."
      };
    }

    const writes: Array<{ absolutePath: string; content: string }> = [];
    const changedFiles: string[] = [];

    for (const filePatch of parsedPatches) {
      const oldPath = cleanPatchPath(filePatch.oldFileName);
      const newPath = cleanPatchPath(filePatch.newFileName);

      if (!filePatch.oldFileName && !filePatch.newFileName) {
        return {
          ok: false,
          summary: "Patch parsing failed: missing file paths in unified diff.",
          error: "Missing file paths in unified diff."
        };
      }

      if (!oldPath || !newPath) {
        return {
          ok: false,
          summary: "Patch creates or deletes files, which is not supported in v1.",
          error: "Creating or deleting files is not supported."
        };
      }

      if (oldPath !== newPath) {
        return {
          ok: false,
          summary: "Patch renames files, which is not supported in v1.",
          error: "Renaming files is not supported."
        };
      }

      const absolutePath = await resolvePathInsideRepo(context.repoRoot, newPath);
      const before = await fs.readFile(absolutePath, "utf8");
      const after = applyTextPatch(before, filePatch);

      if (after === false) {
        return {
          ok: false,
          summary: `Patch failed to apply cleanly to ${newPath}.`,
          error: `Patch failed to apply cleanly to ${newPath}.`
        };
      }

      writes.push({ absolutePath, content: after });
      changedFiles.push(toRepoRelativePath(context.repoRoot, absolutePath));
    }

    logger.info(`apply_patch: writing ${writes.length} file(s).`);

    for (const write of writes) {
      await fs.writeFile(write.absolutePath, write.content, "utf8");
    }

    logger.info("apply_patch: done.");

    return {
      ok: true,
      summary: `Applied patch to ${changedFiles.length} file(s): ${changedFiles.join(", ")}.`,
      data: {
        changedFiles
      }
    };
  }
};
