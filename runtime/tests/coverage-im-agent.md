# IM、Agent 与事件模块 TDD 覆盖表

以下映射指向实际调用公开接口并断言行为的测试。接口现已连接实际 Go 实现与真实 SQLite 夹具，模型测试使用本机 HTTP 测试供应商。`RequireFactory` 仍在漏接夹具时给出明确失败；外部授权或供应商依赖可控，被测模块本身不使用 mock。已有纯校验测试继续独立运行。

`Service`、`Runtime`、`Client` 继承的小接口方法也逐项列出；同一行为测试可以覆盖不同接口所声明的同一方法。测试夹具中的故障、时钟与观测方法属于测试接入约定，不是新的产品 API。

## `internal/im/conversation`

| 包 | 接口 / 方法 | 测试 |
| --- | --- | --- |
| `internal/im/conversation` | `Client.List` | `TestTDDConversationAuthorizationAndArchive` |
| `internal/im/conversation` | `Service.List` | `TestTDDConversationAuthorizationAndArchive` |
| `internal/im/conversation` | `Client.Get` | `TestTDDConversationAuthorizationAndArchive`、`TestTDDPublishReadFeedbackAndAcceptance` |
| `internal/im/conversation` | `Service.Get` | `TestTDDConversationAuthorizationAndArchive`、`TestTDDPublishReadFeedbackAndAcceptance` |
| `internal/im/conversation` | `Client.Messages` | `TestTDDSendUserPersistsOneMessageAndOutboxEvent`、`TestTDDConversationMessagePaginationAndCancellation` |
| `internal/im/conversation` | `Service.Messages` | `TestTDDSendUserPersistsOneMessageAndOutboxEvent`、`TestTDDConversationMessagePaginationAndCancellation` |
| `internal/im/conversation` | `Client.SendUser` | `TestTDDSendUserPersistsOneMessageAndOutboxEvent`、`TestTDDSendTransactionFailureLeavesNoPartialMessage`、`TestTDDConversationMessagePaginationAndCancellation` |
| `internal/im/conversation` | `Service.SendUser` | `TestTDDSendUserPersistsOneMessageAndOutboxEvent`、`TestTDDSendTransactionFailureLeavesNoPartialMessage`、`TestTDDConversationMessagePaginationAndCancellation` |
| `internal/im/conversation` | `Client.MarkRead` | `TestTDDPublishReadFeedbackAndAcceptance` |
| `internal/im/conversation` | `Service.MarkRead` | `TestTDDPublishReadFeedbackAndAcceptance` |
| `internal/im/conversation` | `Client.SetFeedback` | `TestTDDPublishReadFeedbackAndAcceptance` |
| `internal/im/conversation` | `Service.SetFeedback` | `TestTDDPublishReadFeedbackAndAcceptance` |
| `internal/im/conversation` | `Publisher.Publish` | `TestTDDPublishReadFeedbackAndAcceptance`、`TestTDDConversationAuthorizationAndArchive` |
| `internal/im/conversation` | `Service.Publish` | `TestTDDPublishReadFeedbackAndAcceptance`、`TestTDDConversationAuthorizationAndArchive` |
| `internal/im/conversation` | `Publisher.AcceptUserMessage` | `TestTDDPublishReadFeedbackAndAcceptance` |
| `internal/im/conversation` | `Service.AcceptUserMessage` | `TestTDDPublishReadFeedbackAndAcceptance` |
| `internal/im/conversation` | `Repository.List` | `TestTDDConversationRepositoryOwnershipVersionAndMessageState` |
| `internal/im/conversation` | `Repository.Get` | `TestTDDConversationRepositoryOwnershipVersionAndMessageState`、`TestTDDConversationRepositoryRejectsCrossOwnerMutations` |
| `internal/im/conversation` | `Repository.Insert` | `TestTDDConversationRepositoryOwnershipVersionAndMessageState` |
| `internal/im/conversation` | `Repository.SetArchived` | `TestTDDConversationRepositoryOwnershipVersionAndMessageState`、`TestTDDConversationRepositoryRejectsCrossOwnerMutations` |
| `internal/im/conversation` | `Repository.Messages` | `TestTDDSendTransactionFailureLeavesNoPartialMessage`、`TestTDDConversationRepositoryOwnershipVersionAndMessageState` |
| `internal/im/conversation` | `Repository.Append` | `TestTDDConversationRepositoryOwnershipVersionAndMessageState`、`TestTDDConversationRepositoryRejectsCrossOwnerMutations` |
| `internal/im/conversation` | `Repository.MarkRead` | `TestTDDConversationRepositoryOwnershipVersionAndMessageState`、`TestTDDConversationRepositoryRejectsCrossOwnerMutations` |
| `internal/im/conversation` | `Repository.AcceptUserMessage` | `TestTDDConversationRepositoryOwnershipVersionAndMessageState`、`TestTDDConversationRepositoryRejectsCrossOwnerMutations` |
| `internal/im/conversation` | `Repository.SetFeedback` | `TestTDDConversationRepositoryOwnershipVersionAndMessageState`、`TestTDDConversationRepositoryRejectsCrossOwnerMutations` |
| `internal/im/conversation` | `Outbox.Append` | `TestTDDOutboxPendingIsNonDestructiveAndAcknowledgementIsScoped` |
| `internal/im/conversation` | `Outbox.Pending` | `TestTDDOutboxPendingIsNonDestructiveAndAcknowledgementIsScoped`、`TestTDDSendTransactionFailureLeavesNoPartialMessage` |
| `internal/im/conversation` | `Outbox.Acknowledge` | `TestTDDOutboxPendingIsNonDestructiveAndAcknowledgementIsScoped` |
| `internal/im/conversation` | `Content.Validate` | `TestMessageContentEnforcesFixedCardAndSizeBoundaries`、`TestTDDContentValidationBoundaries` |
| `internal/im/conversation` | `SendUserRequest.Validate` | `TestMessageContentEnforcesFixedCardAndSizeBoundaries`、`TestTDDSendAndPublishValidateMutationTargetsAndSources` |
| `internal/im/conversation` | `PublishRequest.Validate` | `TestPublishingCannotImpersonateUserOrAnotherOperation`、`TestTDDSendAndPublishValidateMutationTargetsAndSources` |

