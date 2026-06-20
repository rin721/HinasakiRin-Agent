import type { AgentAction } from "../agent/types.js";
import type { ModelClient, ModelInput } from "./ModelClient.js";

const demoPatch = `--- a/src/add.ts
+++ b/src/add.ts
@@ -1,3 +1,3 @@
 export function add(a: number, b: number): number {
-  return a - b;
+  return a + b;
 }
`;

const scriptedActions: AgentAction[] = [
  {
    type: "tool_call",
    tool: "list_files",
    args: {}
  },
  {
    type: "tool_call",
    tool: "read_file",
    args: { path: "package.json" }
  },
  {
    type: "tool_call",
    tool: "read_file",
    args: { path: "test/add.test.ts" }
  },
  {
    type: "tool_call",
    tool: "read_file",
    args: { path: "src/add.ts" }
  },
  {
    type: "tool_call",
    tool: "run_cmd",
    args: { command: "pnpm test", timeoutMs: 10000 }
  },
  {
    type: "tool_call",
    tool: "apply_patch",
    args: { patch: demoPatch }
  },
  {
    type: "tool_call",
    tool: "run_cmd",
    args: { command: "pnpm test", timeoutMs: 10000 }
  },
  {
    type: "final_answer",
    answer: "The failing test was fixed by changing add() to return a + b, and the test suite now passes."
  }
];

export class MockModel implements ModelClient {
  name = "mock";

  async nextAction(input: ModelInput): Promise<AgentAction> {
    // Count prior assistant actions so this mock behaves like a stateless model reading history.
    const actionCount = input.messages.filter((message) => message.role === "assistant").length;
    return scriptedActions[actionCount] ?? scriptedActions[scriptedActions.length - 1];
  }
}
