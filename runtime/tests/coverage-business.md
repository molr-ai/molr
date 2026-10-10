# Business 与 Search 接口行为测试覆盖映射

这些 TDD 合同已通过 `fixture_test.go` 接入真实 SQLite 实现；映射表描述入口与测试的对应关系，不是行覆盖率报告。fixture 负责创建实际 Service/Repository/Execution/Indexer、数据库隔离、依赖播种、时钟/故障注入和持久化观测，不能用 fake Service 代替被测对象。

`hire`、`connect`、`createTask` 是测试准备 helper：分别直接调用 `Service.Hire`、`Service.Connect`、`Service.Create`。表中列出的调用它们的测试属于通过 helper 的真实接口调用，不是只检查类型。

合同假设：每个测试得到全新隔离实例；主体来自受信身份设施；查询完成即可读到已提交写入；跨用户目标允许 `Forbidden` 或 `NotFound`；版本冲突是 `Conflict`。测试中的具体 verification purpose 字符串为测试适配器与真实验证组件之间的映射名，不要求生产 HTTP 路由使用同名字符串。

| 包 | 公开接口 / 方法 | 直接调用该方法的测试 |
| --- | --- | --- |
| `internal/business/assistant` | `Service.Hire` | `TestTDDHireIdentityAtomicityAndIdempotency`, `TestTDDHireRollsBackDependentRows`, `TestTDDLifecycleAndCloneKeepSeparateIdentity`, `TestTDDGroupsAndPlacement`, `TestTDDAssistantAndGroupOwnership`, `TestTDDConcurrentHireReplaysOneIdentity`, `TestTDDRehireAndPlacementRejectForeignAssistant`, `TestTDDCloneCopiesOnlySelectedPersistentData`, `TestTDDRehireRestoresOnlyExplicitlySelectedCapabilities` |
| `internal/business/assistant` | `Service.Get` | `TestTDDHireIdentityAtomicityAndIdempotency`, `TestTDDGroupsAndPlacement`, `TestTDDAssistantAndGroupOwnership`, `TestTDDRehireAndPlacementRejectForeignAssistant` |
| `internal/business/assistant` | `Service.List` | `TestTDDHireIdentityAtomicityAndIdempotency`, `TestTDDAssistantAndGroupOwnership` |
| `internal/business/assistant` | `Service.UpdateSelfProfile`, `Assistant.Profile`, `ProfileConflict.Error`, `ProfileConflict.Unwrap`, `NormalizeName` | `TestProfileMutationAtomicNoopAndIdentity` |
| `internal/business/assistant` | `AvatarFromImage` | `TestAvatarImageCropAndLimits` |
| `internal/business/assistant` | `Service.Update` | `TestTDDHireIdentityAtomicityAndIdempotency`, `TestTDDAssistantAndGroupOwnership` |
| `internal/business/assistant` | `Service.Place` | `TestTDDGroupsAndPlacement`, `TestTDDAssistantAndGroupOwnership`, `TestTDDRehireAndPlacementRejectForeignAssistant` |
| `internal/business/assistant` | `Service.Dismiss` | `TestTDDLifecycleAndCloneKeepSeparateIdentity`, `TestTDDAssistantAndGroupOwnership`, `TestTDDRehireAndPlacementRejectForeignAssistant`, `TestTDDRehireRestoresOnlyExplicitlySelectedCapabilities` |
| `internal/business/assistant` | `Service.Rehire` | `TestTDDLifecycleAndCloneKeepSeparateIdentity`, `TestTDDRehireAndPlacementRejectForeignAssistant`, `TestTDDRehireRestoresOnlyExplicitlySelectedCapabilities` |
| `internal/business/assistant` | `Service.Clone` | `TestTDDLifecycleAndCloneKeepSeparateIdentity`, `TestTDDAssistantAndGroupOwnership`, `TestTDDCloneCopiesOnlySelectedPersistentData` |
| `internal/business/assistant` | `Service.Groups` | `TestTDDGroupsAndPlacement` |
| `internal/business/assistant` | `Service.CreateGroup` | `TestTDDGroupsAndPlacement`, `TestTDDAssistantAndGroupOwnership` |
| `internal/business/assistant` | `Service.UpdateGroup` | `TestTDDGroupsAndPlacement`, `TestTDDAssistantAndGroupOwnership` |
| `internal/business/assistant` | `Service.DeleteGroup` | `TestTDDGroupsAndPlacement`, `TestTDDAssistantAndGroupOwnership` |
| `internal/business/assistant` | `Repository.Get` | `TestTDDRepositoryAssistantAndGroups` |
| `internal/business/assistant` | `Repository.List` | `TestTDDRepositoryAssistantAndGroups` |
| `internal/business/assistant` | `Repository.Insert` | `TestTDDRepositoryAssistantAndGroups` |
| `internal/business/assistant` | `Repository.Save` | `TestTDDRepositoryAssistantAndGroups` |
| `internal/business/assistant` | `Repository.Groups` | `TestTDDRepositoryAssistantAndGroups` |
| `internal/business/assistant` | `Repository.PutGroup` | `TestTDDRepositoryAssistantAndGroups` |
| `internal/business/assistant` | `Repository.DeleteGroup` | `TestTDDRepositoryAssistantAndGroups` |
| `internal/business/memory` | `Service.Create` | `TestTDDServiceCRUD`, `TestTDDServiceOwnershipAndValidation`, `TestTDDCreateCommitFailureLeavesNoObject`, `TestTDDPrivateAndSharedMemoryPolicy` |
| `internal/business/memory` | `Service.Get` | `TestTDDServiceCRUD`, `TestTDDServiceOwnershipAndValidation`, `TestTDDPrivateAndSharedMemoryPolicy` |
| `internal/business/memory` | `Service.List` | `TestTDDServiceCRUD`, `TestTDDServiceOwnershipAndValidation`, `TestTDDCreateCommitFailureLeavesNoObject`, `TestTDDPrivateAndSharedMemoryPolicy` |
| `internal/business/memory` | `Service.Update` | `TestTDDServiceCRUD`, `TestTDDServiceOwnershipAndValidation`, `TestTDDPrivateAndSharedMemoryPolicy` |
| `internal/business/memory` | `Service.Delete` | `TestTDDServiceCRUD`, `TestTDDServiceOwnershipAndValidation`, `TestTDDPrivateAndSharedMemoryPolicy` |
| `internal/business/resource` | `Service.Connect` | `TestTDDResourceConnectLifecycle`, `TestTDDResourceOwnerOnlyAndSafeDescriptors` |
| `internal/business/resource` | `Service.Get` | `TestTDDResourceConnectLifecycle`, `TestTDDResourceOwnerOnlyAndSafeDescriptors` |
| `internal/business/resource` | `Service.List` | `TestTDDResourceConnectLifecycle`, `TestTDDResourceOwnerOnlyAndSafeDescriptors` |
| `internal/business/resource` | `Service.Update` | `TestTDDResourceConnectLifecycle`, `TestTDDResourceOwnerOnlyAndSafeDescriptors` |
| `internal/business/resource` | `Service.Disconnect` | `TestTDDResourceConnectLifecycle`, `TestTDDResourceOwnerOnlyAndSafeDescriptors` |
| `internal/business/resource` | `Service.Check` | `TestTDDResourceConnectLifecycle`, `TestTDDResourceOwnerOnlyAndSafeDescriptors` |
| `internal/business/resource` | `Repository.Get` | `TestTDDResourceRepositoryBindingIsolation` |
| `internal/business/resource` | `Repository.List` | `TestTDDResourceRepositoryBindingIsolation` |
| `internal/business/resource` | `Repository.Insert` | `TestTDDResourceRepositoryBindingIsolation` |
| `internal/business/resource` | `Repository.Save` | `TestTDDResourceRepositoryBindingIsolation` |
| `internal/business/resource` | `Repository.Binding` | `TestTDDResourceRepositoryBindingIsolation` |
| `internal/business/resource` | `Repository.BindingForModel` | `TestModelBindingIndexTracksLifecycleAndRollback`、`TestModelBindingIndexSurvivesReopenAndSharedCatalog` |
| `internal/business/resource` | `ModelID` | `TestModelIDRemainsStable` |
| `internal/business/schedule` | `Service.Create` | `TestTDDServiceCRUD`, `TestTDDServiceOwnershipAndValidation`, `TestTDDCreateCommitFailureLeavesNoObject`, `TestTDDPersonalScheduleDoesNotWakeAssistant` |
| `internal/business/schedule` | `Service.Get` | `TestTDDServiceCRUD`, `TestTDDServiceOwnershipAndValidation` |
| `internal/business/schedule` | `Service.List` | `TestTDDServiceCRUD`, `TestTDDServiceOwnershipAndValidation`, `TestTDDCreateCommitFailureLeavesNoObject` |
| `internal/business/schedule` | `Service.Update` | `TestTDDServiceCRUD`, `TestTDDServiceOwnershipAndValidation` |
| `internal/business/schedule` | `Service.Delete` | `TestTDDServiceCRUD`, `TestTDDServiceOwnershipAndValidation` |
| `internal/business/schedule` | `Repository.Get` | `TestTDDRepositoryVersionAndIsolation` |
| `internal/business/schedule` | `Repository.List` | `TestTDDRepositoryVersionAndIsolation` |
| `internal/business/schedule` | `Repository.Insert` | `TestTDDRepositoryVersionAndIsolation`, `TestTDDDueAndUniqueOccurrences`, `TestTDDOccurrenceConcurrentDeduplication` |
| `internal/business/schedule` | `Repository.Save` | `TestTDDRepositoryVersionAndIsolation` |
| `internal/business/schedule` | `Repository.Delete` | `TestTDDRepositoryVersionAndIsolation` |
| `internal/business/schedule` | `Repository.Due` | `TestTDDDueAndUniqueOccurrences` |
| `internal/business/schedule` | `Repository.RecordOccurrence` | `TestTDDDueAndUniqueOccurrences`, `TestTDDOccurrenceConcurrentDeduplication` |
| `internal/business/schedule` | `Trigger.Validate` | `TestTDDTriggerValidationBoundaries`；已有 `TestCatchUpIsBoundedAndTimezoneIsExplicit` |
| `internal/business/settings` | `Service.Get` | `TestTDDSettingsUpdateDoesNotRewriteActiveBudget` |
| `internal/business/settings` | `Service.Update` | `TestTDDSettingsUpdateDoesNotRewriteActiveBudget` |
| `internal/business/settings` | `Service.Export` | `TestTDDExportRequiresFreshPurposeBoundVerification` |
| `internal/business/settings` | `Service.ExportStatus` | `TestTDDExportRequiresFreshPurposeBoundVerification` |
| `internal/business/settings` | `Service.Cleanup` | `TestTDDCleanupChecksEveryObjectBeforeMutation` |
| `internal/business/settings` | `Repository.Get` | `TestTDDSettingsRepositoryVersionsAndExports` |
| `internal/business/settings` | `Repository.Save` | `TestTDDSettingsRepositoryVersionsAndExports` |
| `internal/business/settings` | `Repository.PutExport` | `TestTDDSettingsRepositoryVersionsAndExports` |
| `internal/business/settings` | `Repository.Export` | `TestTDDSettingsRepositoryVersionsAndExports` |
| `internal/business/task` | `Service.Create` | `TestTDDTaskCreateAndRead`, `TestTDDTaskControlsAndOptimisticVersion`, `TestTDDRevisionPreservesOutputsAndTotalSpend`, `TestTDDDeliveryRejectsClaimsStaleRevisionAndWrongExecution`, `TestTDDReportCannotBypassDeliveryOrCancel`, `TestTDDTaskCreationCommitFailureIsAtomic`, `TestTDDTaskAllControlsRejectForeignOwner` |
| `internal/business/task` | `Service.Get` | `TestTDDTaskCreateAndRead`, `TestTDDRevisionPreservesOutputsAndTotalSpend`, `TestTDDDeliveryRejectsClaimsStaleRevisionAndWrongExecution`, `TestTDDTaskAllControlsRejectForeignOwner` |
| `internal/business/task` | `Service.List` | `TestTDDTaskCreateAndRead` |
| `internal/business/task` | `Service.Revise` | `TestTDDRevisionPreservesOutputsAndTotalSpend`, `TestTDDTaskAllControlsRejectForeignOwner` |
| `internal/business/task` | `Service.Revisions` | `TestTDDRevisionPreservesOutputsAndTotalSpend` |
| `internal/business/task` | `Service.Pause` | `TestTDDTaskControlsAndOptimisticVersion` |
| `internal/business/task` | `Service.Resume` | `TestTDDTaskControlsAndOptimisticVersion`, `TestTDDTaskAllControlsRejectForeignOwner` |
| `internal/business/task` | `Service.Cancel` | `TestTDDTaskControlsAndOptimisticVersion`, `TestTDDReportCannotBypassDeliveryOrCancel`, `TestTDDTaskAllControlsRejectForeignOwner` |
| `internal/business/task` | `Execution.Report` | `TestTDDRevisionPreservesOutputsAndTotalSpend`, `TestTDDDeliveryRejectsClaimsStaleRevisionAndWrongExecution`, `TestTDDReportCannotBypassDeliveryOrCancel` |
| `internal/business/task` | `Execution.Deliver` | `TestTDDRevisionPreservesOutputsAndTotalSpend`, `TestTDDDeliveryRejectsClaimsStaleRevisionAndWrongExecution` |
| `internal/business/task` | `Repository.Get` | `TestTDDTaskRepositoryAndAppendOnlyRevisions` |
| `internal/business/task` | `Repository.List` | `TestTDDTaskRepositoryAndAppendOnlyRevisions` |
| `internal/business/task` | `Repository.Insert` | `TestTDDTaskRepositoryAndAppendOnlyRevisions` |
| `internal/business/task` | `Repository.Save` | `TestTDDTaskRepositoryAndAppendOnlyRevisions` |
| `internal/business/task` | `Repository.AppendRevision` | `TestTDDTaskRepositoryAndAppendOnlyRevisions` |
| `internal/business/task` | `Repository.Revisions` | `TestTDDTaskRepositoryAndAppendOnlyRevisions` |
| `internal/business/todo` | `Service.Create` | `TestTDDServiceCRUD`, `TestTDDServiceOwnershipAndValidation`, `TestTDDCreateCommitFailureLeavesNoObject`, `TestTDDAssignmentDoesNotStartExecution` |
| `internal/business/todo` | `Service.Get` | `TestTDDServiceCRUD`, `TestTDDServiceOwnershipAndValidation` |
| `internal/business/todo` | `Service.List` | `TestTDDServiceCRUD`, `TestTDDServiceOwnershipAndValidation`, `TestTDDCreateCommitFailureLeavesNoObject`, `TestTDDAssignmentDoesNotStartExecution` |
| `internal/business/todo` | `Service.Update` | `TestTDDServiceCRUD`, `TestTDDServiceOwnershipAndValidation` |
| `internal/business/todo` | `Service.Delete` | `TestTDDServiceCRUD`, `TestTDDServiceOwnershipAndValidation` |
| `internal/business/todo` | `Repository.Get` | `TestTDDRepositoryVersionAndIsolation` |
| `internal/business/todo` | `Repository.List` | `TestTDDRepositoryVersionAndIsolation` |
| `internal/business/todo` | `Repository.Insert` | `TestTDDRepositoryVersionAndIsolation` |
| `internal/business/todo` | `Repository.Save` | `TestTDDRepositoryVersionAndIsolation` |
| `internal/business/todo` | `Repository.Delete` | `TestTDDRepositoryVersionAndIsolation` |
| `internal/search` | `Service.Search` | `TestTDDSearchCombinesAuthorizedSourcesAndCounts`, `TestTDDSearchRechecksRevokedAccessAndCursorScope`, `TestTDDIndexApplyHonorsTombstonesAndSourceVersion` |
| `internal/search` | `Maintenance.Rebuild` | `TestTDDRebuildOwnerOnlyAndStatusIsolation` |
| `internal/search` | `Maintenance.Status` | `TestTDDRebuildOwnerOnlyAndStatusIsolation` |
| `internal/search` | `Indexer.Rebuild` | `TestTDDRebuildOwnerOnlyAndStatusIsolation` |
| `internal/search` | `Indexer.Status` | `TestTDDRebuildOwnerOnlyAndStatusIsolation` |
| `internal/search` | `Indexer.Apply` | `TestTDDIndexApplyHonorsTombstonesAndSourceVersion` |