重点：真实用户发送和助理发布保持不同发送者；同一发送与消息、outbox、可见事件一起提交；幂等请求不重复产生事件；失败和取消不留下部分状态；归档阻止新消息；已读进度不倒退；消息分页不重复；反馈覆盖及清空；跨用户读写禁止。

## `internal/im/realtime`

| 包 | 接口 / 方法 | 测试 |
| --- | --- | --- |
| `internal/im/realtime` | `Feed.List` | `TestTDDRealtimeReplayIsOwnerScopedAndDoesNotConsume`、`TestTDDRealtimeEverySubscriberReceivesLiveEventAndRevocationStopsDelivery` |
| `internal/im/realtime` | `Feed.Subscribe` | `TestTDDRealtimeEverySubscriberReceivesLiveEventAndRevocationStopsDelivery`、`TestTDDRealtimeSubscriptionCancellationAndClose` |
| `internal/im/realtime` | `Subscription.Next` | `TestTDDRealtimeEverySubscriberReceivesLiveEventAndRevocationStopsDelivery`、`TestTDDRealtimeSubscriptionCancellationAndClose` |
| `internal/im/realtime` | `Subscription.Close` | `TestTDDRealtimeSubscriptionCancellationAndClose` |
| `internal/im/realtime` | `Store.Append` | `TestTDDRealtimeReplayIsOwnerScopedAndDoesNotConsume`、`TestTDDRealtimeEverySubscriberReceivesLiveEventAndRevocationStopsDelivery` |
| `internal/im/realtime` | `Store.Replay` | `TestTDDRealtimeReplayIsOwnerScopedAndDoesNotConsume` |
| `internal/im/realtime` | `Store.Watch` | `TestIdempotentAppendDoesNotNotify`、`TestIdleAuthorizationDoesNotReplay` |

重点：逐客户端重放、游标不可跨用户使用、主题过滤、多个客户端都收到实时事件、凭证撤销即停止、取消和关闭不挂起。

## `internal/agent`

