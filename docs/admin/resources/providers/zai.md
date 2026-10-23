# 接入 Z.AI

已支持官方标准 API 的已知型号自动计价；具体型号、地域币种与收费边界见[自动计价](../models.md#自动计价)。

返回[模型接入概览](../models.md)。Molr 提供 Z.AI 连接预设。

## 连接配置

| 配置项 | 值 |
| --- | --- |
| 默认基础地址 | `https://api.z.ai/api/paas/v4` |
| 协议 | Chat Completions |
| 认证 | API Key |

选择「Z.AI」，填写 Z.AI 标准 API Key。该预设与中国区智谱预设独立，保留 Z.AI 身份与同源 `reasoning_content`，不会强制开启特定模型的思考模式。目录不可用时可保存后手动添加模型。GLM Coding Plan 的专用端点和订阅权益不属于此标准 API 预设；订阅接入见[模型接入概览](../models.md#如何接入订阅套餐)。

按[通用连接步骤](../models.md#连接提供商)测试、保存并刷新模型；Anthropic 自动补齐 `/messages`，其他预设补齐 `/chat/completions`。

## 能力与验收

连接测试只读取目录，不执行推理。配置模型能力、上下文、输出上限和价格后，再验证真实对话及完整工具调用；未知价格不会按免费处理。协议与界面回归使用人工模拟服务，不代表全部在线模型已完成验收。

[官方接口文档](https://docs.z.ai/api-reference/llm/chat-completion)。
