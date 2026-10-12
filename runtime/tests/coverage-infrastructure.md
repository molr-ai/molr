# 基础设施与公共类型测试覆盖清单

这里记录测试设计到公开入口的映射，不是已通过的实现覆盖率。各模块已经通过真实实现工厂接入；缺失工厂仍会明确失败。

| 包 | 公开入口 | 测试 |
| --- | --- | --- |
| `internal/core` | `ID.Validate` | `TestTDDCoreIDBoundary` |
| `internal/core` | `Principal.Validate` | `TestTDDCorePrincipalAndExecutionBoundaries` |
| `internal/core` | `Principal.RequireOwner` | `TestTDDCorePrincipalAndExecutionBoundaries` |
| `internal/core` | `Execution.Validate` | `TestTDDCorePrincipalAndExecutionBoundaries` |
| `internal/core` | `Mutation.Validate` | `TestTDDCoreMutationAndPagination` |
| `internal/core` | `PageRequest.Normalize` | `TestTDDCoreMutationAndPagination` |
| `internal/core` | `Money.Validate` | `TestTDDCoreMoneyPrecisionAndCurrency` |
| `internal/core` | `NewSecret` | `TestTDDCoreSecretEveryRepresentationIsRedacted` |
| `internal/core` | `Secret.Reveal` | `TestTDDCoreSecretEveryRepresentationIsRedacted` |
| `internal/core` | `Secret.Empty` | `TestTDDCoreSecretEveryRepresentationIsRedacted` |
| `internal/core` | `Secret.String` | `TestTDDCoreSecretEveryRepresentationIsRedacted` |
| `internal/core` | `Secret.GoString` | `TestTDDCoreSecretEveryRepresentationIsRedacted` |
| `internal/core` | `Secret.Format` | `TestTDDCoreSecretEveryRepresentationIsRedacted` |
| `internal/core` | `Secret.MarshalJSON` | `TestTDDCoreSecretEveryRepresentationIsRedacted` |
| `internal/core` | `Secret.MarshalText` | `TestTDDCoreSecretEveryRepresentationIsRedacted` |
| `internal/core` | `Error.Error` | `TestTDDCoreErrorClassificationAndPrivateCause` |
| `internal/core` | `Error.Unwrap` | `TestTDDCoreErrorClassificationAndPrivateCause` |
| `internal/core` | `NewError` | `TestTDDCoreErrorClassificationAndPrivateCause` |
| `internal/core` | `Wrap` | `TestTDDCoreErrorClassificationAndPrivateCause` |
| `internal/core` | `Invalid` | `TestTDDCoreErrorClassificationAndPrivateCause` |
| `internal/core` | `CodeOf` | `TestTDDCoreErrorClassificationAndPrivateCause` |
| `internal/core` | `WithReason` | `TestReasonsPreserveCodesAndPrivateCauses` |
| `internal/core` | `ReasonOf` | `TestReasonsPreserveCodesAndPrivateCauses` |
| `internal/config` | `Defaults` | `TestTDDConfigDefaultAndDirectTLS`, `TestTDDConfigRejectsUnboundedOrAmbiguousConfiguration` |
| `internal/config` | `Config.Validate` | `TestTDDConfigDefaultAndDirectTLS`, `TestTDDConfigRejectsUnboundedOrAmbiguousConfiguration` |
| `internal/config` | `Config.OwnListenPort` | `TestNetworkPresetBoundaries` |
| `internal/config` | `Load` | `TestTDDConfigLoadStrictAndBounded` |
| `internal/config` | `Duration.Value` | `TestTDDConfigDurationRoundTripAndInvalidInput` |
| `internal/config` | `Duration.MarshalJSON` | `TestTDDConfigDurationRoundTripAndInvalidInput` |
| `internal/config` | `Duration.UnmarshalJSON` | `TestTDDConfigDurationRoundTripAndInvalidInput` |
| `internal/platform` | `AssistantWorkspace` | `TestAssistantWorkspaceIdentityAndOverrides` |
| `internal/platform` | `EnsureWorkspace`, `WorkspaceDirectories` | `TestWorkspaceLayoutPreservesFilesAndRejectsLinks` |
| `internal/platform` | `Runner.Capabilities` | `TestTDDPlatformCapabilitiesAndProcessStreams` |
| `internal/platform` | `Runner.Start` | `TestTDDPlatformCapabilitiesAndProcessStreams` |
| `internal/platform` | `Process.Stdout` | `TestTDDPlatformCapabilitiesAndProcessStreams` |
| `internal/platform` | `Process.Stderr` | `TestTDDPlatformCapabilitiesAndProcessStreams` |
| `internal/platform` | `Process.Wait` | `TestTDDPlatformCapabilitiesAndProcessStreams` |
| `internal/platform` | `Process.Cancel` | `TestTDDPlatformCancellationAndTimeoutReapProcessTree` |
| `internal/platform` | `Lock.Close` | `TestTDDPlatformLockExcludesConcurrentInstanceAndReleases` |
| `internal/platform` | `Locker.Acquire` | `TestTDDPlatformLockExcludesConcurrentInstanceAndReleases` |
| `internal/platform` | `NetworkGuard.Check` | `TestTDDPlatformNetworkRejectsSSRFAndRebinding`, `TestTDDPlatformExplicitLANBindingDoesNotOpenEntireNetwork` |
| `internal/storage/files` | `Service.Upload` | `TestTDDFilesUploadGetOpenDelete`, `TestTDDFilesAccessDeniedBeforeReadingContent`, `TestTDDFilesDisplayNameCannotEscapeStorage` |
| `internal/storage/files` | `Service.Get` | `TestTDDFilesUploadGetOpenDelete`, `TestTDDFilesAccessDeniedBeforeReadingContent`, `TestTDDFilesDisplayNameCannotEscapeStorage` |
| `internal/storage/files` | `Service.Open` | `TestTDDFilesUploadGetOpenDelete`, `TestTDDFilesAccessDeniedBeforeReadingContent`, `TestTDDFilesDisplayNameCannotEscapeStorage` |
| `internal/storage/files` | `Service.Delete` | `TestTDDFilesUploadGetOpenDelete`, `TestTDDFilesAccessDeniedBeforeReadingContent`, `TestTDDFilesDisplayNameCannotEscapeStorage` |
| `internal/storage/files` | `Objects.Put` | `TestTDDFilesObjectsStageCommitOpenRemove` |
| `internal/storage/files` | `Objects.Commit` | `TestTDDFilesObjectsStageCommitOpenRemove` |
| `internal/storage/files` | `Objects.Open` | `TestTDDFilesObjectsStageCommitOpenRemove` |
| `internal/storage/files` | `Objects.Remove` | `TestTDDFilesObjectsStageCommitOpenRemove` |
| `internal/storage/files` | `Objects.Recover` | `TestTDDFilesObjectsRecoverIsRepeatable`, `TestTDDFilesValidationAndCapacityFailuresPublishNothing` |
| `internal/storage/files` | `Repository.Get` | `TestTDDFilesRepositoryOwnershipAndStates` |
| `internal/storage/files` | `Repository.Insert` | `TestTDDFilesRepositoryOwnershipAndStates` |
| `internal/storage/files` | `Repository.SetState` | `TestTDDFilesRepositoryOwnershipAndStates` |
| `internal/storage/files` | `Repository.Delete` | `TestTDDFilesRepositoryOwnershipAndStates` |
| `internal/storage/sqlite` | `Repositories.Auth` | `TestTDDSQLiteRepositoriesAreAvailableInTransaction` |
| `internal/storage/sqlite` | `Repositories.Audit` | `TestTDDSQLiteRepositoriesAreAvailableInTransaction` |
| `internal/storage/sqlite` | `Repositories.Assistants` | `TestTDDSQLiteRepositoriesAreAvailableInTransaction` |
| `internal/storage/sqlite` | `Repositories.Conversations` | `TestTDDSQLiteRepositoriesAreAvailableInTransaction` |
| `internal/storage/sqlite` | `Repositories.IMOutbox` | `TestTDDSQLiteRepositoriesAreAvailableInTransaction` |
| `internal/storage/sqlite` | `Repositories.Tasks` | `TestTDDSQLiteRepositoriesAreAvailableInTransaction` |
| `internal/storage/sqlite` | `Repositories.Todos` | `TestTDDSQLiteRepositoriesAreAvailableInTransaction` |
| `internal/storage/sqlite` | `Repositories.Schedules` | `TestTDDSQLiteRepositoriesAreAvailableInTransaction` |
| `internal/storage/sqlite` | `Repositories.Resources` | `TestTDDSQLiteRepositoriesAreAvailableInTransaction` |
| `internal/storage/sqlite` | `Repositories.Settings` | `TestTDDSQLiteRepositoriesAreAvailableInTransaction` |
| `internal/storage/sqlite` | `Repositories.Credentials` | `TestTDDSQLiteRepositoriesAreAvailableInTransaction` |
| `internal/storage/sqlite` | `Repositories.Policy` | `TestTDDSQLiteRepositoriesAreAvailableInTransaction` |
| `internal/storage/sqlite` | `Repositories.Operations` | `TestTDDSQLiteRepositoriesAreAvailableInTransaction` |
| `internal/storage/sqlite` | `Repositories.ModelReceipts` | `TestTDDSQLiteRepositoriesAreAvailableInTransaction` |
| `internal/storage/sqlite` | `Repositories.Checkpoints` | `TestTDDSQLiteRepositoriesAreAvailableInTransaction` |
| `internal/storage/sqlite` | `Repositories.AssistantEvents` | `TestTDDSQLiteRepositoriesAreAvailableInTransaction` |
| `internal/storage/sqlite` | `Repositories.Realtime` | `TestTDDSQLiteRepositoriesAreAvailableInTransaction` |
| `internal/storage/sqlite` | `Repositories.Queue` | `TestTDDSQLiteRepositoriesAreAvailableInTransaction` |
| `internal/storage/sqlite` | `Repositories.Files` | `TestTDDSQLiteRepositoriesAreAvailableInTransaction` |
| `internal/storage/sqlite` | `Repositories.Idempotency` | `TestTDDSQLiteRepositoriesAreAvailableInTransaction` |
| `internal/storage/sqlite` | `Transactions.Within` | `TestTDDSQLiteCallbackAndCommitErrorsRollBack`, `TestTDDSQLiteMessageOutboxAndVisibleEventCommitTogether`, `TestTDDSQLiteCanceledTransactionsDoNotCommit` |
| `internal/storage/sqlite` | `Database.Within` | `TestTDDSQLiteCallbackAndCommitErrorsRollBack`, `TestTDDSQLiteMessageOutboxAndVisibleEventCommitTogether`, `TestTDDSQLiteCanceledTransactionsDoNotCommit` |
| `internal/storage/sqlite` | `Database.Schema` | `TestTDDSQLiteMigrateSchemaCheckAndClose` |
| `internal/storage/sqlite` | `Database.Migrate` | `TestTDDSQLiteMigrateSchemaCheckAndClose` |
| `internal/storage/sqlite` | `Database.Check` | `TestTDDSQLiteMigrateSchemaCheckAndClose` |
| `internal/storage/sqlite` | `Database.Close` | `TestTDDSQLiteMigrateSchemaCheckAndClose` |
| `internal/storage/sqlite` | `Database.Backup` | `TestTDDSQLiteBackupRestoresDataOfflineAndRevokesAuthentication` |
| `internal/storage/sqlite` | `Restorer.Restore` | `TestTDDSQLiteBackupRestoresDataOfflineAndRevokesAuthentication` |
| `internal/storage/sqlite` | `Idempotency.Get` | `TestTDDSQLiteIdempotencyIsScopedAndPayloadBound`, `TestTDDSQLiteConcurrentIdempotencyKeyHasOneWinner` |
| `internal/storage/sqlite` | `Idempotency.Insert` | `TestTDDSQLiteIdempotencyIsScopedAndPayloadBound`, `TestTDDSQLiteConcurrentIdempotencyKeyHasOneWinner` |

