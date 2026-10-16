# 接入 OpenAI

返回[模型接入概览](https://molr.ai/zh/docs/admin/resources/models/)。本页区分 OpenAI 标准 API 与 ChatGPT 订阅。

## 标准 API

在「资源 → 模型 → 连接提供商」选择 OpenAI，基础地址使用 `https://api.openai.com/v1`，填写具备 API 权限的 API Key。测试连接、保存并刷新目录后，配置模型能力和价格。

当前预设调用 Chat Completions。请选择支持该接口的模型；目录中出现某个模型不表示 Molr 已支持其协议。仅支持 Responses 的模型尚不能直接使用。API Key 与 ChatGPT 登录是不同认证方式，见[官方认证说明](https://learn.chatgpt.com/docs/auth)。

## ChatGPT 订阅

Molr 目前尚未内置支持该订阅套餐。如需使用，请参考[如何接入订阅套餐](https://molr.ai/zh/docs/admin/resources/models/#如何接入订阅套餐)。