原始接口合同映射 **94** 个公开方法（含 `Indexer` 继承的维护方法及 `Trigger.Validate`）。

重点行为：助理创建及任务创建事务回滚；雇佣/解雇/返聘及克隆身份；分组与置顶；待办分派不触发执行；私有/共享记忆隔离及源数据保留；日程时区、追补上限、到期选择与 occurrence 去重；资源连接的所有者限制及绑定不泄漏；任务版本、暂停恢复取消、历史产物/费用保留、证据交付；设置默认值不改在途预算、敏感操作验证、清理原子性；跨来源搜索权限、统计过滤、游标绑定、撤权即生效、索引墓碑及重建权限。

实现适配约束：工厂提供接口文件注释中约定的 seed、观察及故障注入依赖，不以 Skip 或仅检查错误占位。定时投递原子性与并发由本组 Dispatcher 测试覆盖；持久化崩溃恢复和事务隔离同时由 scheduler/storage 组及真实存储集成测试承担。


实现接线补充：所有工厂现在构造真实 SQLite Store、Service 与 Repository。`Options.Authorizer` 缺失时拒绝操作；外部鉴权/供应商/Agent 生命周期/文件编码器依赖可以受控，IM 与 Grant 仓储使用真实实现。

Clone/返聘授权契约细化：新授权具有新 ID，并绑定目标助理；测试对比原授权的资源、动作、范围、有效期与目标身份，不把源 Grant ID 当作可跨助理复用的授权对象。

