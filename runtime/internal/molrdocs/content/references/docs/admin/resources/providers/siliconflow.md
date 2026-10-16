# 接入 SiliconFlow · 硅基流动

返回[模型接入概览](https://molr.ai/zh/docs/admin/resources/models/)。Molr 提供 SiliconFlow · 硅基流动 连接预设。

## 连接配置

| 配置项 | 值 |
| --- | --- |
| 默认基础地址 | `https://api.siliconflow.cn/v1` |
| 协议 | Chat Completions |
| 认证 | API Key |

使用硅基流动 API Key。模型发现请求 `/models?sub_type=chat`，只查询聊天模型；推理保留同源 `reasoning_content`。工具调用和图片能力取决于实际托管模型，不能依据提供商身份统一开启。此预设不接入 embedding、reranker 或图像生成接口。

按[通用连接步骤](https://molr.ai/zh/docs/admin/resources/models/#连接提供商)测试、保存并刷新模型；Anthropic 自动补齐 `/messages`，其他预设补齐 `/chat/completions`。

## 能力与验收

连接测试只读取目录，不执行推理。配置模型能力、上下文、输出上限和价格后，再验证真实对话及完整工具调用；未知价格不会按免费处理。协议与界面回归使用人工模拟服务，不代表全部在线模型已完成验收。

[官方接口文档](https://api-docs.siliconflow.cn/docs/api/models-get)。
