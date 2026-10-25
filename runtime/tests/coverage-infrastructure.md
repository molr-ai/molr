# 基础设施与公共类型测试覆盖清单

这里记录测试设计到公开入口的映射，不是已通过的实现覆盖率。各模块已经通过真实实现工厂接入；缺失工厂仍会明确失败。

| 包 | 公开入口 | 测试 |
| --- | --- | --- |
| `internal/sandboxidentity` | `Encode`, `Decode` | `TestConnectionCodeVersionValidationAndRedaction`, `TestRenewalPreservesIdentityAndRecoversExpiredStartup` |
| `internal/sandboxidentity` | `PublicKeySHA256`, `ServerTLS` | `TestRenewalPreservesIdentityAndRecoversExpiredStartup`, `TestCertificateHotReloadAndCustomIdentityBoundaries` |
| `internal/logging` | `DefaultOptions` | `internal/config.TestLoggingPolicyDefaultsAndJSONOverrides` |
| `internal/logging` | `Options.Validate` | `TestInvalidPolicyHasNoFilesystemEffects`, `internal/config.TestLoggingPolicyDefaultsAndJSONOverrides` |
| `internal/logging` | `NewFile`, `File.Write` | `TestFileRotationRetainsWholeRecordsAndDeletesOldest`, `TestConcurrentWritesStayIntactAcrossRotation`, `TestOversizedRecordAndInvalidDestinationDoNotDestroyLogs` |
| `internal/logging` | `OpenFile` | `TestRestartAppliesReducedRetentionAndPreservesOtherFiles`, `TestFileRejectsSymlinks`, `TestInvalidPolicyHasNoFilesystemEffects` |
| `internal/core` | `CompressionTokens` | `TestCompressionTokens` |
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
| `internal/config` | `SandboxPreset` | `TestResourcePresetsExpandAndRoundTrip` |
| `internal/config` | `Duration.Value` | `TestTDDConfigDurationRoundTripAndInvalidInput` |
| `internal/config` | `Duration.MarshalJSON` | `TestTDDConfigDurationRoundTripAndInvalidInput` |
| `internal/config` | `Duration.UnmarshalJSON` | `TestTDDConfigDurationRoundTripAndInvalidInput` |
| `internal/platform` | `NativeSandbox` | `TestNativeSandboxSelectsHostPlatform` |
| `internal/platform` | `MachineCapacity`, `FilesystemCapacity` | `TestMachineCapacity`, `TestWindowsCapacitySnapshot` |
| `internal/platform` | `ResourceEnvironment` | `internal/platform/sandbox.TestSeatbeltRemoteExecutionScopes`, `internal/app.TestRemoteNativeSandboxIntegration` |
| `internal/platform` | `Capabilities.ControlledReady` | `TestControlledReadyRequiresBackendIsolation` |
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
| `internal/storage/files` | `Objects.Publish` | `TestTDDFilesObjectsStageCommitOpenRemove` |
| `internal/storage/files` | `Objects.Open` | `TestTDDFilesObjectsStageCommitOpenRemove` |
| `internal/storage/files` | `Objects.Remove` | `TestTDDFilesObjectsStageCommitOpenRemove` |
| `internal/storage/files` | `Objects.Recover` | `TestTDDFilesObjectsRecoverIsRepeatable`, `TestTDDFilesValidationAndCapacityFailuresPublishNothing` |
| `internal/storage/files` | `Repository.Get` | `TestTDDFilesRepositoryOwnershipAndStates` |
| `internal/storage/files` | `Repository.Insert` | `TestTDDFilesRepositoryOwnershipAndStates` |
| `internal/storage/files` | `Repository.SetState` | `TestTDDFilesRepositoryOwnershipAndStates` |
| `internal/storage/files` | `Repository.Delete` | `TestTDDFilesRepositoryOwnershipAndStates` |
| `internal/storage/sqlite` | `Database.Schema` | `TestTDDSQLiteMigrateSchemaCheckAndClose` |
| `internal/storage/sqlite` | `Database.Migrate` | `TestTDDSQLiteMigrateSchemaCheckAndClose`, `TestPreviousSchemaIsRejectedWithoutConversion` |
| `internal/storage/sqlite` | `Database.Check` | `TestTDDSQLiteMigrateSchemaCheckAndClose` |
| `internal/storage/sqlite` | `Database.Close` | `TestTDDSQLiteMigrateSchemaCheckAndClose` |
| `internal/storage/sqlite` | `Database.Backup` | `TestTDDSQLiteBackupRestoresDataOfflineAndRevokesAuthentication` |
| `internal/storage/sqlite` | `Restorer.Restore` | `TestTDDSQLiteBackupRestoresDataOfflineAndRevokesAuthentication` |

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
| `internal/storage/sqlite` | `DB.Backup` | `TestTDDSQLiteBackupRestoresDataOfflineAndRevokesAuthentication`, `TestTDDSQLiteCorruptBackupLeavesOriginalUntouched` |

