# 接入 DeepSeek

返回[模型接入概览](https://molr.ai/zh/docs/admin/resources/models/)。Molr 已提供 DeepSeek 连接预设。

## 连接配置

| 配置项 | 值 |
| --- | --- |
| 预设 | DeepSeek |
| 默认基础地址 | `https://api.deepseek.com/v1` |
| 认证 | API Key |

选择 DeepSeek 预设会启用专属适配器，使用 DeepSeek 的输出参数、缓存用量及推理状态回传。不要为了修改地址改选「自定义服务」：自定义入口使用通用协议，不会自动启用这些行为。

按[通用连接步骤](https://molr.ai/zh/docs/admin/resources/models/#连接提供商)测试、保存并刷新模型。连接测试读取模型目录；需要另行验证真实推理。

## 能力与验收

连续执行工具调用，确认推理状态能随上下文回传。切换来源后若出现上下文不兼容提示，按提示重置助理上下文；不要手工补造推理字段。自动价格以当前价格配置为准，手动覆盖不会改变协议。

模型用量与价格取决于实际调用产品，不能用网页会员权益代替 API 授权。通用网络排查、凭据维护与预算见[模型接入概览](https://molr.ai/zh/docs/admin/resources/models/)。