补充行为：受控进程不能继承秘密环境或把参数当 Shell 执行；取消回收子进程；文件大小、配额、失败恢复与路径／符号链接边界；事务句柄过期、提交失败回滚、跨模块原子写入；离线备份恢复不复活认证凭据。工厂中的故障注入针对真实依赖，不能伪造被测接口返回值。

`Secret.Format` 通过 `fmt` 的格式化协议间接调用。`Repositories` 的 accessor 测试验证非 nil 绑定；各仓储业务方法由所属模块测试验证，这不等于 accessor 本身已经证明事务行为。跨仓储事务另有 rollback、message/outbox/realtime 和恢复用例。

SQLite 实现接线与恢复安全：真实数据库聚合器持有独立实例锁；快照带摘要及 schema 清单；恢复必须离线，拒绝符号链接和未 checkpoint 的 WAL/SHM，校验副本后撤销恢复副本中的会话、令牌、挑战及已用验证码，再原子替换数据库。

| 包 | 公开入口 | 测试 |
| --- | --- | --- |
| `internal/storage/sqlite` | `Open` | `TestTDDSQLiteInstanceLockExcludesAnotherOpen`, `TestTDDSQLiteCorruptBackupLeavesOriginalUntouched` |
| `internal/storage/sqlite` | `NewRestorer` | `TestTDDSQLiteCorruptBackupLeavesOriginalUntouched`, `TestTDDSQLiteRestoreRejectsSymlinkSnapshot` |
| `internal/storage/sqlite` | `DB.Store`, `DB.InjectMigrationError` | `TestTDDSQLiteMigrationFailureDoesNotAdvanceVersion` |
| `internal/storage/sqlite` | `DB.FailNextCommit` | `TestTDDSQLiteCallbackAndCommitErrorsRollBack` |
| `internal/storage/sqlite` | `DB.Close`, `DB.Check`, `DB.Migrate`, `DB.Schema` | `TestTDDSQLiteMigrateSchemaCheckAndClose` |
| `internal/storage/sqlite` | `DB.Within` | `TestTDDSQLiteMessageOutboxAndVisibleEventCommitTogether`, `TestTDDSQLiteCommitPersistsAcrossReopen` |
| `internal/storage/sqlite` | `DB.Backup` | `TestTDDSQLiteBackupRestoresDataOfflineAndRevokesAuthentication`, `TestTDDSQLiteCorruptBackupLeavesOriginalUntouched` |