| 包 | 公开接口 / 方法 | 直接或工厂调用该入口的测试 |
| --- | --- | --- |
| `internal/business/assistant` | `NewService` | `TestTDDMissingAuthorizerFailsClosed` |
| `internal/business/assistant` | `NewRepository` | `TestTDDRepositoryAssistantAndGroups` |
| `internal/business/todo` | `NewService` | `TestTDDMissingAuthorizerFailsClosed` |
| `internal/business/todo` | `NewRepository` | `TestTDDRepositoryVersionAndIsolation` |
| `internal/business/schedule` | `NewService` | `TestTDDMissingAuthorizerFailsClosed` |
| `internal/business/schedule` | `NewRepository` | `TestTDDRepositoryVersionAndIsolation` |
| `internal/business/memory` | `NewService` | `TestTDDMissingAuthorizerFailsClosed` |
| `internal/business/task` | `NewService` | `TestTDDMissingAuthorizerFailsClosed` |
| `internal/business/task` | `NewRepository` | `TestTDDTaskRepositoryAndAppendOnlyRevisions` |
| `internal/business/resource` | `NewService` | `TestTDDMissingAuthorizerFailsClosed` |
| `internal/business/resource` | `NewRepository` | `TestTDDResourceRepositoryBindingIsolation` |
| `internal/business/settings` | `NewService` | `TestTDDMissingAuthorizerFailsClosed` |
| `internal/business/settings` | `NewRepository` | `TestTDDSettingsRepositoryVersionsAndExports` |
| `internal/business/task` | `NewExecution` | `TestTDDRevisionPreservesOutputsAndTotalSpend` |
| `internal/search` | `NewService` | `TestTDDMissingAuthorizerFailsClosed` |
| `internal/search` | `NewIndexer` | `TestTDDRebuildOwnerOnlyAndStatusIsolation` |
| `internal/business/schedule` | `NewDispatcher`, `Dispatcher.Tick` | `TestTDDDispatcherMissedPoliciesAreBoundedAndAdvance`, `TestTDDDispatcherPersonalAndDismissedPlansNeverWakeAgent`, `TestTDDDispatcherOccurrenceEventAndProgressRollBackTogether`, `TestTDDDispatcherTimezoneAndCountSurviveRepeatedTicks`, `TestTDDDispatcherConcurrentTicksAndInvalidRule`, `TestTDDDispatcherResumesPersistedCursorWithoutReplayingLifetime` |