| 包 | 接口 / 方法 | 测试 |
| --- | --- | --- |
| `internal/agent` | `Observer.Status` | `TestTDDAgentHandlesEventAndResumesCommittedCheckpoint`、`TestTDDAgentAuthorizationStopAndFailedCommitRecovery` |
| `internal/agent` | `Runtime.Status` | `TestTDDAgentHandlesEventAndResumesCommittedCheckpoint`、`TestTDDAgentAuthorizationStopAndFailedCommitRecovery` |
| `internal/agent` | `Observer.Stop` | `TestTDDAgentAuthorizationStopAndFailedCommitRecovery` |
| `internal/agent` | `Runtime.Stop` | `TestTDDAgentAuthorizationStopAndFailedCommitRecovery` |
| `internal/agent` | `Runtime.Handle` | `TestTDDAgentHandlesEventAndResumesCommittedCheckpoint`、`TestTDDAgentAuthorizationStopAndFailedCommitRecovery` |
| `internal/agent` | `ContextBuilder.Build` | `TestTDDAgentContextExcludesImplicitChatHistory` |
| `internal/agent` | `Checkpoints.Load` | `TestTDDCheckpointCompareAndSwapAndOwnerIsolation`、`TestTDDAgentHandlesEventAndResumesCommittedCheckpoint` |
| `internal/agent` | `Checkpoints.SaveIfChanged` | `TestCheckpointSaveIfChanged`、`TestProgressNoopPreservesRecoveryReferenceAndForcedObservation` |
| `internal/agent` | `Checkpoints.Save` | `TestTDDCheckpointCompareAndSwapAndOwnerIsolation` |
| `internal/agent` | `Turn.Validate` | `TestTurnRejectsCrossOwnerAndAssistantCheckpoints`、`TestTDDTurnValidationSourceAndCheckpointFormat` |

重点：已提交输入重投不重复执行，重启读取持久检查点；提交失败不得报告完成；上下文不隐式包含原始聊天历史；内部输出不自动发布消息；版本比较交换及身份校验。

## `internal/agent/model`

| 包 | 接口 / 方法 | 测试 |
| --- | --- | --- |
| `internal/agent/model` | `Catalog.List` | `TestTDDModelCatalogAndGenerationRequireCurrentAuthorization` |
| `internal/agent/model` | `Client.List` | `TestTDDModelCatalogAndGenerationRequireCurrentAuthorization` |
| `internal/agent/model` | `Client.Generate` | `TestTDDModelCatalogAndGenerationRequireCurrentAuthorization`、`TestTDDModelReservationIsCheckedBeforeDispatch`、`TestTDDModelStreamTerminalReceiptAndEOF`、`TestTDDModelTransportFailureIsNotSuccessfulEOF` |
| `internal/agent/model` | `Client.Receipt` | `TestTDDModelStreamTerminalReceiptAndEOF`、`TestTDDModelReceiptAndReconcileUseReceiptID` |
| `internal/agent/model` | `Client.Reconcile` | `TestTDDModelClosePreservesProviderSpendAndReconciliation`、`TestTDDModelReceiptAndReconcileUseReceiptID` |
| `internal/agent/model` | `Stream.Next` | `TestTDDModelStreamTerminalReceiptAndEOF`、`TestTDDModelTransportFailureIsNotSuccessfulEOF` |
| `internal/agent/model` | `Stream.Close` | `TestTDDModelClosePreservesProviderSpendAndReconciliation` |
| `internal/agent/model` | `Receipts.Get` | `TestTDDModelReceiptsResolveUnknownWithoutErasingEvidence` |
| `internal/agent/model` | `Receipts.ByOperation` | `TestTDDModelReceiptsResolveUnknownWithoutErasingEvidence` |
| `internal/agent/model` | `Receipts.Record` | `TestTDDModelReceiptsResolveUnknownWithoutErasingEvidence` |

重点：供应商调用之前检查当前授权与预算预留；终态后才是 EOF；关闭消费不消除已发生费用；网络超时保持未知结果；晚到证据可以确认结果但不能清除已知费用；回执不得重新绑定其他操作。本组将 `Client.Reconcile` 的 ID 约定为回执 ID，与 `Client.Receipt` 一致，后续实现应明确这一参数语义。

## `internal/agent/tools`

| 包 | 接口 / 方法 | 测试 |
| --- | --- | --- |
| `internal/agent/tools` | `Executor.Definitions` | `TestTDDToolDefinitionsAreFilteredAndExecutionIsIdempotent`、`TestTDDToolExecutionRechecksGrantsAndRejectsUnknownTools` |
| `internal/agent/tools` | `Executor.Execute` | `TestRemovedToolsCannotExecute`、 `TestTDDToolDefinitionsAreFilteredAndExecutionIsIdempotent`、`TestTDDToolExecutionRechecksGrantsAndRejectsUnknownTools`、`TestTDDToolArgumentsCannotOverrideTrustedIdentity` |
| `internal/agent/tools` | `Executor.Cancel` | `TestTDDToolCancelIsExecutionScoped` |

重点：只暴露有权限的内置工具；执行再次鉴权；模型参数不能伪造身份；重复操作不得重复副作用；工具输出脱敏且不自动成为聊天消息；取消有执行身份边界。

## `internal/agent/operation`

