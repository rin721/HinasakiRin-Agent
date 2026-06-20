import { applyPatchTool } from "./applyPatch.js";
import { listFilesTool } from "./listFiles.js";
import { readFileTool } from "./readFile.js";
import { runCommandTool } from "./runCommand.js";
import { searchTool } from "./search.js";
import type { ToolRegistry } from "./types.js";

export function createToolRegistry(): ToolRegistry {
  return {
    list_files: listFilesTool,
    read_file: readFileTool,
    search: searchTool,
    apply_patch: applyPatchTool,
    run_cmd: runCommandTool
  };
}