日程执行采用 RFC 5545 RRULE，`StartAt`/IANA `Timezone` 是唯一 DTSTART 来源；`Trigger.Validate` 保留形状校验，Service 创建/启用与 Dispatcher 进一步校验实际规则。正常轮询容差为一分钟：Skip 仅投递容差内发生点，Coalesce 投递最近一个，CatchUp 投递最近至多 MaxCatchUp 个；三者均推进到当前时间之后，避免每次 Tick 无限追补。COUNT、DST 本地时间和已消费规则游标持久化；一次历史计算上限为 100,000 个发生点，超出则明确失败，不无限占用事务。发生记录、助理来源事件、日程版本及下一时间同事务提交；个人日程不产生 Agent 事件，离职助理计划禁用。

敏感业务写入现与 `security.audit` 同事务：资源连接/改名/断开、导出申请/完成、逐对象清理。`TestTDDResourceAuditIsAtomicIdempotentAndContainsNoBinding` 与 `TestTDDSettingsSensitiveAuditsCommitWithEffectsAndRedactProofs` 验证提交故障回滚、重试不重复审计以及审计不包含绑定、凭证引用、证明令牌或导出正文。
| `internal/business/resource` | `Service.RefreshModels` | `TestRefreshModelsPreservesOverridesAndMutationContracts` |
| `internal/business/resource` | `Service.ConfigureModel` | `TestConfigureModelPersistsOverrideAndRejectsStaleOrUnauthorizedChanges` |

