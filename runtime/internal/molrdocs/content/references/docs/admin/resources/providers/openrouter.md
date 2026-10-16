# 接入 OpenRouter

返回[模型接入概览](https://molr.ai/zh/docs/admin/resources/models/)。

在「连接提供商」选择 OpenRouter，填写 OpenRouter API Key。默认地址为 `https://openrouter.ai/api/v1`，可在「更多配置」调整。测试连接和保存后刷新均读取同源模型目录，只保留支持文本输出的模型；模型 ID 保留厂商前缀。

目录提供上下文上限、输出上限、工具与图片／音频输入能力。完整的输入、输出和缓存读取报价会转换为美元／百万 token；缺少缓存折扣时按普通输入价格处理。报价缺失、无效或包含当前账本无法计量的额外收费项时，价格保持未知，需要手动配置。目录报价用于 Molr 的预算和用量计量，不代表 OpenRouter 最终账单。刷新更新自动价格，保留用户手动价格与能力；关闭自定义价格可恢复目录报价。

OpenRouter 专用适配器保留推理模型的续接状态，包括工具调用后的签名和加密推理块。更换提供商、端点或模型不会回传原后端状态。接入使用 Chat Completions，不包含 OpenRouter Responses、原生 Messages、路由偏好或推理强度设置。
