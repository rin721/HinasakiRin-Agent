export const logger = {
  info(message: string): void {
    console.log(`[info] ${message}`);
  },

  step(step: number, tool: string, args: Record<string, unknown>, summary: string): void {
    console.log("");
    console.log(`Step ${step}`);
    console.log(`selected tool: ${tool}`);
    console.log(`tool args: ${JSON.stringify(args, null, 2)}`);
    console.log(`observation summary: ${summary}`);
  },

  final(message: string): void {
    console.log("");
    console.log("final_answer");
    console.log(message);
  }
};