| 包 | 接口 / 方法 | 测试 |
| --- | --- | --- |
| `internal/agent/operation` | `Recorder.Begin` | `TestTDDOperationBeginDeduplicatesIntentAndRejectsPayloadChange` |
| `internal/agent/operation` | `Service.Begin` | `TestTDDOperationBeginDeduplicatesIntentAndRejectsPayloadChange` |
| `internal/agent/operation` | `Recorder.Complete` | `TestTDDOperationCompletionChecksIdentityAndVersion` |
| `internal/agent/operation` | `Service.Complete` | `TestTDDOperationCompletionChecksIdentityAndVersion` |
| `internal/agent/operation` | `Recorder.MarkUnknown` | `TestTDDOperationUnknownRequiresReconciliationWithoutBlindRetry` |
| `internal/agent/operation` | `Service.MarkUnknown` | `TestTDDOperationUnknownRequiresReconciliationWithoutBlindRetry` |
| `internal/agent/operation` | `Service.Get` | `TestTDDOperationBeginDeduplicatesIntentAndRejectsPayloadChange`、`TestTDDOperationCompletionChecksIdentityAndVersion` |
| `internal/agent/operation` | `Control.Get` | `TestTDDOperationBeginDeduplicatesIntentAndRejectsPayloadChange`、`TestTDDOperationCompletionChecksIdentityAndVersion` |
| `internal/agent/operation` | `Service.List` | `TestTDDOperationBeginDeduplicatesIntentAndRejectsPayloadChange` |
| `internal/agent/operation` | `Control.List` | `TestTDDOperationBeginDeduplicatesIntentAndRejectsPayloadChange` |
| `internal/agent/operation` | `Service.Cancel` | `TestTDDOperationCancellationAndRepositoryIsolation` |
| `internal/agent/operation` | `Control.Cancel` | `TestTDDOperationCancellationAndRepositoryIsolation` |
| `internal/agent/operation` | `Service.Reconcile` | `TestTDDOperationUnknownRequiresReconciliationWithoutBlindRetry` |
| `internal/agent/operation` | `Control.Reconcile` | `TestTDDOperationUnknownRequiresReconciliationWithoutBlindRetry` |
| `internal/agent/operation` | `Repository.Get` | `TestTDDOperationCancellationAndRepositoryIsolation`、`TestTDDOperationRepositoryRejectsDuplicateAndStaleSave` |
| `internal/agent/operation` | `Repository.FindByKey` | `TestTDDOperationCancellationAndRepositoryIsolation` |
| `internal/agent/operation` | `Repository.List` | `TestTDDOperationCancellationAndRepositoryIsolation` |
| `internal/agent/operation` | `Repository.Insert` | `TestTDDOperationRepositoryRejectsDuplicateAndStaleSave` |
| `internal/agent/operation` | `Repository.Save` | `TestTDDOperationRepositoryRejectsDuplicateAndStaleSave` |

重点：副作用意图与输入摘要绑定，幂等重试不更改载荷；旧版本或错误执行身份不能完成操作；未知结果先对账；取消持久化；仓储保留所有权与版本约束。

## `internal/agent/scheduler`

| 包 | 接口 / 方法 | 测试 |
| --- | --- | --- |
| `internal/agent/scheduler` | `Control.Jobs` | `TestTDDSchedulerControlIsOwnerScoped`、`TestTDDSchedulerRecoveryDoesNotBlindlyReplayUnknownEffect` |
| `internal/agent/scheduler` | `Service.Jobs` | `TestTDDSchedulerControlIsOwnerScoped`、`TestTDDSchedulerRecoveryDoesNotBlindlyReplayUnknownEffect` |
| `internal/agent/scheduler` | `Control.Cancel` | `TestTDDSchedulerControlIsOwnerScoped` |
| `internal/agent/scheduler` | `Service.Cancel` | `TestTDDSchedulerControlIsOwnerScoped` |
| `internal/agent/scheduler` | `Service.Run` | `TestTDDSchedulerRunShutdownMaintainsSingleActiveTurn` |
| `internal/agent/scheduler` | `Service.Shutdown` | `TestTDDSchedulerRunShutdownMaintainsSingleActiveTurn` |
| `internal/agent/scheduler` | `Service.Recover` | `TestTDDSchedulerRecoveryDoesNotBlindlyReplayUnknownEffect` |
| `internal/agent/scheduler` | `Queue.Claim` | `TestTDDQueueClaimSerializesAssistantAndChecksAckIdentity`、`TestTDDQueueRetryHonorsDelayAndCancelPreventsClaim` |
| `internal/agent/scheduler` | `Queue.Ack` | `TestTDDQueueClaimSerializesAssistantAndChecksAckIdentity` |
| `internal/agent/scheduler` | `Queue.Retry` | `TestTDDQueueRetryHonorsDelayAndCancelPreventsClaim` |
| `internal/agent/scheduler` | `Queue.List` | `TestTDDQueueClaimSerializesAssistantAndChecksAckIdentity`、`TestTDDQueueRetryHonorsDelayAndCancelPreventsClaim` |
| `internal/agent/scheduler` | `Queue.Cancel` | `TestTDDQueueRetryHonorsDelayAndCancelPreventsClaim` |
| `internal/agent/scheduler` | `MessageConsumer.AcceptMessage` | `TestTDDMessageConsumerWakesOnlyRealUserAndDeduplicates`、`TestTDDMessageConsumerFailureRollsBackInputAndAcknowledgement` |

