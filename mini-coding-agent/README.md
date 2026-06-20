# mini-coding-agent

`mini-coding-agent` 是一个教学型 TypeScript MVP，用最少的代码演示 coding agent 的核心机制：模型选择动作，agent loop 执行工具，把工具观察结果写回历史，然后继续下一步推理。

它不是商业级 agent，也不包含长期记忆、多 agent、GUI、向量数据库或任意 shell 执行。第一版保留 `MockModel`，所以不需要真实 LLM 或 API key 就能跑完整 demo。现在也提供 `OpenAICompatibleModel`，可以接入 Ollama、LM Studio、Gemini OpenAI-compatible API、未来的 OpenAI API，以及其他兼容 OpenAI Chat Completions 的服务。

## Coding Agent 核心循环

一个 coding agent 的最小循环是：

1. 用户给出任务。
2. 模型根据当前 history 选择下一步 action。
3. loop 校验 action 参数。
4. loop 调用工具，例如读取文件、搜索文本、运行测试、应用 patch。
5. 工具返回 observation。
6. loop 把 observation 追加到 messages/history。
7. 模型基于新的 history 继续选择动作。
8. 当模型返回 `final_answer` 时结束。

`MockModel` 会模拟真实模型，一步一步返回：

```text
list_files
read_file package.json
read_file test/add.test.ts
read_file src/add.ts
run_cmd pnpm test
apply_patch
run_cmd pnpm test
final_answer
```

注意：`MockModel` 不会直接修改文件。它只会像真实 LLM 一样选择工具调用，真正的文件修改由 `apply_patch` 工具完成。

## 安装

```bash
cd mini-coding-agent
pnpm install
```

## 运行 Mock Demo

```bash
pnpm dev run "fix the failing test" --repo examples/broken-project --model mock
```

你会看到每一步都打印：

- step number
- selected tool
- tool args
- observation summary

demo 项目里 `src/add.ts` 一开始故意写错，第一次测试会失败。agent 会读取代码和测试，用 patch 修复实现，然后再次运行测试并输出 `final_answer`。

## 接入真实模型

真实模型路径使用 `src/model/OpenAICompatibleModel.ts`，它通过 `openai` npm package 调用 `chat.completions.create`。这里特意使用 OpenAI-compatible Chat Completions，而不是 OpenAI 专属能力，这样同一套 adapter 可以连接本地模型和云端兼容服务。

模型必须只输出原始 JSON，不要输出 Markdown、解释文字或代码块：

```json
{
  "tool": "list_files",
  "args": {}
}
```

完成任务时输出：

```json
{
  "tool": "final_answer",
  "args": {
    "answer": "The failing test is fixed."
  }
}
```

运行真实模型：

```bash
pnpm dev run "fix the failing test" --repo examples/broken-project --model real
```

本地小模型可能需要多试几次，因为这个教学 MVP 要求模型严格输出 JSON。如果模型输出了 Markdown 代码块或解释文字，agent 会把 JSON 解析错误作为 `model_error` observation 追加到 history，然后继续下一步，而不是直接崩溃。

### A. Ollama

```bash
ollama pull qwen2.5-coder:7b
export MODEL_BASE_URL=http://localhost:11434/v1
export MODEL_API_KEY=ollama
export MODEL_NAME=qwen2.5-coder:7b
```

### B. LM Studio

先在 LM Studio 中启动 Local Server，然后设置：

```bash
export MODEL_BASE_URL=http://localhost:1234/v1
export MODEL_API_KEY=lm-studio
export MODEL_NAME=本地加载的模型名
```

### C. Gemini

```bash
export MODEL_BASE_URL=https://generativelanguage.googleapis.com/v1beta/openai/
export MODEL_API_KEY=你的 Gemini API Key
export MODEL_NAME=gemini-2.5-flash
```

如果 `MODEL_API_KEY` 为空，代码会使用 `"dummy-key"`，这是为了兼容 OpenAI SDK 对 api key 字段的要求。`MODEL_BASE_URL` 和 `MODEL_NAME` 必须设置。

## 目录作用

```text
src/
  index.ts                    CLI 入口
  cli.ts                      commander 命令定义
  agent/
    loop.ts                   通用 agent loop
    types.ts                  agent 核心类型
    context.ts                初始消息和 observation 辅助函数
  model/
    ModelClient.ts            模型适配器接口
    MockModel.ts              固定脚本式模型，用来教学和 demo
    OpenAICompatibleModel.ts  OpenAI-compatible 真实模型适配器
    OpenAIModel.ts            兼容旧命名的 thin alias
  tools/
    index.ts                  工具注册表
    types.ts                  工具协议类型
    listFiles.ts              list_files 工具
    readFile.ts               read_file 工具
    search.ts                 search 工具
    applyPatch.ts             apply_patch 工具
    runCommand.ts             run_cmd 工具
  sandbox/
    paths.ts                  repo 内路径限制
    commands.ts               命令白名单
  utils/
    logger.ts                 终端日志
examples/
  broken-project/             可被 agent 修复的示例项目
```

## 学习顺序

1. 第一步：理解工具协议  
   先读 `src/tools/types.ts`，看工具如何声明 name、schema 和 execute。

2. 第二步：理解 agent loop  
   再读 `src/agent/loop.ts`，看模型 action、工具执行和 observation history 如何串起来。

3. 第三步：理解 sandbox  
   读 `src/sandbox/paths.ts` 和 `src/sandbox/commands.ts`，理解为什么 coding agent 不能随便读路径或执行命令。

4. 第四步：理解 MockModel  
   读 `src/model/MockModel.ts`，看它如何模拟真实 LLM 的逐步决策。

5. 第五步：替换成真实 LLM  
   读 `src/model/OpenAICompatibleModel.ts`，看真实 API 响应如何被解析成同样的 agent action。

## 工具安全设计

- 所有文件路径都必须解析到 `repoRoot` 内。
- `read_file` 会拒绝绝对路径和 repo 外路径，例如 `../../secret`。
- `run_cmd` 不使用任意 shell，只允许白名单命令：
  - `npm test`
  - `pnpm test`
  - `npm run test`
  - `pnpm run test`
  - `npm run build`
  - `pnpm run build`
  - `tsc --noEmit`
- `run_cmd` 必须带 timeout，默认 10 秒，最大 30 秒。
- `apply_patch` v1 只支持修改已有文本文件，不支持创建、删除、重命名或二进制 patch。

## 为什么 apply_patch 使用 npm 包 diff

unified diff 的边界情况很多，例如多个 hunk、上下文匹配、换行处理和 patch 失败定位。这个项目的目标是学习 coding agent 的 loop 和工具协议，而不是手写一个脆弱的 patch parser。因此 v1 使用成熟的 `diff` 包来解析和应用 unified diff，同时把安全边界放在 repo 路径校验和清晰错误返回上。

## 真实模型接入点

需要重点看的文件：

- `src/model/ModelClient.ts`：模型输入输出接口。
- `src/model/OpenAICompatibleModel.ts`：真实 API 调用、JSON.parse、zod 校验和 action 映射。
- `src/agent/loop.ts`：模型错误如何变成 `model_error` observation。
- `src/cli.ts`：`--model mock` 和 `--model real` 的切换。
