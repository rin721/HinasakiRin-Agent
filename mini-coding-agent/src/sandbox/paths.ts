import fs from "node:fs/promises";
import path from "node:path";

export const DEFAULT_IGNORE_GLOBS = [
  "**/node_modules/**",
  "**/.git/**",
  "**/dist/**",
  "**/coverage/**"
];

function assertRelativeRequest(requestedPath: string): void {
  if (requestedPath.includes("\0")) {
    throw new Error("Path contains a null byte.");
  }

  if (path.isAbsolute(requestedPath)) {
    throw new Error("Absolute paths are not allowed.");
  }
}

function assertInside(repoRoot: string, targetPath: string): void {
  const relative = path.relative(repoRoot, targetPath);

  if (relative.startsWith("..") || path.isAbsolute(relative)) {
    throw new Error(`Path escapes repo root: ${targetPath}`);
  }
}

export async function resolvePathInsideRepo(
  repoRoot: string,
  requestedPath: string
): Promise<string> {
  assertRelativeRequest(requestedPath);

  const root = path.resolve(repoRoot);
  const rootRealPath = await fs.realpath(root);
  const resolvedPath = path.resolve(rootRealPath, requestedPath);

  assertInside(rootRealPath, resolvedPath);

  try {
    const targetRealPath = await fs.realpath(resolvedPath);
    assertInside(rootRealPath, targetRealPath);
    return targetRealPath;
  } catch {
    return resolvedPath;
  }
}

export function toRepoRelativePath(repoRoot: string, absolutePath: string): string {
  return path.relative(path.resolve(repoRoot), absolutePath).replaceAll(path.sep, "/");
}