重点：同助理单活动执行、执行身份与版本绑定、延迟重试、取消后不再领取、停止后无遗留执行；恢复未知副作用不得盲目重复；只把真实用户消息转为 Agent 输入，助理发布不会唤醒自身；输入入队与 IM outbox 确认一起提交。

## `internal/event`

| 包 | 接口 / 方法 | 测试 |
| --- | --- | --- |
| `internal/event` | `Writer.AppendAssistant` | `TestTDDAssistantWriterDeduplicatesOccurrenceAndCommitsActualSource`、`TestTDDAssistantWriterRollbackAndInvalidSource`、`TestTDDAssistantWriterRejectsInvalidSource` |
| `internal/event` | `AssistantEvent.Validate` | `TestScheduledInputCannotMasqueradeAsUserMessage`、`TestTDDAssistantEventValidatesEverySourceAndRequiredIdentity` |

重点：按来源及定时发生点去重，不按传输 ID 重复执行；回滚无已提交输入；来源校验覆盖全部四种事件与必需身份、时间字段。

## 接入限制

本表是接口行为覆盖映射，不是已实现代码的行覆盖率或完整安全验证报告。并发存储提交顺序、进程级崩溃及 SSE 网络层背压仍需在后续真实持久层与 HTTP 实现接入后补充集成压力测试。

## 实现构造与公开实现类型

构造器由相应测试的真实 SQLite 夹具调用；下面的实现类型与接口指向同一实际对象。

| 包 | 接口 / 方法 | 测试 |
| --- | --- | --- |
| `internal/im/conversation` | `NewRepository` | `TestTDDConversationConcurrentRetriesPersistOneSendAcrossRestart` |
| `internal/im/conversation` | `NewOutbox` | `TestTDDConversationConcurrentRetriesPersistOneSendAcrossRestart` |
| `internal/im/conversation` | `NewService` | `TestTDDConversationDefaultAuthorizationFailsClosed`、`TestTDDConversationConcurrentRetriesPersistOneSendAcrossRestart` |
| `internal/im/realtime` | `NewStore` | `TestTDDRealtimeStoreUsesCommittedSequenceAcrossInstances` |
| `internal/im/realtime` | `NewFeed` | `TestTDDRealtimeFeedDefaultsToDeny` |
| `internal/im/realtime` | `Cursor` | `TestTDDRealtimeCursorBindsOwnerAndTopics` |
| `internal/event` | `NewWriter` | `TestTDDAssistantWriterBindsSourceAndExpiresWithTransaction`、`TestTDDEventTaskAndScheduleRevisionIdentity` |
| `internal/agent` | `NewCheckpoints` | `TestTDDCheckpointCompareAndSwapAndOwnerIsolation` |
| `internal/agent` | `NewContextBuilder` | `TestTDDAgentContextExcludesImplicitChatHistory` |
| `internal/agent` | `NewRuntime` | `TestTDDAgentHandlesEventAndResumesCommittedCheckpoint`、`TestTDDAgentUsesExplicitHistoryAndPublishingToolsOverHTTP` |
| `internal/agent` | `Engine.Handle` | `TestTDDAgentHandlesEventAndResumesCommittedCheckpoint`、`TestTDDAgentUsesExplicitHistoryAndPublishingToolsOverHTTP`、`TestTDDAgentTaskEventBindsEveryExecutionToTask` |
| `internal/agent` | `Engine.Status` | `TestTDDAgentAuthorizationStopAndFailedCommitRecovery` |
| `internal/agent` | `Engine.Stop` | `TestTDDAgentAuthorizationStopAndFailedCommitRecovery` |
| `internal/agent` | `Engine.Lifecycle` | `TestTDDAgentAuthorizationStopAndFailedCommitRecovery` |
| `internal/agent/model` | `NewClient` | `TestTDDModelStreamTerminalReceiptAndEOF` |
| `internal/agent/model` | `NewReceipts` | `TestTDDModelReceiptsResolveUnknownWithoutErasingEvidence` |
| `internal/agent/operation` | `NewService` | `TestTDDOperationCompletionChecksIdentityAndVersion` |
| `internal/agent/operation` | `NewRepository` | `TestTDDOperationRepositoryRejectsDuplicateAndStaleSave` |
| `internal/agent/tools` | `NewExecutor` | `TestTDDToolDefinitionsAreFilteredAndExecutionIsIdempotent`、`TestRemovedToolsCannotExecute` |
| `internal/agent/scheduler` | `NewQueue` | `TestTDDQueueClaimSerializesAssistantAndChecksAckIdentity` |
| `internal/agent/scheduler` | `NewService` | `TestTDDSchedulerRunShutdownMaintainsSingleActiveTurn` |
| `internal/agent/scheduler` | `NewMessageConsumer` | `TestTDDMessageConsumerWakesOnlyRealUserAndDeduplicates` |