| `internal/business/resource` | `Service.TestConnection` | `TestConnectionTestIsOwnerOnlyAndDoesNotCreateResource` |

| `internal/business/resource` | `Service.ProviderConnection`, `Service.SaveProvider` | `TestProviderConnectionOwnerOnlyAndVersionedChanges` |

| `internal/business/resource` | `Service.DeleteProvider` | `TestDeleteProviderAuthorizationVersionAndReplay` |

| `internal/business/plan` | `New` | `TestPersonalHandoffCreatesExactlyOneLinkedExecutionWithSnapshot` |
| `internal/business/plan` | `Service.Configure`, `Coordinator.Configure` | `TestDependenciesRejectCyclesAndCrossOwnerAndPausePreventsHandoff`, `TestPausingFutureArrangementsDoesNotBlockCurrentPersonalExecution` |
| `internal/business/plan` | `Service.Execute`, `Coordinator.Execute` | `TestPersonalHandoffCreatesExactlyOneLinkedExecutionWithSnapshot`, `TestFailedCommitRollsBackPersonalCompletion` |
| `internal/business/plan` | `Service.List`, `Coordinator.List` | `TestPersonalHandoffCreatesExactlyOneLinkedExecutionWithSnapshot`, `TestNotifyModeDoesNotRunAndPersonalEvidenceCannotBeOverwritten` |
| `internal/business/plan` | `Coordinator.BindTask`, `Coordinator.Migrate` | `TestOrdinaryTaskCreatesOnePlanAndMigrationIsIdempotent` |
| `internal/business/plan` | `Coordinator.ScheduleReady`, `Coordinator.StartScheduled` | `TestScheduledOccurrencesSharePlanAndRemainIndependent` |
| `internal/business/plan` | `Coordinator.Tick` | `TestPersonalHandoffCreatesExactlyOneLinkedExecutionWithSnapshot`, `TestDependenciesRejectCyclesAndCrossOwnerAndPausePreventsHandoff`, `TestNotifyModeDoesNotRunAndPersonalEvidenceCannotBeOverwritten` |
| `internal/business/schedule` | `CalendarTimes` | `TestCalendarUsesRecurrenceTimezoneAndDoesNotAdvancePausedPlan`, `TestCalendarIncludesOverlappingTimeBlocksAndExcludesEndBoundary` |

