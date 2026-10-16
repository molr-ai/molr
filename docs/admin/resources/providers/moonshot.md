# 接入 Moonshot / Kimi

返回[模型接入概览](../models.md)。Moonshot 开放平台 API 与 Kimi Code 订阅是不同接入产品。

## Moonshot 标准 API

在「资源 → 模型 → 连接提供商」选择 Moonshot，默认基础地址为 `https://api.moonshot.cn/v1`，填写该平台的 API Key。其他地域或部署使用其实际地址与配套密钥，不混用账号配置。

Molr 使用 OpenAI 兼容 Chat Completions。测试并保存后刷新模型，配置工具／多模态能力、上下文与价格。该预设不包含 Kimi Code 登录或订阅专用适配；需回传专属推理字段的模型必须验证连续工具调用，不能只测试文本回复。

## Kimi Code 订阅

Molr 目前尚未内置支持该订阅套餐。如需使用，请参考[如何接入订阅套餐](../models.md#如何接入订阅套餐)。