| `internal/agent` | `Engine.ResetContext` | `TestResetContextPreservesProductDataAndFencesOldEvents`、`TestResetContextCancelsActiveCallAndRejectsLateCheckpoint`、`TestResetContextOwnershipAndTransactionRollback` |

| `internal/agent` | `Engine.Capabilities`, `Observer.Capabilities`, `Runtime.Capabilities` | `TestCapabilitiesOwnerInspectionDoesNotStartExecution` |
| `internal/agent/tools` | `Catalog.Catalog`, `Executor.Catalog` | `TestOwnerToolCatalogIsMetadataOnlyAndRejectsAssistant` |

| `internal/agent/tools` | `ConversationAttachments.Get`, `ConversationAttachments.Upload`, `ConversationAttachments.Import` | `TestConversationAttachmentDispatchPreservesIdentityAndReferences` |
| `internal/agent/tools` | `ParsePlanRequest`, `PlanReference` | `TestPlanRequestValidationAndOpaqueReferences` |
| `internal/agent/tools` | `ErrorResult` | `TestToolFailuresReturnConcreteResults` |
| `internal/agent/tools` | `SkippedResult` | `TestSkippedToolHasNoPossibleEffects` |

| `internal/agent` | `Engine.MessageMetrics` | `TestTurnMetricsDynamicAggregationFinalizationAndIsolation` |
| `internal/agent` | `Observer.MessageMetrics` | `TestTurnMetricsDynamicAggregationFinalizationAndIsolation` |
| `internal/agent` | `Runtime.MessageMetrics` | `TestTurnMetricsDynamicAggregationFinalizationAndIsolation` |

| `internal/agent` | `Engine.Expedite` | `TestExpediteCancelsModelReadsOrderedMessagesAndConsumesQueueOnce`、`TestExpediteAuthorizationBoundsAndStop` |
| `internal/agent` | `Observer.Expedite` | `TestExpediteCancelsModelReadsOrderedMessagesAndConsumesQueueOnce`、`TestExpediteAuthorizationBoundsAndStop` |
| `internal/agent` | `Runtime.Expedite` | `TestExpediteCancelsModelReadsOrderedMessagesAndConsumesQueueOnce`、`TestExpediteAuthorizationBoundsAndStop` |

触发消息与加急：`TestExpediteCancelsModelReadsOrderedMessagesAndConsumesQueueOnce` 还验证触发消息保持未读、触发消息单独加急返回可重放的 `not_needed`、后续消息加急不重复注入触发消息。`TestTriggerTracksTurnChangesAndStop` 验证执行切换及停止时触发消息关联的更新与清除；`TestTriggerClearedOnFailureAndRestoredOnRetry` 验证失败后清除关联，并在可安全重试的事件恢复时重建关联。WebUI 的 `expedite.spec.ts` 使用延迟模型验证首条消息无按钮闪现、连续消息正常加急，以及加急完成后无临时状态残留。
| `internal/im/conversation` | `Publisher.ReadUserMessages` | `TestReadUserMessagesTracksDeliveryAndEnforcesOwnership` |
| `internal/im/conversation` | `Service.ReadUserMessages` | `TestReadUserMessagesTracksDeliveryAndEnforcesOwnership` |
| `internal/agent/scheduler` | `UserMessageEvent` | `TestUserMessageEventStableIdentity` |

| `internal/event` | `InterruptBoundary.Blocks` | `TestInterruptBoundaryDistinguishesOldAndNewSources` |