实际存储、系统边界及生产组合实现：

| 包 | 公开入口 | 测试 |
| --- | --- | --- |
| `internal/persistence` | `CheckContext`, `Conflict`, `Decode`, `Key`, `Load`, `Missing`, `Scan`, `Store.Read`, `Store.Write`, `Tx.Delete`, `Tx.Get`, `Tx.Put`, `Tx.Scan`, `Tx.Store` | `TestPersistenceHelpersOnRealTransactions` |
| `internal/persistence` | `Insert`, `SaveVersion`, `DeleteVersion` | `TestVersionedWritesPreserveConflictsAndRollback`, `TestVersionedWritesSerializeConcurrentUpdates` |
| `internal/persistence` | `PayloadDigest`, `ReplayReceipt` | `TestReceiptReplayPreservesScopeDigestAndErrors`, `TestReceiptAndBusinessWritesRollbackTogether` |
| `internal/persistence` | `MutateReceipt` | `TestMutateReceiptReplaysAndPropagatesFailures`, `TestReceiptAndBusinessWritesRollbackTogether` |
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
| `internal/storage/files` | `NewObjects`, `ObjectStore.Put`, `ObjectStore.Publish`, `ObjectStore.Open`, `ObjectStore.Remove`, `ObjectStore.Close` | `TestTDDFilesObjectsStageCommitOpenRemove` |
| `internal/storage/files` | `ObjectStore.Recover` | `TestTDDFilesObjectsRecoverIsRepeatable` |
| `internal/storage/files` | `ObjectStore.FailNextCommit` | `TestTDDFilesValidationAndCapacityFailuresPublishNothing` |
| `internal/storage/files` | `NewRepository` | `TestTDDFilesRepositoryOwnershipAndStates` |
| `internal/storage/files` | `NewService` | `TestTDDFilesUploadGetOpenDelete` |
| `internal/app` | `Build`, `Bundle.Close` | `TestBuildRealMessageAgentAndRestart` |
| `internal/platform` | `Guard.CheckOwner`, `Guard.OwnerHTTPClient` | `TestOwnerNetworkRequiresApprovedBindingAndLiveIdentity` |

| `internal/pricing` | `New`, `Catalog.Lookup` | `TestDeepSeekScheduleAndExactModelMatching`, `TestProviderPricesMatchExactProductAndCurrency`, `TestTierSnapshotReserveAndPromotionExpiry` |
| `internal/pricing` | `Catalog.Reload` | `TestReloadValidatesAndPreservesSnapshots` |
| `internal/pricing` | `Rates.UnmarshalJSON` | `TestRatesRequireExplicitIntegers` |
| `internal/pricing` | `NewUpdater`, `Updater.Refresh` | `TestPricingFileRefreshCacheAndRecovery`, `TestPricingURLConditionalRefreshAndFailureRetention`, `TestPricingUpdaterDefaultsValidationAndCacheFailure`, `TestPricingUpdaterPollingAndConcurrentCancellation`, `TestPricingURLRejectsOversizeAndUnexpectedNotModified` |
| `internal/pricing` | `Updater.Run` | `TestPricingUpdaterDefaultsValidationAndCacheFailure`, `TestPricingUpdaterPollingAndConcurrentCancellation` |