实际存储、系统边界及生产组合实现：

| 包 | 公开入口 | 测试 |
| --- | --- | --- |
| `internal/persistence` | `CheckContext`, `Conflict`, `Decode`, `Key`, `Load`, `Missing`, `Scan`, `Store.Read`, `Store.Write`, `Tx.Delete`, `Tx.Get`, `Tx.Put`, `Tx.Scan`, `Tx.Store` | `TestPersistenceHelpersOnRealTransactions` |
| `internal/persistence/sqlitestore` | `Open`, `Store.Close`, `Store.Path`, `Store.Read`, `Store.Write` | `TestStoreCommitRollbackAndReopen` |
| `internal/persistence/sqlitestore` | `Store.FailNextCommit` | `TestStoreReadOnlyCanceledAndCommitFailure` |
| `internal/persistence/sqlitestore` | `Store.Snapshot` | `TestStoreScanDeleteAndSnapshot` |
| `internal/persistence` | `Store.Watch`, `Tx.NotifyAfterCommit` | `internal/persistence/sqlitestore.TestCommitNotificationsSavepointsAndFailures` |
| `internal/persistence` | `Watch.Changes`, `Watch.Close`, `Watch.Done` | `internal/persistence/sqlitestore.TestWatchLifecycleAndScope` |
| `internal/persistence/sqlitestore` | `Store.Watch` | `TestCommitNotificationsSavepointsAndFailures`、`TestWatchLifecycleAndScope`、`TestConcurrentWatchCloseAndPublish` |
| `internal/platform` | `NewRunner`, `ProcessRunner.Capabilities`, `ProcessRunner.Start` | `TestTDDPlatformCapabilitiesAndProcessStreams` |
| `internal/platform` | `NewLocker`, `FileLocker.Acquire` | `TestTDDPlatformLockExcludesConcurrentInstanceAndReleases` |
| `internal/platform` | `NewNetworkGuard`, `Guard.Check` | `TestTDDPlatformNetworkRejectsSSRFAndRebinding` |
| `internal/platform` | `Guard.HTTPClient` | `TestNetworkHTTPClientChecksActualDialAndRevocation` |
| `internal/storage/files` | `NewObjects`, `ObjectStore.Put`, `ObjectStore.Commit`, `ObjectStore.Open`, `ObjectStore.Remove`, `ObjectStore.Close` | `TestTDDFilesObjectsStageCommitOpenRemove` |
| `internal/storage/files` | `ObjectStore.Recover` | `TestTDDFilesObjectsRecoverIsRepeatable` |
| `internal/storage/files` | `ObjectStore.FailNextCommit` | `TestTDDFilesValidationAndCapacityFailuresPublishNothing` |
| `internal/storage/files` | `NewRepository` | `TestTDDFilesRepositoryOwnershipAndStates` |
| `internal/storage/files` | `NewService` | `TestTDDFilesUploadGetOpenDelete` |
| `internal/app` | `Build`, `Bundle.Close` | `TestBuildRealMessageAgentAndRestart` |
| `internal/platform` | `Guard.CheckOwner`, `Guard.OwnerHTTPClient` | `TestOwnerNetworkRequiresApprovedBindingAndLiveIdentity` |