| `internal/agent/model` | `Model.ContextWindow` | `TestModelContextWindowPrecedence` |
| `internal/agent/model` | `Model.OutputWindow` | `TestModelOutputWindowPrecedence` |

| `internal/agent/model` | `Failure.Error` | `TestFailureRetainsTypedReasonThroughWrappers` |
| `internal/agent/model` | `Failure.KnownFailure` | `TestFailureRetainsTypedReasonThroughWrappers` |
| `internal/agent/model` | `Failure.Unwrap` | `TestFailureRetainsTypedReasonThroughWrappers` |
| `internal/agent/model` | `FailureOf` | `TestFailureRetainsTypedReasonThroughWrappers` |
| `internal/agent/scheduler` | `Deferred.Error` | `TestDeferredRetainsScheduleAndCause` |
| `internal/agent/scheduler` | `Deferred.Unwrap` | `TestDeferredRetainsScheduleAndCause` |
| `internal/agent/scheduler` | `IsDeferred` | `TestDeferredRetainsScheduleAndCause` |

| `internal/agent/tools` | `ParseShellTask` | `TestShellSessionArgumentsAndBoundedControl` |

## `internal/agent/prompt`

2026-10-02：模型展示模板与显式字段选择。

| 包 | 接口 / 方法 | 测试 |
| --- | --- | --- |
| `internal/agent/prompt` | `Text` | `TestProjectionPreservesMeaningAndExcludesInternalFields` |
| `internal/agent/prompt` | `Structured` | `TestProjectionPreservesMeaningAndExcludesInternalFields`、`TestInvalidStructuredSourceIsNotSilentlyEmptied` |
| `internal/agent/prompt` | `String` | `TestStringPreservesTextAndEscapesOnlyJSON` |

| `internal/agent` | `Observer.Compress`, `Runtime.Compress`, `Engine.Compress` | `TestCompressionEventManualIdleDeduplicatesAndSurvivesRestart` |
| `internal/agent/scheduler` | `Pending` | `TestPendingDeliveryHonorsClaimAndStopBoundaries` |
| `internal/agent/scheduler` | `PendingWithDeadline` | `TestPendingDeadlineExcludesIneligibleInputs` |
| `internal/event` | `AssistantEvent.Delivery` | `TestDeliveryDefaultsAndValidation` |

2026-10-03：集成插件与指令事件时补齐插件公开接口清单；此清单仅表示测试入口映射。

| 包 | 接口 / 方法 | 测试 |
| --- | --- | --- |
| `internal/plugins` | `New`, `Key`, `Package.Digest`, `Requested`, `Service.Install`, `Service.List`, `Service.Configure`, `Service.Remove`, `Service.UI` | `TestInstallIsolationImmutableUpgradeAndRemoval` |
| `internal/plugins` | `Package.Validate`, `Schema.Validate`, `Schema.Check` | `TestManifestAndBridgeFailClosed` |
| `internal/plugins` | `ToolName`, `Service.Execute`, `Service.ValidateCall` | `TestHTTPThroughToolExecutorReplaysOriginalReceiptAfterSwitch` |
| `internal/plugins` | `Service.Inspect`, `Service.Definitions` | `TestHTTPToolCapabilitiesHonorBindingsAndStoppedAssistants` |

| `internal/plugins` | `Service.PrepareCall`, `Service.ConnectionAccess`, `Service.TestConnection` | `TestHTTPProviderSwitchPinsSnapshotAndProtectsCredentials`, `TestHTTPConnectionQuotaProbeAndRevocation` |
| `internal/plugins` | `Service.Settings`, `Service.SaveSettings`, `Service.Connections`, `Service.SaveConnection` | `TestPluginSettingsIsolationCASAndDeniedConnection`, `TestPluginSettingsBridgeRequiresCapabilityAndUpgradeReview` |
| `internal/plugins` | `Service.RecoverHTTP`, `Service.HTTPHistory`, `Service.AcknowledgeHTTP`, `Service.RemoveConnection` | `TestHTTPRecoveryHistoryAcknowledgementAndConnectionRemoval` |
| `internal/plugins` | `Service.CancelOperation`, `Service.RevalidateRunning` | `TestHTTPExplicitCancellationAndGroupRevocation` |

| `internal/plugins` | `Service.SavePreference` | `TestPersonalPluginEnablementAndGroupAccess` |

| `internal/agent/tools` | `FileSearchRequest.SearchOptions`, `FileSearchRequest.Validate` | `TestFileSearchArgumentsAndSemanticDefaults` |

## `internal/im/weixin`

