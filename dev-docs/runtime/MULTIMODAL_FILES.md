# 按需读取多模态文件

`file_read` 是统一入口，不增加媒体 ID 或独立媒体工具。用户上传的附件通过 `conversation_read` 获取信息，再用 `conversation_attachment` 保存到工作区。普通工具产出的文件也只提供路径；媒体不会自动进入模型上下文。

```json
{"path":"/work/screenshot.png"}
```

授权目录使用 `/resources/<resource_id>/...`。`permission_list.FilePath` 和 `file_list.Entries[].Path` 返回可直接使用的路径。旧的 `resource_id + path` 仍兼容。虚拟路径不扩大授权，文件访问沿用禁止符号链接、硬链接、特殊文件和越界路径的约束。

## 文件与模型能力

- UTF-8 文本保留原有 `offset` / `limit` 行范围、版本及截断信息。
- JPEG、PNG、GIF、WebP 可作为图片输入；WebP 经隔离解码转为 PNG。GIF 直接按提供商支持的图片格式发送，不承诺动画时序理解。
- 音频通过隔离解码转为单声道 16 kHz WAV，发送至支持音频输入的 OpenAI-compatible 模型。
- 视频通过隔离解码抽取带采样时间戳的 JPEG 帧，要求模型支持图片；不包含音轨，不等价于原生视频理解。
- PDF、其他二进制或当前模型不支持的媒体仅返回元信息和原因。文件类型根据实际内容识别，不依赖扩展名或上传 MIME。

`content_loaded=false` 明确表示模型尚未看到或听到内容。`metadata_only=true` 可只取元信息，不加载文本或媒体。图片支持 `detail=auto|low|high`；音视频支持 `start_seconds` / `end_seconds`；视频支持 `max_frames`。媒体选项不与文本行范围混用。

媒体读取的 JSON 元信息和原生媒体内容分开传递。OpenAI 与 DeepSeek Chat Completions 适配器均在完整工具回复批次之后附加关联的 user 媒体消息，避免打断 tool-call 配对；DeepSeek 原有 `reasoning_content` 保留机制继续生效。采用此兼容方式是因为 DeepSeek API 定义允许 tool 图片，但其 Vision 指南仍声明仅 user 图片，不依赖存在分歧的行为。

## 模型配置与发现

模型配置增加 `SupportsImages` / `SupportsAudio`，WebUI 模型配置窗口可调整。发现端点若返回 `input_modalities` 和 `context_window`，会读取这些字段；未声明媒体能力时默认关闭，支持手动配置覆盖，不按模型名称猜测。

DeepSeek 适配器当前只开放图片和视频抽帧，原生音频始终关闭。OpenAI-compatible 音频需同时具备音频输入、文本输出及工具调用能力，单独的转录端点不属于这个接口。各提供商实际模型能力仍需按部署的端点核实。

音频还要求手动设置 `AudioInputMicrosPerMillion`，显式零表示免费。预留预算取文本和音频输入单价的较高值；结算解析 `prompt_tokens_details.audio_tokens`。缺失音频用量，或出现无法区分文字/音频的混合缓存用量时，费用保持待核对，不猜测账单或按文本单价结算。图片按提供商报告的输入 token 计费。

## 限制与缓存

- 输入媒体文件最大 64 MiB；单张图片或单个派生媒体最大 8 MiB。
- 直接图片尺寸最大每边 4096、总计 4000 万像素（采用 DeepSeek 多图请求的较低边界）；WebP 受限解码并缩放到 2048 边界内。
- 音视频默认读取 30 秒，每次最多 60 秒；视频默认 6 帧、最多 16 帧。
- 单次模型请求最大 32 个媒体内容块、24 MiB 媒体文件字节；优先保留最近读取的媒体，较早内容以未加载提示替代，可再次读取。
- 图片读取创建不可变快照，后续覆盖源文件不会改变已经读取的图像。持久上下文仅保存来源、版本、处理信息和内部快照摘要，不保存 Base64。
- 快照位于独立的 `<data_dir>/media/<owner>/<assistant>/`，不属于助理工作区。单助理缓存限额 512 MiB，写入时清理超过 7 天或最久未使用的快照。缓存可能被淘汰，持久引用不承诺永久可用。
- 每次发送前重新检查源权限、资源绑定和当前模型能力。源文件删除、授权撤销、缓存缺失或模型不兼容时，仅提供“本轮未加载”的文字提示；需要内容时再次 `file_read`。不会自动换模型或转录。
- 调试轨迹记录媒体引用及请求结构，图片和音频编码字节替换为 `[media omitted]`。

微信入站无转写 SILK 语音会先由内嵌、无宿主权限的 WebAssembly 解码器转换为 WAV，失败则保留原音频；这一步不依赖外部转录服务。模型读取 WAV 仍遵循上述音频能力与定价检查。

Linux 音视频和 WebP 解码需要 `bwrap`、`prlimit`、`ffmpeg`、`ffprobe`。固定参数解码运行在无网络、隔离 PID/用户/挂载空间中，仅能读取系统程序和本次暂存文件；有内存、CPU、输出文件及执行时间限制。隔离不可用时返回元信息和原因，绝不回退到宿主环境解码。不要求助理获得 Shell 或计算资源授权。

## 验证

```bash
MOLR_REQUIRE_MEDIA_SANDBOX=1 go test ./internal/app -run TestMediaCodec -v
go test ./internal/agent/model -run 'TestMediaAdapters|TestAudioEncoding'
go test ./internal/app -run 'TestFileMedia|TestFileReadUnknown'
```

官方协议参考：[OpenAI 图片](https://developers.openai.com/api/docs/guides/images-vision)、[OpenAI 音频](https://developers.openai.com/api/docs/guides/audio-chat-completions)、[DeepSeek Vision](https://api-docs.deepseek.com/guides/vision/)、[DeepSeek Chat Completions](https://api-docs.deepseek.com/api/create-chat-completion/)。