| `internal/pricing` | `New`, `Catalog.Lookup` | `TestDeepSeekScheduleAndExactModelMatching` |
| `internal/pricing` | `Catalog.Reload` | `TestReloadValidatesAndPreservesSnapshots` |
| `internal/pricing` | `Rates.UnmarshalJSON` | `TestRatesRequireExplicitIntegers` |

| `internal/core` | `AssistantBudget.Validate` | `TestAssistantBudgetRequiresLimitAndCalendarPeriod` |

| `internal/pricing` | `NewExchange`, `Exchange.Reload`, `Exchange.Snapshot`, `ExchangeReader.Snapshot`, `ExchangeSnapshot.Convert` | `TestExchangePreservesSnapshotsAndIntegerAmounts` |

| `internal/debugtrace` | `New`, `Service.Begin`, `Service.Record`, `Service.Finish`, `Service.Runs`, `Service.Get`, `Service.Detail`, `Service.Revision`, `Reader.Runs`, `Reader.Get`, `Reader.Detail`, `Reader.Revision` | `TestCaptureIsolationRedactionAndSnapshots` |
| `internal/debugtrace` | `Service.Trajectory`, `Reader.Trajectory`, `Control.Trajectory` | `TestTrajectoryPagingLiveBackfillAndScopedCursors`, `TestTrajectoryRetentionLegacyPreviewAndIncompletePayload`, `TestTrajectoryOwnerIsolationAndPreviewBounds` |
| `internal/debugtrace` | `Service.Recover` | `TestLimitsRecoveryAndDistinctAttempts` |
| `internal/debugtrace` | `Service.Checkpoint`, `Reader.Checkpoint` | `TestCheckpointOwnerOnlyAndObservationFailureDoesNotFailExecution` |

