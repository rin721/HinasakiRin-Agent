import { OpenAICompatibleModel } from "./OpenAICompatibleModel.js";

// Backward-compatible alias for older notes that referenced OpenAIModel.
export class OpenAIModel extends OpenAICompatibleModel {
  name = "real";
}
