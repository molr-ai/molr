// UI copy: keep both locales in sync; parameter types are checked against zh.
const zh = {
  fileMetadataAttachedThisPrototypeDoesNotUploadContents: "已附加文件，图片可在本地预览。原型不上传文件内容，也不执行转写。",
  recordingIsUnavailableInThisBrowserOrHttpContext: "此浏览器或当前 HTTP 地址不支持录音。请使用 HTTPS 或上传音频文件。",
  recordingFailedRetryOrAttachAnAudioFile: "录音失败，请重试或上传音频文件。",
  recordingPressStopToFinishUpTo60Seconds: "正在录音，点击停止结束（最长 60 秒）。原型仅附加音频信息，不会转写。",
  cannotAccessMicrophoneCheckBrowserPermissions: "无法访问麦克风，请检查浏览器权限。",
  addARequestOrAskAboutProgress: "继续补充要求，或问问进展…",
  enterToSendShiftEnterForANewLine: "Enter 发送，Shift+Enter 换行 · 当前为模拟对话",
};

const en: typeof zh = {
  fileMetadataAttachedThisPrototypeDoesNotUploadContents: "Files attached with local image previews. This prototype does not upload contents or transcribe audio.",
  recordingIsUnavailableInThisBrowserOrHttpContext: "Recording is unavailable in this browser or HTTP context. Use HTTPS or attach an audio file.",
  recordingFailedRetryOrAttachAnAudioFile: "Recording failed. Retry or attach an audio file.",
  recordingPressStopToFinishUpTo60Seconds: "Recording. Press stop to finish (up to 60 seconds). This prototype attaches audio metadata without transcription.",
  cannotAccessMicrophoneCheckBrowserPermissions: "Cannot access microphone. Check browser permissions.",
  addARequestOrAskAboutProgress: "Add a request or ask about progress…",
  enterToSendShiftEnterForANewLine: "Enter to send, Shift+Enter for a new line · Simulated conversation",
};

export const messageComposerMessages = { zh, en };
