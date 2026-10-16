# 接入 Volcengine · 火山方舟

返回[模型接入概览](../models.md)。Molr 提供 Volcengine · 火山方舟 连接预设。

## 连接配置

| 配置项 | 值 |
| --- | --- |
| 默认基础地址 | `https://ark.cn-beijing.volces.com/api/v3` |
| 协议 | Chat Completions |
| 认证 | API Key |

使用火山方舟 API Key 和已开通的模型 ID；采用推理接入点时填写对应接入点 ID。预设连接 Chat Completions，保留同源 `reasoning_content`。目录与实际调用权限可能不同；目录不可用时可先保存，再手动添加已获授权的模型。没有接入 Responses 或方舟服务端工具。

按[通用连接步骤](../models.md#连接提供商)测试、保存并刷新模型；Anthropic 自动补齐 `/messages`，其他预设补齐 `/chat/completions`。

## 能力与验收

连接测试只读取目录，不执行推理。配置模型能力、上下文、输出上限和价格后，再验证真实对话及完整工具调用；未知价格不会按免费处理。协议与界面回归使用人工模拟服务，不代表全部在线模型已完成验收。

[官方接口文档](https://docs.volcengine.com/docs/ark/compatible-with-openai-sdk?lang=zh)。
