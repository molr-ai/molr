# 接入 MiniMax

已支持官方标准 API 的已知型号自动计价；具体型号、地域币种与收费边界见[自动计价](../models.md#自动计价)。

返回[模型接入概览](../models.md)。Molr 提供 MiniMax 连接预设。

## 连接配置

| 配置项 | 值 |
| --- | --- |
| 默认基础地址 | `https://api.minimax.cn/v1` |
| 协议 | Chat Completions |
| 认证 | API Key |

连接 MiniMax 的 OpenAI 兼容文本接口，通过 `reasoning_split: true` 将思考与正文分离，并保留多轮工具调用所需的 `reasoning_content`。其他地域使用该地域的 API Key 与基础地址。此预设不包含语音、图像或视频生成 API，也不会把订阅权益转换成标准 API 额度。

按[通用连接步骤](../models.md#连接提供商)测试、保存并刷新模型；Anthropic 自动补齐 `/messages`，其他预设补齐 `/chat/completions`。

## 能力与验收

连接测试只读取目录，不执行推理。配置模型能力、上下文、输出上限和价格后，再验证真实对话及完整工具调用；未知价格不会按免费处理。协议与界面回归使用人工模拟服务，不代表全部在线模型已完成验收。

[官方接口文档](https://platform.minimax.cn/docs/api-reference/text-openai-api)。
