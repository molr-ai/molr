# 接入 Z.AI

返回[模型接入概览](https://molr.ai/zh/docs/admin/resources/models/)。Z.AI 的服务地址和账号权限需单独核对；Molr 的[智谱预设](https://molr.ai/zh/docs/admin/resources/providers/zhipu/)指向中国区开放平台。

## 标准 API

Molr 没有 Z.AI 专用预设。具有标准 API 权限时，可选择「自定义服务」，按官方文档填写 `https://api.z.ai/api/paas/v4` 和 API Key，评估 Chat Completions 模型。此入口尚未作为独立提供商完成在线验收。[Z.AI API 文档](https://docs.z.ai/api-reference/introduction)

按[自定义服务说明](https://molr.ai/zh/docs/admin/resources/providers/custom/)验证目录、工具调用、流式用量与价格；如果目录不可用，可以在保存后手动添加模型。标准 API 与 Coding Plan 专用端点的权益不同，改地址不会将订阅额度转换为标准 API 额度。

## GLM Coding Plan 订阅

Molr 目前尚未内置支持该订阅套餐。如需使用，请参考[如何接入订阅套餐](https://molr.ai/zh/docs/admin/resources/models/#如何接入订阅套餐)。