| `internal/debugtrace` | `Control.Runs`, `Control.Get`, `Control.Detail`, `Control.Revision` | `TestCaptureIsolationRedactionAndSnapshots` |
| `internal/debugtrace` | `Control.Checkpoint` | `TestCheckpointOwnerOnlyAndObservationFailureDoesNotFailExecution` |
| `internal/debugtrace` | `Control.ResetContext`, `Service.ResetContext` | `TestResetControlGatesMutationAndNotifiesOnlyOnSuccess` |

## Linux Agent Sandbox

以下真实 Linux 用例由 `make test-sandbox` 运行；普通 Go 测试明确跳过它们，接口清单检查不能替代真实内核隔离验证。

| 包 | 公开入口 | 测试 |
| --- | --- | --- |
| `internal/platform/sandbox` | `New`, `Runner.Capabilities`, `Runner.Start` | `TestSandboxFailsClosed`, `TestPlanRejectsUnsafeMountsAndUnboundedExecution`, `TestLinuxRealSandboxIsolation`, `TestLinuxRealDirectMountAndReadonly`, `TestLinuxRealCgroupLimitsAndExpiry` |
| `internal/platform/sandbox` | `Runner.CancelOperation`, `Runner.StopOwner`, `Runner.Close` | `TestLinuxRealExecutionScopedControl`, `TestLinuxRealCancelKillsDetachedChildren` |
| `internal/platform/sandbox` | `HelperMain` | `TestLinuxRealSandboxIsolation`, `TestLinuxRealSyscallsAndTempLimit`, `TestLinuxRealTransparentNetworkAndDenied` |

| `internal/platform/sandbox` | `Runner.Prepare` | `TestAutomaticPreparationWithoutPrivateDirectoryFailsClosed`, `TestLinuxAutomaticPreparationUsesOrdinaryDirectories` |
| `internal/platform/sandbox` | `SupervisorMain` | `TestLinuxAutomaticSupervisorDelegatesWithoutCallerConfiguration` |

## Shared timed triggers

| 包 | 公开入口 | 测试 |
| --- | --- | --- |
| `internal/trigger` | `Rule.Validate`, `First` | `TestRuleValidationAndFirst`, `TestDueAndCalendarAgreeAcrossDSTAndRestart` |
| `internal/trigger` | `Due` | `TestDueMissedPoliciesAndCursor`, `TestDueAndCalendarAgreeAcrossDSTAndRestart` |
| `internal/trigger` | `Between` | `TestDueAndCalendarAgreeAcrossDSTAndRestart` |
| `internal/trigger` | `RecordOccurrence` | `TestOccurrenceNamespacesAtomicRollbackAndConcurrentDedup` |

| `internal/trigger` | `VisitUpcoming` | `TestVisitUpcomingPreservesCountAndLocalDST` |

| 包 | 公开入口 | 测试 |
| --- | --- | --- |
| `internal/sandboxmanager` | `Identity.Execution`, `New`, `Manager.Submit`, `Manager.Get`, `Manager.Close` | `TestDurableDeduplicationAndDigest`, `TestRecoveryNeverReplaysUnknownEffects` |
| `internal/sandboxmanager` | `Manager.Renew`, `Manager.Cancel`, `Manager.Revoke` | `TestRevocationFencesLateRequestsAndCancelsRunning`, `TestLeaseExpiryStopsWork`, `TestTLSCredentialAndPinnedIdentity` |
| `internal/sandboxmanager` | `Manager.Read` | `TestReadEndpointCannotMutate` |
| `internal/sandboxmanager` | `Handler`, `NewClient`, `Client.Info`, `Client.Get`, `Client.Execute`, `Client.Await`, `Client.Cancel`, `Client.Revoke` | `TestTLSCredentialAndPinnedIdentity`, `TestRevocationFencesLateRequestsAndCancelsRunning` |
| `internal/sandboxmanager` | `Backend.Info`, `Backend.Call`, `Backend.Cancel`, `Backend.Close` | `TestTLSCredentialAndPinnedIdentity`, `TestRevocationFencesLateRequestsAndCancelsRunning` |
| `internal/app` | `ServeSandbox` | `TestStandaloneSandboxRequiresTLS`, `TestRemoteSandboxStorageRoutingAuthorizationAndContext` |
| `internal/config` | `SandboxNetwork.Target` | `TestSandboxNetworkConfiguration` |
| `internal/platform` | `CompileNetworkTarget`, `NetworkRule.Allows`, `NetworkRule.AllowsPort` | `TestNetworkRuleAddressAndPortSets`, `TestNetworkRuleExclusions`, `TestNetworkTargetRejectsInvalidConfiguration` |

