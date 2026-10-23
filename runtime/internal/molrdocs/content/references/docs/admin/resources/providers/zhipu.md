# 接入智谱

已支持官方标准 API 的已知型号自动计价；具体型号、地域币种与收费边界见[自动计价](https://molr.ai/zh/docs/admin/resources/models/#自动计价)。

返回[模型接入概览](https://molr.ai/zh/docs/admin/resources/models/)。Molr 已提供 智谱 连接预设。

## 连接配置

| 配置项 | 值 |
| --- | --- |
| 预设 | 智谱 |
| 默认基础地址 | `https://open.bigmodel.cn/api/paas/v4` |
| 认证 | API Key |

选择「智谱」，填写中国区开放平台 API Key。预设使用 OpenAI 兼容 Chat Completions，不代表接入厂商全部原生接口或订阅产品。海外 Z.AI 的账号、地址和套餐范围见[接入 Z.AI](https://molr.ai/zh/docs/admin/resources/providers/zai/)，不要直接混用。

按[通用连接步骤](https://molr.ai/zh/docs/admin/resources/models/#连接提供商)测试、保存并刷新模型。连接测试读取模型目录；需要另行验证真实推理。

## 能力与验收

配置实际模型的工具能力、上下文与价格，并验证工具调用后能继续回答。通用适配器不会回传厂商专属推理字段；如模型要求这些字段，不能只凭连接测试成功判断可用。

模型用量与价格取决于实际调用产品，不能用网页会员权益代替 API 授权。通用网络排查、凭据维护与预算见[模型接入概览](https://molr.ai/zh/docs/admin/resources/models/)。