| 包 | 接口 / 方法 | 测试 |
| --- | --- | --- |
| `internal/im/weixin` | `New`、`Service.SetReceiver`、`Service.CheckWebInput`、`Service.AuthorizeChannel`、`Receiver.ReceiveChannel`、`Service.Status`、`Service.StartLogin`、`Service.PollLogin`、`Service.Disconnect` | `TestBindSwitchIngressAndDisconnect` |
| `internal/im/weixin` | `Service.CancelLogin` | `TestLoginIsolationCancelAndRefresh` |
| `internal/im/weixin` | `Service.Published` | `TestPublishAndDeliveryCommitTogether` |
| `internal/im/weixin` | `Service.Run` | `TestRestartKeepsCursorAndMarksInFlightUnknown` |
| `internal/im/weixin` | `Client.QR`、`Client.Login`、`Client.Poll`、`Client.Send`、`HTTPClient.QR`、`HTTPClient.Login`、`HTTPClient.Poll`、`HTTPClient.Send` | `TestHTTPProtocolRequestsAndErrors` |

| `internal/im/weixin` | `HTTPClient.Download`、`HTTPClient.SendMedia`、`MediaClient.Download`、`MediaClient.SendMedia` | `TestMediaCDNUploadDownloadProtocol`、`TestMediaIngressDeduplicationAndIndependentDelivery`、`TestMediaUploadFailureDoesNotSendMessage` |
| `internal/im/weixin` | `Service.Connected` | `internal/app.TestWeixinMediaAndContextUseLiveBinding` |

| `internal/im/weixin` | `Client.Check`、`HTTPClient.Check` | `TestExistingCredentialCheckAndLosslessSendReceipt`、`TestAlreadyBoundResponseNeedsMatchingLiveCredential` |

| `internal/notification` | `New`、`Sync`、`Service.List`、`Service.Get`、`Service.Update` | `TestLifecycleIsolationReplayAndRestart`、`TestSourceAndNoticeRollbackTogether` |

| 包 | 接口 / 方法 | 测试 |
| --- | --- | --- |
| `internal/event` | `DispatchSignal`, `AssistantInputSignal` | `TestJobCommitSignalAndStoppedOutboxFence` |
| `internal/event` | `SaveJob` | `TestJobCommitSignalAndStoppedOutboxFence` |
| `internal/im/conversation` | `OutboxSignal` | `TestOutboxSignalsOnlyCommittedNewMessages` |

| `internal/notification` | `Unresolved` | `internal/business/plan.TestFilteredHandoffsStillResolveNotices` |

| `internal/agent` | `Engine.Activities`, `Observer.Activities`, `Runtime.Activities` | `TestActivitiesBoundedProjectionAndDreamDeadline` |

| `internal/im/realtime` | `AuthorizationWatch.Changes`, `AuthorizationWatch.Check` | `TestAuthorizationWatchIdleConnections`, `TestAuthorizationCommitDuringCheckIsNotLost`, `TestAuthorizationWatchExpiryRechecksWithoutReplay` |
| `internal/im/realtime` | `AuthorizationWatch.Close`, `AuthorizationWatch.Done` | `TestAuthorizationWatchReleasedOnCancellationAndStoreClose` |

| `internal/agent/model` | `NewResponseLogger`, `ResponseLogger.Record` | `TestResponseLogCapturesHTTPAndSSEWithoutAffectingExecution`, `TestResponseLogRotationAndPermissions` |
| `internal/agent/model` | `Provider.StateSource`, `ReplayedReasoning` | `TestAdaptersIsolateProviderStateAndWireParameters`, `TestReasoningRequestPresenceAndBounds` |
| `internal/agent/tools` | `WithVisibleTools` | `internal/app.TestCommonFileMoveDeleteReceiptsAndVisibility` |
| `internal/im/conversation` | `Client.LatestMessages`, `Service.LatestMessages` | `TestLatestMessagesBatchesNewestAndEnforcesAccess`, `TestLatestMessagesFullPage` |

2026-10-11：插件包导入。

| 包 | 接口 / 方法 | 测试 |
| --- | --- | --- |
| `internal/plugins` | `ReadArchive` | `TestArchiveRejectsTraversalDuplicateLinksAndExpansion` |
| `internal/plugins` | `Service.ImportArchive`, `Service.ImportURL` | `TestArchiveImportAuthorizationAndRemoteURLBoundaries` |

| `internal/agent/tools` | `BrowserRequest.ValidateBatch` | `internal/app.TestBrowserInputBatchRejectsInvalidSuffixBeforeExecution` |
| `internal/agent/tools` | `ParseBrowser` | `TestBrowserArgumentsRejectControlAndLaunchOptions` |