| `internal/debugtrace` | `Service.RecordCheckpoint` | `TestCheckpointRecordingScopeAndDeploymentGate` |

| `internal/config` | `Sandbox.NetworkTargets` | `TestSandboxNetworkRequiresExplicitProtocolsAndPresets` |
| `internal/platform` | `NetworkRule.AllowsAddress`, `NetworkRule.AllowsTransport`, `NetworkRule.MayResolve` | `TestNetworkTransportAndDNSAuthorization` |

| `internal/platform/sandbox` | `Runner.Terminal` | `TestLinuxInteractiveShellProfileAndInterrupt` |
| `internal/config` | `Sandbox.UsesHostTools` | `TestHostToolsInheritsDeploymentAccount` |


### 资源额度与端口转发补充（2026-10-02）

跨包测试使用完整包名明确指出真实集成调用位置；以下清单不是硬配额实机验证的替代。

| 包 | 公开入口 | 验证 |
| --- | --- | --- |
| `internal/platform` | `ValidateForwardPorts` | `TestForwardPortsAreBoundedUniqueTCPPorts` |
| `internal/platform/sandbox` | `Runner.ComputeUsage`, `Runner.StopAssistant` | `TestLinuxComputeQuotaSharesParentAcrossConcurrentExecutions` |
| `internal/platform/sandbox` | `Runner.NetworkUsage` | `TestLinuxNetworkQuotaSharedAcrossExecutions` |
| `internal/platform/sandbox` | `Runner.DialForward` | `internal/app.TestShellForwardRealLocalAndRemote` |
| `internal/sandboxmanager` | `Client.DialForward` | `internal/app.TestShellForwardRealLocalAndRemote` |
| `internal/sandboxmanager` | `Client.QuotaUsage`, `Client.UpdateQuotas` | `TestQuotaControlsBindExecutionIdentityAndServiceCredential` |
| `internal/portforward` | `Framed.Read`, `Framed.Write`, `Framed.CloseWrite`, `Stream.Read`, `Stream.Write`, `Stream.Close`, `Stream.CloseWrite` | `TestFramedBidirectionalHalfClose`, `TestFramedRejectsOversizedAndTruncatedData` |
| `internal/portforward` | `Bridge` | `internal/app.TestShellForwardRealLocalAndRemote` |
| `internal/quota` | `Ledger.Reserve`, `Ledger.Settle`, `Ledger.Usage` | `TestSharedQuotaConcurrentReservationsAndRestart`, `TestUnknownOutcomeRetainsCreditAcrossPeriodAndRestart` |
| `internal/quota` | `Ledger.Claim`, `Ledger.Release` | `TestConnectionSlotsSharedAndReleased` |
| `internal/resourcequota` | `ApplyWorkspace`, `WorkspaceStatus` | `TestWorkspaceQuotaFailsClosedWithoutProvisioning` |

### 多用户资源目录和双层配额（2026-10-03）

| 包 | 公开入口 | 验证 |
| --- | --- | --- |
| `internal/core` | `ResourceConstraints.Validate`, `ResourceConstraints.Kind` | `TestResourceConstraintsRejectAmbiguousOrUnboundedLimits` |
| `internal/persistence` | `CatalogOwner`, `LoadCatalog` | `TestSharedCatalogKeepsPrivateNamespaces` |
| `internal/quota` | `Ledger.ReserveMany`, `Ledger.ClaimMany` | `TestGroupTrafficBudgetSharedByAssistantsNotUsers`, `TestGroupPeriodChangePreservesTrafficAndPendingReservations` |
| `internal/platform/sandbox` | `Runner.StopAccess` | `internal/app.TestFullAccessAndIsolationCoexist` |

| `internal/debugtrace` | `Control.AdminRetention`, `Control.Retention`, `Control.SetRetention`, `Reader.Retention`, `Service.AdminRetention`, `Service.Retention`, `Service.SetRetention` | `TestRetentionAdministrationPersistenceConflictsAndValidation`, `TestCountRetentionIgnoresAgeAndAlwaysSavesNewDetails` |

### 额度核算与索引查询（2026-10-05）