| `internal/business/plan` | `Coordinator.Read` | `TestReadPlanIncludesVersionsAndInstructions` |

| `internal/business/dream` | `New`, `Coordinator.Get`, `Service.Get`, `Coordinator.Configure`, `Service.Configure`, `Coordinator.Tick` | `TestNightlyScheduleUsesConfiguredTimezone` |
| `internal/business/dream` | `Coordinator.Request`, `Service.Request`, `Coordinator.Runs`, `Service.Runs`, `Coordinator.Prepare`, `Processor.Prepare`, `Coordinator.Commit`, `Processor.Commit` | `TestBoundedReplayAndOfflineCatchup`, `TestSourceChangedAndAssistantIsolation` |
| `internal/business/dream` | `Coordinator.Failed` | `TestFailedDreamManualRetry` |
| `internal/business/dream` | `Coordinator.Undo`, `Service.Undo` | `TestForgetToolGeneratedKnowledgeRemovesCoreAndSources` |
| `internal/business/memory` | `Terms`, `Score` | `TestRecallTermsAndRanking` |

做梦和用户计划共享 `internal/trigger` 的规则计算、漏执行策略和发生点登记。`TestTimerSkipsExpiredWindowAndDoesNotCreatePlans` 验证过期窗口跳过、游标推进和不创建计划／任务；`TestTimerOccurrenceRunAndReplayCursorRollBackTogether` 验证并发去重与原子回滚；`TestTimerSettingChangePreservesReplayProgressAndVersionedDedup`、`TestTimezoneEditKeepsAnAdmittedBatchWindow` 验证设置修订与资料窗口独立；`TestDispatcherRecognizesLegacyOccurrenceLedger` 验证已有计划发生记录升级后不重复投递。

