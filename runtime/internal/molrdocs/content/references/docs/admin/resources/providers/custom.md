# 接入自定义服务

返回[模型接入概览](https://molr.ai/zh/docs/admin/resources/models/)。自定义入口适用于满足当前协议的模型服务和自建网关；它不会自动检测或转换任意厂商协议。

## 连接配置

选择「自定义服务」，填写基础地址和名称，并按服务要求选择无认证或 API Key。API Key 以 Bearer 方式发送；当前入口不支持 OAuth 登录或任意自定义请求头。

基础地址例如 `https://models.example.com/v1`，界面补齐 `/chat/completions`。连接测试与刷新目录会使用同一基础地址下的 `/models`。若上游目录不在此路径，需由网关统一，或保存连接后手动添加模型；目录失败和推理失败应分别排查。

## 服务需满足的契约

- 接收 OpenAI 兼容 Chat Completions，包括 `messages`、工具定义、`stream: true`、`stream_options.include_usage`、`max_completion_tokens` 与 `store: false`。
- 返回可解析的完成结果、工具调用及 token 用量。完整工具循环需要保留调用 ID、参数和工具结果的对应关系。
- 通用适配器不会回传 DeepSeek 等厂商专属推理字段。需专属字段或其他协议的模型应单独适配，不能仅替换 URL。
- 自动发现的目录需提供 `data` 数组和模型 `id`；能力、上下文与价格仍需核对配置。

精确协议和流式限制见[模型适配说明](https://molr.ai/zh/docs/dev/runtime/model-adapters/)。

## 自建网关

网关负责转换上游协议、整理模型目录和管理凭据。仅暴露一个 Chat Completions 地址不代表多轮工具调用、取消、用量和推理状态都已正确转换，接入前需使用人工测试资料验证。

Molr 只向配置的网关发送请求；网关处理上游凭据、路由与日志，管理员需明确其信任边界。不要将真实密钥交给来源不明的中转站。

## 订阅套餐

Molr 目前尚未内置支持订阅套餐。如需使用，请参考[如何接入订阅套餐](https://molr.ai/zh/docs/admin/resources/models/#如何接入订阅套餐)。