| `internal/core` | `AssistantBudget.Validate` | `TestAssistantBudgetRequiresLimitAndCalendarPeriod` |

| `internal/pricing` | `NewExchange`, `Exchange.Reload`, `Exchange.Snapshot`, `ExchangeReader.Snapshot`, `ExchangeSnapshot.Convert` | `TestExchangePreservesSnapshotsAndIntegerAmounts` |

| `internal/debugtrace` | `New`, `Service.Begin`, `Service.Record`, `Service.Finish`, `Service.Runs`, `Service.Get`, `Service.Detail`, `Service.Revision`, `Reader.Runs`, `Reader.Get`, `Reader.Detail`, `Reader.Revision` | `TestCaptureIsolationRedactionAndSnapshots` |
| `internal/debugtrace` | `Service.Trajectory`, `Reader.Trajectory`, `Control.Trajectory` | `TestTrajectoryPagingLiveBackfillAndScopedCursors`, `TestTrajectoryRetentionLegacyPreviewAndIncompletePayload`, `TestTrajectoryOwnerIsolationAndPreviewBounds`, `TestTrajectorySummarySyncStateCleanupAndRollback`, `TestTrajectorySummaryCursorIndependentOfStepPaging`, `TestTrajectorySummarySyncScopeResetAndAuthorization`, `TestTrajectoryIdleSyncDoesNotReadRetainedHistory` |
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
| `internal/sandboxmanager` | `Client.Directory` | `TestDirectoryClientRoundTripAndBackendErrors`, `TestDirectoryClientRequiresServiceAuthentication`, `TestDirectoryClientRejectsUnsupportedBackend` |
| `internal/sandboxmanager` | `Backend.Info`, `Backend.Call`, `Backend.Cancel`, `Backend.Close` | `TestTLSCredentialAndPinnedIdentity`, `TestRevocationFencesLateRequestsAndCancelsRunning` |
| `internal/app` | `ServeSandbox` | `TestStandaloneSandboxRequiresTLS`, `TestRemoteSandboxStorageRoutingAuthorizationAndContext` |
| `internal/config` | `SandboxNetwork.Target` | `TestSandboxNetworkConfiguration` |
| `internal/platform` | `CompileNetworkTarget`, `NetworkRule.Allows`, `NetworkRule.AllowsPort` | `TestNetworkRuleAddressAndPortSets`, `TestNetworkRuleExclusions`, `TestNetworkTargetRejectsInvalidConfiguration` |

| `internal/debugtrace` | `Service.RecordCheckpoint` | `TestCheckpointRecordingScopeAndDeploymentGate` |

| `internal/config` | `Sandbox.NetworkTargets` | `TestSandboxNetworkRequiresExplicitProtocolsAndPresets` |
| `internal/platform` | `NetworkRule.AllowsAddress`, `NetworkRule.AllowsTransport`, `NetworkRule.MayResolve` | `TestNetworkTransportAndDNSAuthorization` |

| `internal/platform/sandbox` | `Runner.Terminal` | `TestLinuxInteractiveShellProfileAndInterrupt`, `TestWindowsInteractiveTerminalIsolationAndConcurrentSessions`, `TestWindowsTerminalLifecycle` |
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
| `internal/textfiles` | `SearchFS` | `internal/app.TestBuiltinDocsWithoutSandbox`, `internal/app.TestBuiltinDocsRejectMutationsAndForeignPaths` |
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
| `internal/persistence` | `Tx.Sum`, `Tx.DeleteMatching` | `internal/persistence/sqlitestore.TestScopedAggregateAndDeletionRespectTransactions` |
| `internal/persistence` | `Tx.Count` | `internal/persistence/sqlitestore.TestCountRespectsScopeLimitAndTransactions` |

