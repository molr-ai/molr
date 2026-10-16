# 接入百炼

返回[模型接入概览](https://molr.ai/zh/docs/admin/resources/models/)。Molr 已提供 百炼 连接预设。

## 连接配置

| 配置项 | 值 |
| --- | --- |
| 预设 | 百炼 |
| 默认基础地址 | `https://dashscope.aliyuncs.com/compatible-mode/v1` |
| 认证 | API Key |

预设连接百炼的 OpenAI 兼容模式。选择「百炼」，使用与该地域端点对应的 API Key；其他地域按实际服务修改基础地址。Molr 没有接入 DashScope 原生调用协议，也没有将 Coding Plan 订阅作为此预设的默认权益。

按[通用连接步骤](https://molr.ai/zh/docs/admin/resources/models/#连接提供商)测试、保存并刷新模型。连接测试读取模型目录；需要另行验证真实推理。

## 能力与验收

选择支持 Chat Completions 与工具调用的模型，核对图片等输入能力和上下文上限。预设保留百炼身份，但不表示价格已自动覆盖；缺少价格时手动配置。

模型用量与价格取决于实际调用产品，不能用网页会员权益代替 API 授权。通用网络排查、凭据维护与预算见[模型接入概览](https://molr.ai/zh/docs/admin/resources/models/)。
