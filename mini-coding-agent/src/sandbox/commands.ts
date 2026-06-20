export type AllowedCommand = {
  file: string;
  args: string[];
};

const ALLOWED_COMMANDS: Record<string, AllowedCommand> = {
  "npm test": { file: "npm", args: ["test"] },
  "pnpm test": { file: "pnpm", args: ["test"] },
  "npm run test": { file: "npm", args: ["run", "test"] },
  "pnpm run test": { file: "pnpm", args: ["run", "test"] },
  "npm run build": { file: "npm", args: ["run", "build"] },
  "pnpm run build": { file: "pnpm", args: ["run", "build"] },
  "tsc --noEmit": { file: "tsc", args: ["--noEmit"] }
};

export function normalizeCommand(command: string): string {
  return command.trim().replace(/\s+/g, " ");
}

export function commandToExecutable(command: string): AllowedCommand | undefined {
  return ALLOWED_COMMANDS[command];
}