| `internal/debugtrace` | `Control.WatchRevision`, `Reader.WatchRevision`, `Service.WatchRevision` | `TestRevisionWatchCommitRollbackScopeAndClose`, `TestRevisionWatchAuthorization` |
| `internal/persistence` | `Tx.Ancestors` | `internal/persistence/sqlitestore.TestAncestorsBoundedOrderedAndTransactional` |
| `internal/persistence` | `Tx.EncodedBytes` | `internal/persistence/sqlitestore.TestEncodedBytesRespectsScopeAndOriginalEncoding`, `internal/persistence/sqlitestore.TestEncodedBytesObservesTransactionAndRollback` |
| `internal/contextstore` | `Save`, `SaveChanged`, `Load`, `Release`, `Collect` | `TestSharedPrefixesScopedReferencesAndCollection`, `TestStableCanonicalIdentityAndRollback`, `TestSaveChangedSkipsAllWritesForIdenticalContent`, `TestConcurrentReferencesSurviveGC` |
| `internal/contextstore` | `Intern`, `Append` | `TestAppendIdentityAndLinearStorage`, `TestLongestPrefixUsesBinarySearch` |
| `internal/contextstore` | `AccountUsage`, `GlobalUsage` | `TestUsageDeduplicatesWithinScopeAndTracksCollection`, `TestUsagePaginatesReferencesAndObjects`, `TestGlobalUsageDoesNotReadContextPayloads`, `TestAccountUsageReadsOnlyBatchedMetadata` |
| `internal/contextstore` | `ReleaseKind`, `RunGC` | `TestReleaseKindScopesReferencesAndBackgroundCollection` |
| `internal/retry` | `Policy.Delay` | `TestDefaultPolicy`, `TestPolicyBounds` |

2026-10-11：补齐构建信息接口映射。

| 包 | 接口 / 方法 | 测试 |
| --- | --- | --- |
| `internal/buildinfo` | `UserAgent` | `internal/agent/model.TestTDDModelCatalogAndGenerationRequireCurrentAuthorization` |
| `internal/buildinfo` | `Current` | `TestInjectedReleaseIdentityWinsOverVCSFallback` |
| `internal/releaseplatform` | `Supported` | `TestSupportMatchesInstallableTargets` |
| `internal/updates` | `Compare`, `Development` | `TestVersionPrecedence` |
| `internal/updates` | `New`, `Service.Check`, `Service.Snapshot` | `TestCheckCoalescesAndRetainsSuccessfulResult`, `TestReleaseChecksStableRegardlessOfConfiguredChannel`, `TestNativePrereleasesCompareAgainstStableWithoutDowngrading`, `TestMissingStableClearsRecommendationAndCanRecover`, `TestContainerChecksStableForEveryCurrentChannel`, `TestContainerMissingStableDoesNotFallBackOrHideRegistryErrors`, `TestContainerRejectsInvalidOrUnavailableImagesAndRetainsLastSuccess` |
| `internal/updates` | `Service.Run` | `TestCanceledLifecycleDoesNotCheck` |
| `internal/updates` | `ReadInstallation` | `TestInstallationReceiptRequiresMatchingExecutableAndCompletedInstall` |
| `internal/config` | `Sandbox.ComputeCeilings` | `internal/app.TestSandboxPlansRequireIndependentComputeStorageAndNetworkGrants` |
| `internal/core` | `CheckGPUDevices`, `ComputeConstraints.GPUDevices`, `ValidateGPUDevices` | `TestGPUDevicesAreExplicitAndBounded` |
| `internal/platform` | `ConfiguredHTTPClient` | `TestConfiguredHTTPClientPinsExplicitLocalSource` |
| `internal/platform/sandbox` | `Runner.GPUs` | `TestLinuxGPUIsolation` |
| `internal/platform/sandbox` | `Runner.GPUSnapshot` | `TestGPUSnapshotPreservesInventoryWhenSamplingCanceled`, `TestGPUSnapshotWithoutDevices`, `TestGPUUsageSnapshotParsesUnknownAndZero`, `TestGPUUsageDoesNotChangeInventoryOrAvailability` |
| `internal/installation` | `Run`, `VerificationPending.Error` | `TestCommandHelpAndVersionWithoutPackage` |
| `internal/maintenance` | `ComponentError.Error`, `Task.Active` | `internal/installation.TestComponentDownloadChecksDigestSizeAndSource`, `internal/installation.TestMaintenanceLockSerializesCLIAndRuntime` |
| `internal/installation` | `NewMaintenance`, `Maintenance.CanUpdate`, `Maintenance.Cancel`, `Maintenance.Enabled`, `Maintenance.Latest`, `Maintenance.Programs`, `Maintenance.ProtectedRoots`, `Maintenance.Snapshot`, `Maintenance.Start`, `Maintenance.Task` | `TestUnmanagedMaintenanceFailsClosed`, `internal/app.TestMaintenanceAPIRequiresAuthenticationCSRFAndKnownRequests` |
| `internal/installrecord` | `Instance`, `Read`, `Record.File`, `Record.UpdateSource`, `Record.Validate` | `TestReceiptRejectsUnrelatedPathsAndWritableRecords` |
| `internal/installrecord` | `Record.Binary`, `Record.ServiceArgs` | `TestServiceArgsKeepRuntimeAndSandboxBoundaries`, `internal/installation.TestSandboxInstallPlanAndSecrets` |

