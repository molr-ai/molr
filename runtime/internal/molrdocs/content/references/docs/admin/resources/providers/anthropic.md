# 接入 Anthropic / Claude

已支持官方标准 API 的已知型号自动计价；具体型号、地域币种与收费边界见[自动计价](https://molr.ai/zh/docs/admin/resources/models/#自动计价)。

返回[模型接入概览](https://molr.ai/zh/docs/admin/resources/models/)。Molr 提供 Anthropic / Claude 连接预设。

## 连接配置

| 配置项 | 值 |
| --- | --- |
| 默认基础地址 | `https://api.anthropic.com/v1` |
| 协议 | Anthropic Messages |
| 认证 | API Key |

使用 Anthropic API Key，Molr 通过 `x-api-key` 与 `anthropic-version` 请求原生 Messages API。模型目录使用同源 `/models` 并读取分页。系统指令、工具调用、工具结果和图片转换为原生内容块；流式思考块及签名仅向同一后端回传。当前不提供思考强度配置、服务端工具或音频输入，也不主动启用提示缓存写入。缓存读取用量参与计费；非零缓存写入用量无法用当前价格结构表达，会保留为待核对费用。

按[通用连接步骤](https://molr.ai/zh/docs/admin/resources/models/#连接提供商)测试、保存并刷新模型；Anthropic 自动补齐 `/messages`，其他预设补齐 `/chat/completions`。

## 能力与验收

连接测试只读取目录，不执行推理。配置模型能力、上下文、输出上限和价格后，再验证真实对话及完整工具调用；未知价格不会按免费处理。协议与界面回归使用人工模拟服务，不代表全部在线模型已完成验收。

[官方接口文档](https://platform.claude.com/docs/en/api/messages/create)。
