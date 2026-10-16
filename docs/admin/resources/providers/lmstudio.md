# 接入 LM Studio

返回[模型接入概览](../models.md)。Molr 提供 LM Studio 连接预设。

## 连接配置

| 配置项 | 值 |
| --- | --- |
| 默认基础地址 | `http://127.0.0.1:1234/v1` |
| 协议 | OpenAI-compatible Chat Completions |
| 认证 | 无认证或 API Key |

先在 LM Studio 中启动 API Server，并准备支持工具调用的模型。可无认证或使用服务端配置的 API Token。本机扫描包含 1234 端口，通过只读 `/api/v0/models` 元数据识别 LM Studio；不启动服务、不加载或下载模型。保存连接后的目录读取使用 `/v1/models`。本机地址指 Runtime 所在机器；其他设备需修改地址并配置 Runtime 私网白名单。

按[通用连接步骤](../models.md#连接提供商)测试、保存并刷新模型；Anthropic 自动补齐 `/messages`，其他预设补齐 `/chat/completions`。

## 能力与验收

连接测试只读取目录，不执行推理。配置模型能力、上下文、输出上限和价格后，再验证真实对话及完整工具调用；未知价格不会按免费处理。协议与界面回归使用人工模拟服务，不代表全部在线模型已完成验收。

[官方接口文档](https://lmstudio.ai/docs/developer/core/server)。
