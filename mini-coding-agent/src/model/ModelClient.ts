import type { AgentAction, AgentMessage } from "../agent/types.js";

export type ModelInput = {
  task: string;
  repoRoot: string;
  step: number;
  messages: AgentMessage[];
};

export interface ModelClient {
  name: string;
  nextAction(input: ModelInput): Promise<AgentAction>;
}

export class ModelOutputError extends Error {
  observationData?: unknown;

  constructor(message: string, observationData?: unknown) {
    super(message);
    this.name = "ModelOutputError";
    this.observationData = observationData;
  }
}