| 包 | 公开入口 | 验证 |
| --- | --- | --- |
| `internal/persistence` | `Eq`, `Select`, `Tx.Query` | `internal/persistence/sqlitestore.TestIndexedQueriesPaginationProjectionAndPlan` |
| `internal/quota` | `Ledger.SettleMany` | `TestNetworkAtomicFailureRestartAndNestedRead` |
| `internal/quota` | `Budgets.Reserve`, `Budgets.ReserveObserved`, `Budgets.Settle`, `Budgets.SetReservationState`, `Budgets.Usage` | `internal/policy.TestBudgetCalendarWeekTimezoneAndDST`, `internal/policy.TestTDDBudgetSettlementUnknownAndRelease` |
| `internal/quota` | `ModelRecords`, `ModelByOperation`, `ModelTotals`, `CheckMoney`, `AttachmentBytes` | `internal/app.TestMultiUserPrivateDataAndCatalogPermissions`, `internal/app.TestUsageViewsMatchAllowanceAcrossPeriods` |
| `internal/quota` | `CallTotals`, `CheckCalls`, `Exceeded`, `InPeriod` | `TestCallUsagePendingCarryAndProjection` |
| `internal/quota` | `Period` | `TestGroupPeriodChangePreservesTrafficAndPendingReservations` |
| `internal/platform` | `UserGlobal` | `TestUserGlobalOwnerIdentity` |
| `internal/textfiles` | `OpenRoot` | `TestPinnedFileLifecycleAndVersions` |
| `internal/textfiles` | `Open` | `TestPinnedFileLifecycleAndVersions` |
| `internal/textfiles` | `OpenDirectory` | `TestPinnedFileLifecycleAndVersions` |
| `internal/textfiles` | `Version` | `TestPinnedFileLifecycleAndVersions` |
| `internal/textfiles` | `Changed` | `TestPinnedFileLifecycleAndVersions` |
| `internal/textfiles` | `Replace` | `TestPinnedFileLifecycleAndVersions` |
| `internal/textfiles` | `CreateOutput` | `TestPinnedFileLifecycleAndVersions` |
| `internal/textfiles` | `AppendOutput` | `TestPinnedFileLifecycleAndVersions` |
| `internal/textfiles` | `MkdirAll` | `TestPinnedFileLifecycleAndVersions` |
| `internal/textfiles` | `Remove` | `TestPinnedFileLifecycleAndVersions` |

| `internal/textfiles` | `Move` | `TestMoveAndRemoveDirectoryDoNotReplaceTargets` |
| `internal/textfiles` | `RemoveDirectory` | `TestMoveAndRemoveDirectoryDoNotReplaceTargets` |
| `internal/textfiles` | `Search` | `TestSearchContentPathsLimitsAndHiddenRoot` |
| `internal/textfiles` | `SearchOptions.Validate` | `TestSearchRejectsInvalidRegexAndRechecksAuthorization` |

| 包 | 接口 / 方法 | 测试 |
| --- | --- | --- |
| `internal/platform` | `PublicHTTPClient` | `TestPublicHTTPClientRejectsPrivateAndPlainHTTP` |
| `internal/app` | `Bundle.InitializeSetup` | `TestSetupAtomicCompletionAndConcurrentRetry` |
| `internal/config` | `HTTP.PlainHTTP`、`PrivateHTTPHost` | `TestLANHTTPConfigurationBoundaries` |
| `internal/webassets` | `Handler` | `TestWebUIRouting` |

| `internal/storage/files` | `ChannelStore.UploadChannel`、`ChannelStore.ValidateChannelFile`、`ChannelStore.OpenChannel` | `internal/app.TestWeixinMediaAndContextUseLiveBinding`、`internal/im/weixin.TestMediaIngressDeduplicationAndIndependentDelivery` |

| `internal/voicecodec` | `WAV` | `TestWAVMatchesReferenceDecoderAndHasNoHostImports`、`TestWAVRejectsMalformedOversizedAndCanceledInput` |

| `internal/persistence` | `TimeKey` | `internal/business/schedule.TestDueQueryBoundsHistoryAndPreservesExactInstants`、`internal/business/dream.TestExpiryUsesOnlyDueActiveSessionsAcrossBatches` |

| 包 | 公开入口 | 验证入口 |
| --- | --- | --- |
| `internal/persistence` | `Page` | `internal/persistence/sqlitestore.TestTimeKeysAndKeysetPagePreserveNanosecondsAndOffsets`、`internal/auth.TestCredentialLookupsAndSessionPagesBoundHistory` |
| `internal/persistence` | `SelectCatalog` | `TestCatalogSelectionKeepsPrivateNamespaces` |
| `internal/persistence` | `Tx.Sum`, `Tx.DeleteMatching` | `internal/persistence/sqlitestore.TestScopedAggregateAndDeletionRespectTransactions` |
| `internal/persistence` | `Tx.Count` | `internal/persistence/sqlitestore.TestCountRespectsScopeLimitAndTransactions` |

