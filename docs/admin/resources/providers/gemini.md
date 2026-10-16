# 接入 Google Gemini

返回[模型接入概览](../models.md)。Molr 提供 Google Gemini 连接预设。

## 连接配置

| 配置项 | 值 |
| --- | --- |
| 默认基础地址 | `https://generativelanguage.googleapis.com/v1beta/openai` |
| 协议 | Gemini Chat Completions |
| 认证 | API Key |

使用 Google AI Studio 的 Gemini API Key。此预设连接 Google 官方 OpenAI 兼容接口，并保留工具调用中的 `extra_content.google.thought_signature`；它不是 Gemini 原生 GenerateContent 或 Vertex AI 连接。多轮与并行工具调用的签名保持在原调用上，不向其他模型或连接转发。兼容目录未声明的图片、音频、上下文和输出能力需要手动核对配置。

按[通用连接步骤](../models.md#连接提供商)测试、保存并刷新模型；Anthropic 自动补齐 `/messages`，其他预设补齐 `/chat/completions`。

## 能力与验收

连接测试只读取目录，不执行推理。配置模型能力、上下文、输出上限和价格后，再验证真实对话及完整工具调用；未知价格不会按免费处理。协议与界面回归使用人工模拟服务，不代表全部在线模型已完成验收。

[官方接口文档](https://ai.google.dev/gemini-api/docs/openai)。