## `internal/browser`

| 包 | 接口 / 方法 | 测试 |
| --- | --- | --- |
| `internal/browser` | `Action.Input` | `TestWheelInputBounds` |
| `internal/browser` | `Action.Args`, `Action.ReadOnly` | `TestActionDoesNotExposeLaunchOrArbitraryExecution` |
| `internal/browser` | `HelperMain` | `internal/app.TestBrowserRealTaskStreamTakeoverAndPersistence` |

| `internal/browser` | `Proxy.Validate`, `Proxy.LaunchConfig` | `TestProxyValidationAndPrivateLaunchConfig` |
| `internal/browser` | `RedactProxyError` | `TestProxyErrorRedactsAllCredentialForms` |
| `internal/browser` | `ProxyErrorReason` | `internal/shellsession.TestBrowserProxyStatusUsesLaunchSnapshotAndUpdatesDiagnostics` |

| `internal/workspace` | `New`, `Service.Query`, `Service.Upload`, `Service.Download`, `Explorer.Query`, `Explorer.Upload`, `Explorer.Download` | `internal/app.TestWorkspaceExplorerLifecycleAndConflict`, `internal/app.TestWorkspaceExplorerConfinementAndLimits`, `internal/app.TestWorkspaceExplorerPaginationAndExternalEdits` |
| `internal/textfiles` | `MakeDirectory` | `internal/app.TestWorkspaceExplorerLifecycleAndConflict` |

| `internal/maintenance` | `Service.CanUpdate`, `Service.Cancel`, `Service.Enabled`, `Service.Latest`, `Service.Programs`, `Service.ProtectedRoots`, `Service.Snapshot`, `Service.Start`, `Service.Task`, `Unavailable.CanUpdate`, `Unavailable.Cancel`, `Unavailable.Enabled`, `Unavailable.Latest`, `Unavailable.Programs`, `Unavailable.ProtectedRoots`, `Unavailable.Snapshot`, `Unavailable.Start`, `Unavailable.Task` | `TestUnavailableMaintenanceDoesNotOfferOrStartHostOperations`, `internal/app.TestMaintenanceAPIRequiresAuthenticationCSRFAndKnownRequests` |
| `internal/textfiles` | `Supported`, `SystemPath` | `internal/app.TestBuildRealMessageAgentAndRestart`, `TestPinnedFileLifecycleAndVersions`, `internal/app.TestResourceFileRejectsLinksAndSpecialFilesBeforeMutation` |

