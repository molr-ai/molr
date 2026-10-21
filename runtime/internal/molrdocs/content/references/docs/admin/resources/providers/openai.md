# OpenAI

管理员可以在 OpenAI Provider 中选择 API Key 或 Continue with ChatGPT。连接仍属于实例共享资源目录，模型按用户组授权分配。

## API Key

填写 OpenAI API Key 和服务地址，连接后发现模型并配置参考单价。此方式使用 Chat Completions；请选择支持该接口的模型。

## ChatGPT 套餐

1. 在 Molr 桌面 App 中连接目标 Runtime，并以管理员登录。
2. 添加 OpenAI Provider，认证方式选择 Continue with ChatGPT。在系统浏览器中完成账号登录和套餐使用授权。
3. 授权成功后自动发现该账号可用的模型。配置模型参考单价，再按现有方式设置默认模型或分配给用户组。

首次授权与重新授权需要桌面 App。App 只接收本机 OAuth 回调并提交授权码，Runtime 换取、校验并加密保存凭据。关闭 App 后，同一 Runtime 的 WebUI 和后台任务仍可使用连接。多个连接可以对应不同账号；重复连接同一套餐不会增加套餐额度。

连接设置显示账号，提供重新授权、断开和[管理套餐用量](https://chatgpt.com/settings/usage)入口。断开清除本地 token，保留账号与注册信息便于再次登录；远端撤销未确认时会提示在 ChatGPT 中检查。删除连接前先断开。首次换取凭据失败时，已签发的注册会保留为断开状态，可从该连接重试。

套餐调用使用公开 Responses API，复用 Molr Agent、工具、沙盒和预算账本。调用次数、并发与周期限制保持原规则。金额按所配置单价计算，属于内部预算参考计价，不代表 OpenAI 实际扣费或套餐剩余额度；未知价格仍需配置，不能据 token 数推算套餐余额。用量页面和轮次详情标识套餐参考计价。

该路由要求 `store: false`、`stream: true` 和完整历史，不接受 `max_output_tokens`。模型输出上限参与本地预算预留，不能作为服务端生成硬上限。支持文本、已配置图片能力和 Molr 本地函数工具；不启用此路由不支持的音频或 OpenAI 托管工具。套餐额度限制不会触发自动换账号或切换付费 API。

本机和用户自托管 Runtime 可通过桌面 App 接入。Runtime 连接沿用现有 HTTPS／显式局域网 HTTP 策略；局域网 HTTP 不提供传输加密。

官方说明：[开源接入](https://developers.openai.com/siwc/token-sharing-open-source)、[模型与推理](https://developers.openai.com/siwc/token-sharing-open-source/models-and-inference)、[预览限制](https://developers.openai.com/siwc/token-sharing-open-source/preview-limitations)。