| `internal/debugtrace` | `Control.WatchRevision`, `Reader.WatchRevision`, `Service.WatchRevision` | `TestRevisionWatchCommitRollbackScopeAndClose`, `TestRevisionWatchAuthorization` |
| `internal/persistence` | `Tx.Ancestors` | `internal/persistence/sqlitestore.TestAncestorsBoundedOrderedAndTransactional` |
| `internal/persistence` | `Tx.EncodedBytes` | `internal/persistence/sqlitestore.TestEncodedBytesRespectsScopeAndOriginalEncoding`, `internal/persistence/sqlitestore.TestEncodedBytesObservesTransactionAndRollback` |
| `internal/contextstore` | `Save`, `SaveChanged`, `Load`, `Release`, `Collect` | `TestSharedPrefixesScopedReferencesAndCollection`, `TestStableCanonicalIdentityAndRollback`, `TestSaveChangedSkipsAllWritesForIdenticalContent`, `TestConcurrentReferencesSurviveGC` |
| `internal/contextstore` | `Intern`, `Append` | `TestAppendIdentityAndLinearStorage`, `TestLongestPrefixUsesBinarySearch` |
| `internal/contextstore` | `AccountUsage`, `GlobalUsage` | `TestUsageDeduplicatesWithinScopeAndTracksCollection`, `TestUsagePaginatesReferencesAndObjects`, `TestGlobalUsageDoesNotReadContextPayloads` |
| `internal/contextstore` | `ReleaseKind`, `RunGC` | `TestReleaseKindScopesReferencesAndBackgroundCollection` |
| `internal/retry` | `Policy.Delay` | `TestDefaultPolicy`, `TestPolicyBounds` |

2026-10-11：补齐构建信息接口映射。

| 包 | 接口 / 方法 | 测试 |
| --- | --- | --- |
| `internal/buildinfo` | `UserAgent` | `internal/agent/model.TestTDDModelCatalogAndGenerationRequireCurrentAuthorization` |
| `internal/buildinfo` | `Current` | `TestInjectedReleaseIdentityWinsOverVCSFallback` |
| `internal/releaseplatform` | `Supported` | `TestSupportMatchesInstallableTargets` |
| `internal/updates` | `Compare`, `Development` | `TestVersionPrecedence` |
| `internal/updates` | `New`, `Service.Check`, `Service.Snapshot` | `TestCheckCoalescesAndRetainsSuccessfulResult`, `TestReleaseChannelIsolation` |
| `internal/updates` | `Service.Run` | `TestCanceledLifecycleDoesNotCheck` |
| `internal/updates` | `ReadInstallation` | `TestInstallationReceiptRequiresMatchingExecutableAndCompletedInstall` |
| `internal/config` | `Sandbox.ComputeCeilings` | `internal/app.TestSandboxPlansRequireIndependentComputeStorageAndNetworkGrants` |
| `internal/core` | `CheckGPUDevices`, `ComputeConstraints.GPUDevices`, `ValidateGPUDevices` | `TestGPUDevicesAreExplicitAndBounded` |
| `internal/platform` | `ConfiguredHTTPClient` | `TestConfiguredHTTPClientPinsExplicitLocalSource` |
| `internal/platform/sandbox` | `Runner.GPUs` | `TestLinuxGPUIsolation` |
| `internal/installation` | `Run`, `VerificationPending.Error` | `TestCommandHelpAndVersionWithoutPackage` |
| `internal/installation` | `ComponentError.Error`, `Task.Active` | `TestComponentDownloadChecksDigestSizeAndSource`, `TestMaintenanceLockSerializesCLIAndRuntime` |
| `internal/installation` | `NewMaintenance`, `Maintenance.CanUpdate`, `Maintenance.Cancel`, `Maintenance.Enabled`, `Maintenance.Latest`, `Maintenance.Programs`, `Maintenance.ProtectedRoots`, `Maintenance.Snapshot`, `Maintenance.Start`, `Maintenance.Task` | `TestUnmanagedMaintenanceFailsClosed`, `internal/app.TestMaintenanceAPIRequiresAuthenticationCSRFAndKnownRequests` |
| `internal/installrecord` | `Instance`, `Read`, `Record.File`, `Record.UpdateSource`, `Record.Validate` | `TestReceiptRejectsUnrelatedPathsAndWritableRecords` |

## `internal/browser`

| 包 | 接口 / 方法 | 测试 |
| --- | --- | --- |
| `internal/browser` | `Action.Args`, `Action.ReadOnly` | `TestActionDoesNotExposeLaunchOrArbitraryExecution` |
| `internal/browser` | `HelperMain` | `internal/app.TestBrowserRealTaskStreamTakeoverAndPersistence` |