| `internal/textfiles` | `Overlaps` | `TestProtectedPathsRejectAliasesAndDescendants`, `internal/app.TestDirectoryResourcesRejectProtectedCaseAliases` |

## Native 平台与 Windows 安装

| 包 | 接口 / 方法 | 测试 |
| --- | --- | --- |
| `internal/platform` | `NativeNetworkBackend` | `TestNativeSandboxSelectsHostPlatform` |
| `internal/platform` | `Capabilities.NetworkToggle` | `TestControlledReadyRequiresBackendIsolation` |
| `internal/platform` | `PrivateDirectory`, `PrivateFile` | `TestWindowsPrivateFileRejectsExtraAndComplexAllow` |
| `internal/platform` | `ServiceContext`, `ServiceRunning`, `StopService` | `TestWindowsServiceLifecycle` |
| `internal/installationwindows` | `Run` | `TestWindowsUpgradeRejectsUnownedTaskWithoutRollback` |
| `internal/persistence/sqlitestore` | `FileURL` | `TestWindowsFileURL` |
| `internal/instancebackup` | `Add`, `Seal`, `Pack`, `Unpack` | `TestRuntimeArchiveRoundTrip`, `TestArchivePreservesLinksAndRejectsTampering`, `TestSandboxArchiveKeepsStateWithoutUserFiles` |
| `internal/instancebackup` | `Capture`, `CheckDatabase` | `TestRuntimeArchiveRoundTrip`, `TestSandboxArchiveKeepsStateWithoutUserFiles` |
| `internal/instancebackup` | `Configuration` | `TestRuntimeArchiveRoundTrip`, `TestCrossPlatformConfiguration` |
| `internal/instancebackup` | `RelocateLinks` | `TestManagedLinksRelocatedWithoutChangingExternalReferences` |

| `internal/storage/sqlite` | `Database.Store` | `TestTDDSQLiteMessageOutboxAndVisibleEventCommitTogether` |
| `internal/catalog` | `Load`, `Select` | `TestCatalogScopeIndependentOfAccounts` |
| `internal/modelcatalog` | `Entries`, `Service.Describe`, `Service.Resolve`, `Service.ForEstimate`, `Service.List` | `TestModelCatalogResolutionAndRevocation`, `TestAutomaticPriceTiersCurrencyAndManualOverride` |
| `internal/objectaccess` | `Service.Require` | `TestOwnershipUsesDomainRecordsAndAttachmentReferences` |

## macOS 受管浏览器

| 模块 | 导出接口 | 行为测试 |
| --- | --- | --- |
| `internal/browser` | `ChromiumExecutable`, `NewHandler`, `Command`, `NativeCommand` | `TestDaemonPeerIdentityBeforeSendingCommand`, `internal/platform/sandbox.TestSeatbeltNativeBrowser`, `internal/app.TestBrowserRealTaskStreamTakeoverAndPersistence`, `internal/app.TestBrowserProxyRealSandbox` |
| `internal/platform/sandbox` | `HelperMain`, `Runner.Start`, `Runner.DialForward`, `Runner.StopAssistant`, `Runner.Close` | `TestSeatbeltNativeBrowser`, `TestSeatbeltBrowserParentCrash`, `TestSeatbeltBrowserControlPorts`, `internal/app.TestBrowserFullAccessRealTaskStreamTakeoverAndPersistence`, `internal/app.TestBrowserFullAccessRealProxy`, `internal/app.TestFullAccessManualTerminalRequiresGrantAndStopsOnRevocation` |
| `internal/installation` | `Maintenance.Start`, `Maintenance.Task`, `Maintenance.Programs`, `Maintenance.Snapshot` | `TestMacBrowserBundleLinks`, `TestMacBrowserRealArchive`, `TestMacComponentWorkerLockHandoff`, `TestMaintenanceLockSerializesCLIAndRuntime` |