| `internal/business/dream` | `Coordinator.Deadline`, `Coordinator.Active`, `Coordinator.Cancel` | `TestSessionFrozenDeadlineAndCancel`, `TestSessionDeadlinePreservesCommittedMemoryAndBoundsBackfill` |
| `internal/business/dream` | `Coordinator.Windows`, `Service.Windows`, `Coordinator.Conflicts`, `Service.Conflicts` | `TestWindowsAndRecurringConflictsUseSharedRules` |
| `internal/business/dream` | `Coordinator.GetRun`, `Service.GetRun` | `TestSessionDeadlinePreservesCommittedMemoryAndBoundsBackfill` |

固定时限与排队：`TestSessionDeadlinePreservesCommittedMemoryAndBoundsBackfill` 验证批次事件真实生成、共用截止时间、保留完整提交与单次有限积压；`TestDreamDeadlineQueuesMessagesPlansAndRejectsExpedite` 和 `TestDreamManualStopPreservesQueuedMessages` 使用真实调度器及两个模型批次，验证超时／主动停止、未读消息排队、拒绝加急与继续执行。`TestMissedAutomaticWindowDoesNotPreventManualDream` 验证未开始的自动窗口不消耗手动做梦机会。

| `internal/business/plan` | `Coordinator.Events`, `Service.Events` | `TestEventsMergeRecurringOccurrencesAndBindPagination`, `TestEventsCompletionAppliesToOneOccurrence` |
| `internal/business/memory` | `NewFiles` | `TestDiskFilesExternalEditsSummaryAndRestart` |
| `internal/business/memory` | `Files.Get` | `TestFileRepositoryVersionAndIsolation` |
| `internal/business/memory` | `Files.List` | `TestDiskFilesExternalEditsSummaryAndRestart` |
| `internal/business/memory` | `Files.Put` | `TestDiskRefusesLinksAndAtomicCommitCancellation` |
| `internal/business/memory` | `Files.Delete` | `TestFileRepositoryVersionAndIsolation` |
| `internal/business/memory` | `Files.Preview` | `TestDiskForgetRecordsAndCloneExport` |
| `internal/business/memory` | `Files.Clone` | `TestDiskForgetRecordsAndCloneExport` |
| `internal/business/memory` | `Files.Export` | `TestDiskForgetRecordsAndCloneExport` |
| `internal/business/memory` | `Files.Suppressed` | `TestHybridRecallAndSuppression` |
| `internal/business/memory` | `Mutation.Validate` | `TestTDDServiceOwnershipAndValidation` |

| `internal/business/memory` | `Ref.Validate` | `TestSemanticPathsDirectoryMoveAndExplicitForget` |
| `internal/business/memory` | `Ref.LogicalPath` | `TestSemanticPathsDirectoryMoveAndExplicitForget` |
| `internal/business/memory` | `Service.Browse` | `TestSemanticPathsDirectoryMoveAndExplicitForget` |
| `internal/business/memory` | `Service.Mkdir` | `TestSemanticPathsDirectoryMoveAndExplicitForget` |
| `internal/business/memory` | `Service.Move` | `TestSemanticPathsDirectoryMoveAndExplicitForget` |
| `internal/business/memory` | `Service.RemoveDirectory` | `TestSemanticPathsDirectoryMoveAndExplicitForget` |
| `internal/business/memory` | `Files.WriteReview` | `TestReviewFilesUseSemanticPathsAndForgetSourceReferences` |
| `internal/business/memory` | `Files.RemoveReview` | `TestReviewFilesUseSemanticPathsAndForgetSourceReferences` |

| `internal/avatar` | `Normalize` | `internal/auth.TestProfileAvatarLifecycleAndIsolation`、`internal/business/assistant.TestAvatarImageCropAndLimits` |

| `internal/business/dream` | `Coordinator.SavedOutput` | `TestSavedSummaryRecoveryAndIsolation` |
| `internal/business/memory` | `Files.PrepareFileOperation`, `Files.CompleteFileOperation`, `Files.UndoFileOperation`, `Files.ReadFile` | `TestDreamHistoryObservesCommonToolEffectsAndProtectsLaterEdits`, `TestDreamHistoryRestoresDeletedFileWithoutFollowingLinks` |

| `internal/business/dream` | `FileRef`, `Coordinator.BeforeFileChange`, `Coordinator.CheckFileAccess`, `Coordinator.RememberRead` | `internal/app.TestDreamUsesCommonFileExecutorWithScopedAccess`, `TestForgetToolGeneratedKnowledgeRemovesCoreAndSources` |
