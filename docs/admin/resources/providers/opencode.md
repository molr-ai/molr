# 接入 OpenCode

返回[模型接入概览](../models.md)。本页讨论云端 Zen / Console 与 Go，不是本地 OpenCode CLI 或 Server。

## Zen / Console 按量 API

**允许 API 集成，Molr 尚未完整支持。** 官方分别提供 Chat Completions、Responses、Anthropic Messages 和 Gemini 接口，每个模型需使用指定协议；Molr 当前只具备其中的 Chat Completions 适配。[推理 API](https://opencode.ai/v2/docs/console/api/inference/)

Chat 推理端点为 `https://opencode.ai/inference/openai/v1/chat/completions`，目录为 `https://opencode.ai/inference/v1/models`。Molr 会从推理地址推导同层 `/models`，目前不能自动匹配这个目录；完整目录也不能全部当作 Chat 模型导入。[模型与端点](https://opencode.ai/v2/docs/console/models/)

## Go 订阅

Molr 目前尚未内置支持该订阅套餐。如需使用，请参考[如何接入订阅套餐](../models.md#如何接入订阅套餐)。