## 内存结果缓存

| 模块 | 导出接口 | 行为测试 |
| --- | --- | --- |
| `internal/resultcache` | `New`, `Cache.GetOrCompute` | `TestCacheFixedTTLRefreshAndFailure`, `TestCacheLRUCostAndOversizedResults`, `TestCacheSharesWorkAndIndependentCancellation`, `TestCacheAbandonedWorkCannotOverwriteReplacement`, `TestCacheConcurrentAccess` |
| `internal/resultcache` | `Cache.Close` | `TestCacheLimitsTimeoutPanicAndClose` |

## 沙盒会话模块

| 包 | 公开入口 | 行为测试 |
| --- | --- | --- |
| `internal/shellsession` | `New`, `Manager.Execute`, `Manager.Close` | `TestShellTasksDetachLimitOwnershipAndEvents`, `TestShellForegroundPromotionAndCancellation` |
| `internal/shellsession` | `ManagedFor`, `ManagedPrincipal` | `internal/app.TestSessionHostPreservesExecutionCapability` |
| `internal/shellsession` | `Manager.Start` | `TestShellRestartInterruptsWithoutReplay` |
| `internal/shellsession` | `Manager.Manage` | `TestTerminalHandoffPermissionsVersionsAndReadBarrier`, `TestShellTaskListBoundsHistoryKeepsRunningAndScopesAssistant` |
| `internal/shellsession` | `Manager.Tool`, `Manager.TerminalFrames` | `TestTerminalEntryPointsPreserveStreamBackpressureAndIdentity` |
| `internal/shellsession` | `Manager.StartTerminal`, `TerminalScript` | `TestTerminalManualStartUsesAssistantCapabilityAndSuppressesReminders`, `TestTerminalManualStartPreservesFullAccessMode`, `internal/app.TestWindowsTerminalDefaultShellRouting` |
| `internal/shellsession` | `Manager.BrowserTool`, `Manager.BrowserManage`, `Manager.BrowserReady` | `TestBrowserEntryPointsKeepOwnershipAndFixedDiagnostics`, `internal/app.TestBrowserRealTaskStreamTakeoverAndPersistence` |
| `internal/shellsession` | `Manager.BrowserSettings`, `Host.ProxyCipher`, `Host.VerifyProxy` | `TestBrowserProxySettingsEncryptedVersionedAndAdministratorOnly`, `internal/app.TestBrowserProxyRealSandbox`, `internal/app.TestBrowserProxySettingsRejectRegularMembers` |
| `internal/shellsession` | `Manager.BrowserFrames`, `Host.DialForward` | `internal/app.TestBrowserRealTaskStreamTakeoverAndPersistence`, `internal/app.TestShellForwardRealLocalAndRemote` |
| `internal/shellsession` | `Host.CheckAssistant`, `Host.CheckExecution` | `internal/app.TestSessionHostPreservesExecutionCapability`, `internal/app.TestShellTasksRealDetachedAuthorizationAndTTY` |
| `internal/shellsession` | `Host.Execute`, `Host.ResolveCompute`, `Host.ConcurrencyLimit` | `internal/app.TestFullAccessRequiresGroupDelegation`, `internal/app.TestFullAccessAndIsolationCoexist` |
| `internal/shellsession` | `Host.AuthorizeForward` | `internal/app.TestShellForwardRealLocalAndRemote` |
| `internal/shellsession` | `Host.BrowserSandboxAvailable`, `Host.BrowserSupported`, `Host.AuthorizeSession` | `internal/app.TestBrowserRealTaskStreamTakeoverAndPersistence`, `internal/app.TestBrowserFullAccessRealTaskStreamTakeoverAndPersistence` |
| `internal/shellsession` | `Host.RecordAudit` | `internal/app.TestFullAccessManualTerminalRequiresGrantAndStopsOnRevocation` |
| `internal/shellsession` | `Host.CaptureLogs`, `Host.Workspace` | `internal/app.TestShellTasksRemoteTTYAndArchivedOutput` |
| `internal/shellsession` | `ReadOutput`, `OutputPrefix` | `internal/app.TestTerminalOutputPreservesBytesAcrossChunks`, `internal/app.TestShellArchiveCanBeReadFromStateDirectory` |
| `internal/shellsession` | `NetConn` | `TestForwardStreamDeadlineAndClose`, `internal/app.TestShellForwardRealLocalAndRemote` |

| `internal/filetools` | `New`, `Tools.Read`, `IsText`, `VirtualPath` | `internal/app.TestFileReadRangesLargeFilesAndExplicitTruncation`, `internal/app.TestFileReadRejectsLongLinesAndReportsEmpty` |
| `internal/filetools` | `Tools.List` | `internal/app.TestFileListPaginationScopeAndVersion` |
| `internal/filetools` | `Tools.Search` | `internal/app.TestFileSearchWorkspaceGlobalAndExternalScope` |
| `internal/filetools` | `Tools.Write`, `Tools.Edit` | `internal/app.TestFileWriteExplicitCreateReplaceAndVersions`, `internal/app.TestFileEditExactMatchConflictAndConcurrentUpdates` |
| `internal/filetools` | `Tools.Move`, `Tools.Delete` | `internal/app.TestFileMoveDeleteVersionsDestinationsAndLinks`, `internal/app.TestCommonFileMoveDeleteReceiptsAndVisibility` |
| `internal/filetools` | `Tools.BoundResult` | `internal/app.TestLargeToolResultIsSavedAndReadableInChunks`, `internal/app.TestToolResultRefusesLinkedOutput` |
| `internal/filetools` | `Tools.Lock` | `TestResourceFileLockRejectsCanceledContext`, `TestResourceFileLockSerializesConcurrentWriters` |
| `internal/filetools` | `Tools.MediaRoot`, `Tools.Snapshot` | `TestCanceledMediaSnapshotDoesNotTouchOrEvictCache` |
| `internal/filetools` | `Tools.PrepareMedia` | `internal/app.TestFileMediaMetadataSnapshotsModelSwitchAndRevocation`, `internal/app.TestMediaWindowKeepsRecentReadsAndSurvivesCacheEviction` |
| `internal/filetools` | `CodecCommand` | `internal/app.TestMediaCodecIsolationAndAudioVideoRead` |
| `internal/filetools` | `IsBuiltin` | `internal/app.TestBuiltinDocsWithoutSandbox`, `internal/app.TestBuiltinDocsRejectMutationsAndForeignPaths` |
| `internal/filetools` | `WithPolicy`, `CheckPolicy`, `ObserveRead`, `PrepareMutation` | `internal/app.TestDreamUsesCommonFileExecutorWithScopedAccess`, `internal/app.TestDreamReadsDiagnosticsRepairsMalformedMemoryAndPreservesUndo` |
| `internal/im/channelstate` | `Seal`, `Open` | `TestEnvelopeBindsIdentityAndRejectsDamage` |
| `internal/im/channelstate` | `Notify`, `SyncNotice` | `TestConnectionNotifyPreservesScopeRecoveryAndCommitOrder` |
| `internal/sandboxruntime` | `New`, `Remote.Execute`, `Client.Execute`, `Client.Await`, `Host.Connect`, `Host.Epoch`, `Host.CheckExecution`, `Host.CheckConnection` | `TestRemoteExecutionReusesFrozenIdentityAndRechecksEpoch`, `internal/app.TestShellTasksRemoteTTYAndArchivedOutput` |
| `internal/sandboxruntime` | `Remote.Remember`, `Remote.Tick`, `Remote.Close`, `Host.Info`, `Host.CheckMembership` | `TestRemoteWaitRecoversOnceAndRespectsRevocation`, `internal/app.TestRemoteFailureDoesNotRestoreStoppedWork` |
