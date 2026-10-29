# 安全与服务入口合同覆盖

此表是公开可调用接口的测试入口清单，不是测试通过率。嵌入接口逐方法展开；所有表中方法均在所列测试或其直接辅助函数中调用。auth/audit/credential/policy 已绑定真实服务和 SQLite 仓储；api/app 也已绑定真实 HTTP server 与 Application；生命周期故障测试仅控制其依赖。缺失绑定明确失败。现有纯函数测试可独立通过。

每次 factory 创建独立的真实 SUT 与干净存储。Repository fixture 负责提供合法事务作用域（需要跨调用观察时可用真实事务适配器），不得提供内存假实现替代被测仓储。时钟、外部供应商、故障与调用观测可以注入。API fixture 提供实际路由的请求构造器，app fixture 记录其真实 Application 对依赖的调用。

| 包 | 公开方法 | 具体测试 |
| --- | --- | --- |
| `internal/oauthbroker` | `New`, `Broker.Start`, `Provider.AuthorizationURL` | `TestExchangeAndReplay`, `TestStartBoundaries` |
| `internal/oauthbroker` | `Broker.Complete`, `Provider.Complete` | `TestExchangeAndReplay`, `TestStateIdentityAndRevocation`, `TestConcurrentCompletion`, `TestCancelledDeniedExpiredAndFailure` |
| `internal/oauthbroker` | `Broker.Status` | `TestStateIdentityAndRevocation` |
| `internal/oauthbroker` | `Broker.Cancel` | `TestCancelledDeniedExpiredAndFailure`, `TestConcurrentCompletion` |
| internal/auth | Service.Login | TestTDDAuthenticationSessionLifecycle |
| internal/auth | Service.Authenticate | TestTDDAuthenticationSessionLifecycle |
| internal/auth | Service.SecurityStatus | TestTDDMFALifecycleAndChallengeReplay, TestSecurityStatusRequiresLiveOwnerSession |
| internal/auth | Manager.SecurityStatus | TestTDDMFALifecycleAndChallengeReplay, TestSecurityStatusRequiresLiveOwnerSession |
| internal/auth | Service.Sessions | TestTDDAuthenticationSessionLifecycle |
| internal/auth | Service.RevokeSession | TestTDDAuthenticationSessionLifecycle |
| internal/auth | Service.Logout | TestTDDAuthenticationSessionLifecycle |
| internal/auth | Service.Reauthenticate | TestTDDVerificationBindingSingleUseAndPasswordChange |
| internal/auth | Service.CheckVerification | TestTDDVerificationBindingSingleUseAndPasswordChange |
| internal/auth | Service.ChangePassword | TestTDDVerificationBindingSingleUseAndPasswordChange |
| internal/auth | Service.BeginMFAEnrollment | TestTDDMFALifecycleAndChallengeReplay |
| internal/auth | Service.ConfirmMFAEnrollment | TestTDDMFALifecycleAndChallengeReplay |
| internal/auth | Service.CompleteMFA | TestTDDMFALifecycleAndChallengeReplay |
| internal/auth | Service.DisableMFA | TestTDDMFALifecycleAndChallengeReplay |
| internal/auth | Service.CreateToken | TestTDDTokenLifecycleAndManagementAuthorization |
| internal/auth | Service.Tokens | TestTDDTokenLifecycleAndManagementAuthorization |
| internal/auth | Service.RevokeToken | TestTDDTokenLifecycleAndManagementAuthorization |
| internal/auth | Bootstrap.Initialized | TestTDDBootstrapSingleUseAndRecovery |
| internal/auth | Bootstrap.Initialize | TestTDDBootstrapSingleUseAndRecovery |
| internal/auth | Bootstrap.Recover | TestTDDBootstrapSingleUseAndRecovery |
| internal/auth | Repository.Account | TestTDDRepositoryAccountAndCredentials |
| internal/auth | Repository.InsertAccount | TestTDDRepositoryAccountAndCredentials |
| internal/auth | Repository.SaveAccount | TestTDDRepositoryAccountAndCredentials |
| internal/auth | Repository.FindSession | TestTDDRepositoryAccountAndCredentials |
| internal/auth | Repository.SaveSession | TestTDDRepositoryAccountAndCredentials |
| internal/auth | Repository.Sessions | TestTDDRepositoryAccountAndCredentials |
| internal/auth | Repository.RevokeSession | TestTDDRepositoryAccountAndCredentials |
| internal/auth | Repository.FindToken | TestTDDRepositoryAccountAndCredentials |
| internal/auth | Repository.SaveToken | TestTDDRepositoryAccountAndCredentials |
| internal/auth | Repository.Tokens | TestTDDRepositoryAccountAndCredentials |
| internal/auth | Repository.RevokeToken | TestTDDRepositoryAccountAndCredentials |
| internal/auth | Repository.RevokeAll | TestTDDRepositoryAccountAndCredentials |
| internal/auth | Repository.PutChallenge | TestTDDRepositorySingleUseChallengeAndRecovery |
| internal/auth | Repository.ConsumeChallenge | TestTDDRepositorySingleUseChallengeAndRecovery |
| internal/auth | Repository.ConsumeRecoveryCode | TestTDDRepositorySingleUseChallengeAndRecovery |
| internal/auth | BrowserGuard.CheckCSRF | TestTDDBrowserGuardSessionAndOriginBinding |
| internal/auth | Authentication.Validate | TestAuthenticationRejectsAmbiguousCredentials |
| internal/audit | Recorder.Record | TestTDDRecordAndAuthorizedRead |
| internal/audit | Service.Record | TestTDDRecordAndAuthorizedRead |
| internal/audit | Reader.List | TestTDDRecordAndAuthorizedRead |
| internal/audit | Service.List | TestTDDRecordAndAuthorizedRead |
| internal/audit | Repository.Append | TestTDDRepositoryAppendPaginationAndRetention |
| internal/audit | Repository.List | TestTDDRepositoryAppendPaginationAndRetention |
| internal/audit | Repository.Prune | TestTDDRepositoryAppendPaginationAndRetention |
| internal/credential | Manager.Put | TestTDDCredentialMetadataLifecycle |
| internal/credential | Manager.Get | TestTDDCredentialMetadataLifecycle |
| internal/credential | Manager.List | TestTDDCredentialMetadataLifecycle |
| internal/credential | Manager.Delete | TestTDDCredentialMetadataLifecycle |
| internal/credential | Access.EffectivePrincipal | TestAccessTrustedPrincipalAndOptionalExecution |
| internal/credential | Access.Validate | TestAccessTrustedPrincipalAndOptionalExecution、TestAccessRequiresExactOriginAndBoundIdentifiers |
| internal/credential | Broker.WithSecret | TestTDDBrokerDestinationAndLiveRevocation |
| internal/credential | Broker.Refresh | TestTDDBrokerRefreshRechecksBinding |
| internal/credential | Repository.Get | TestTDDRepositoryEncryptionCatalogAndCAS |
| internal/credential | Repository.List | TestTDDRepositoryEncryptionCatalogAndCAS |
| internal/credential | Repository.Save | TestTDDRepositoryEncryptionCatalogAndCAS |
| internal/credential | Repository.Delete | TestTDDRepositoryEncryptionCatalogAndCAS |
| internal/policy | Authorize | TestAuthorizePreservesChecksAndRequest / TestAuthorizeRechecksLiveGrant |
| internal/policy | Authorizer.Require | TestTDDGrantsAndLiveAuthorization |
| internal/policy | Service.Require | TestTDDGrantsAndLiveAuthorization |
| internal/policy | Grants.Grant | TestTDDGrantsAndLiveAuthorization |
| internal/policy | Grants.List | TestTDDGrantsAndLiveAuthorization |
| internal/policy | PermissionRequestInput.Normalize | TestPermissionRequestInputBoundaries |
| internal/policy | PermissionRequestItemError | TestPermissionRequestQuotaMetadataValidation |
| internal/policy | Grants.Revoke | TestTDDGrantsAndLiveAuthorization |
| internal/policy | Service.Grant | TestTDDGrantsAndLiveAuthorization |
| internal/policy | Service.List | TestTDDGrantsAndLiveAuthorization |
| internal/policy | Service.Revoke | TestTDDGrantsAndLiveAuthorization |
| internal/policy | UsageReader.Usage | TestTDDBudgetSettlementUnknownAndRelease |
| internal/policy | Budget.Reserve | TestTDDBudgetAtomicReservationAndIdempotency |
| internal/policy | Budget.Settle | TestTDDBudgetSettlementUnknownAndRelease |
| internal/policy | Budget.MarkUnknown | TestTDDBudgetSettlementUnknownAndRelease |
| internal/policy | Budget.Release | TestTDDBudgetSettlementUnknownAndRelease |
| internal/policy | Budget.Usage | TestTDDBudgetSettlementUnknownAndRelease |
| internal/policy | Service.Reserve | TestTDDBudgetAtomicReservationAndIdempotency |
| internal/policy | Service.Settle | TestTDDBudgetSettlementUnknownAndRelease |
| internal/policy | Service.MarkUnknown | TestTDDBudgetSettlementUnknownAndRelease |
| internal/policy | Service.Release | TestTDDBudgetSettlementUnknownAndRelease |
| internal/policy | Service.Usage | TestTDDBudgetSettlementUnknownAndRelease |
| internal/policy | Repository.Grants | TestTDDRepositoryGrantIsolationAndCAS |
| internal/policy | Repository.InsertGrant | TestTDDRepositoryGrantIsolationAndCAS |
| internal/policy | Repository.RevokeGrant | TestTDDRepositoryGrantIsolationAndCAS |
| internal/policy | Repository.Reserve | TestTDDRepositoryBudgetLifecycle |
| internal/policy | Repository.Settle | TestTDDRepositoryBudgetLifecycle |
| internal/policy | Repository.SetReservationState | TestTDDRepositoryBudgetLifecycle |
| internal/policy | Repository.Usage | TestTDDRepositoryBudgetLifecycle |
| internal/api | Server.Handler | TestTDDServerHandlerAuthAndCSRFBeforeServices |
| internal/api | Server.Serve | TestTDDServerServeAndShutdown |
| internal/api | Server.Shutdown | TestTDDServerServeAndShutdown |
| internal/api | RateLimiter.Allow | TestTDDRateLimiterBoundedIndependentAndFailureClosed |
| internal/api | Services.Validate | TestTDDServicesValidationAllFields |
| internal/api | PublicError | TestTDDPublicErrorStatusMapping, TestPublicErrorReasonsAreReviewedAndKeepDiagnosticsPrivate |
| internal/app | Application.Run | TestTDDApplicationStartupAndShutdownOrder |
| internal/app | Application.Shutdown | TestTDDApplicationStartupAndShutdownOrder |
| internal/app | Dependencies.Validate | TestTDDDependenciesValidateEveryRequiredPort |

| internal/auth | Manager.Login | TestTDDAuthenticationSessionLifecycle |
| internal/auth | Manager.Authenticate | TestTDDAuthenticationSessionLifecycle |
| internal/auth | Manager.Sessions | TestTDDAuthenticationSessionLifecycle |
| internal/auth | Manager.RevokeSession | TestTDDAuthenticationSessionLifecycle |
| internal/auth | Manager.Logout | TestTDDAuthenticationSessionLifecycle |
| internal/auth | Manager.Reauthenticate | TestTDDVerificationBindingSingleUseAndPasswordChange |
| internal/auth | Manager.CheckVerification | TestTDDVerificationBindingSingleUseAndPasswordChange |
| internal/auth | Manager.ChangePassword | TestTDDVerificationBindingSingleUseAndPasswordChange |
| internal/auth | Manager.BeginMFAEnrollment | TestTDDMFALifecycleAndChallengeReplay |
| internal/auth | Manager.ConfirmMFAEnrollment | TestTDDMFALifecycleAndChallengeReplay |
| internal/auth | Manager.CompleteMFA | TestTDDMFALifecycleAndChallengeReplay |
| internal/auth | Manager.DisableMFA | TestTDDMFALifecycleAndChallengeReplay |
| internal/auth | Manager.CreateToken | TestTDDTokenLifecycleAndManagementAuthorization |
| internal/auth | Manager.Tokens | TestTDDTokenLifecycleAndManagementAuthorization |
| internal/auth | Manager.RevokeToken | TestTDDTokenLifecycleAndManagementAuthorization |
| internal/auth | Manager.Initialized | TestTDDBootstrapSingleUseAndRecovery |
| internal/auth | Manager.Initialize | TestTDDBootstrapSingleUseAndRecovery |
| internal/auth | Manager.Recover | TestTDDBootstrapSingleUseAndRecovery |
| internal/auth | Manager.CheckCSRF | TestTDDBrowserGuardSessionAndOriginBinding |
| internal/audit | Manager.Record | TestTDDRecordAndAuthorizedRead |
| internal/audit | Manager.List | TestTDDRecordAndAuthorizedRead |
| internal/credential | Vault.Put | TestTDDCredentialMetadataLifecycle |
| internal/credential | Vault.Get | TestTDDCredentialMetadataLifecycle |
| internal/credential | Vault.List | TestTDDCredentialMetadataLifecycle |
| internal/credential | Vault.Delete | TestTDDCredentialMetadataLifecycle |
| internal/credential | Vault.WithSecret | TestTDDBrokerDestinationAndLiveRevocation, TestOAuthResolutionSerializesRefreshAndPersistsBeforeUse |
| internal/credential | Vault.Refresh | TestTDDBrokerRefreshRechecksBinding |
| internal/policy | Manager.Require | TestTDDGrantsAndLiveAuthorization |
| internal/policy | Manager.Grant | TestTDDGrantsAndLiveAuthorization |
| internal/policy | Manager.List | TestTDDGrantsAndLiveAuthorization |
| internal/policy | Manager.Revoke | TestTDDGrantsAndLiveAuthorization |
| internal/policy | Manager.Reserve | TestTDDBudgetAtomicReservationAndIdempotency |
| internal/policy | Manager.Settle | TestTDDBudgetSettlementUnknownAndRelease |
| internal/policy | Manager.MarkUnknown | TestTDDBudgetSettlementUnknownAndRelease |
| internal/policy | Manager.Release | TestTDDBudgetSettlementUnknownAndRelease |
| internal/policy | Manager.Usage | TestTDDBudgetSettlementUnknownAndRelease |
| internal/auth | NewService | TestTDDBootstrapSingleUseAndRecovery |
| internal/auth | NewRepository | TestTDDBootstrapSingleUseAndRecovery |
| internal/audit | NewService | TestTDDRecordAndAuthorizedRead |
| internal/audit | NewRepository | TestTDDRecordAndAuthorizedRead |
| internal/credential | NewService | TestTDDCredentialMetadataLifecycle |
| internal/credential | NewRepository | TestTDDCredentialMetadataLifecycle |
| internal/policy | NewService | TestTDDGrantsAndLiveAuthorization |
| internal/policy | NewRepository | TestTDDGrantsAndLiveAuthorization |
| internal/auth | Manager.ValidatePrincipal | TestLivePrincipalValidationRejectsRevocationAndTokenScope |
| internal/auth | Manager.CheckScope | TestLivePrincipalValidationRejectsRevocationAndTokenScope |
| internal/auth | PrincipalValidator.ValidatePrincipal | TestLivePrincipalValidationRejectsRevocationAndTokenScope |
| internal/auth | Manager.PreLoginCSRF | TestTDDBrowserGuardSessionAndOriginBinding |

| internal/api | NewServer | TestTDDServerHandlerAuthAndCSRFBeforeServices |
| internal/api | NewRateLimiter | TestTDDRateLimiterBoundedIndependentAndFailureClosed |
| internal/api | Limiter.Allow | TestTDDRateLimiterBoundedIndependentAndFailureClosed |
| internal/api | HTTPServer.Handler | TestTDDServerHandlerAuthAndCSRFBeforeServices |
| internal/api | HTTPServer.Serve | TestTDDServerServeAndShutdown |
| internal/api | HTTPServer.Shutdown | TestTDDServerServeAndShutdown |
| internal/app | NewApplication | TestTDDApplicationStartupAndShutdownOrder |
| internal/app | Runtime.Run | TestTDDApplicationStartupAndShutdownOrder |
| internal/app | Runtime.Shutdown | TestTDDApplicationStartupAndShutdownOrder |

补充边界：

- auth：密码恢复撤销所有凭证、并发首次初始化、会话与 token 过期、二次验证用途/会话绑定与单次消费、MFA/恢复码重放、owner-only 管理、CSRF 登录与会话绑定。
- audit：写入错误必须传播，按用户、动作和结果过滤，分页不重复，保留清理按批次限制。
- credential：元数据管理、二次认证、幂等/CAS、加密持久化、可信回调、目标绑定、实时撤销及过期、刷新不绕过授权。
- policy：实时授权、资源/目标/动作限制、失效关闭、并发预算上限、幂等预留、未知费用保持预留、结算不重复、无费用证据后释放、仓储归属与版本。
- api：保护接口在调用业务之前检查认证/CSRF/origin，SSE 会话撤销后关闭且拒绝重连、启动和排空、限流独立计数及存储故障拒绝访问、安全错误映射、组合依赖完整性。
- app：恢复先于 scheduler/HTTP，失败逆序释放资源，拒绝未初始化公网启动，取消与幂等关闭，缺失/typed-nil 依赖校验。

新增集成测试已验证真实 HTTP/TLS/SSE、可信代理头选择、Cookie 属性、秘密 DTO 及登录到令牌创建完整链路。真实浏览器同源行为和部署端代理配置仍需部署验收；DNS/重定向连接约束由 platform 与 app 的网络适配测试覆盖，不能以本组接口调用清单替代这些检查。

| internal/credential | Broker.WithOwnerSecret, Vault.WithOwnerSecret | TestOwnerConnectionSecretRequiresOwnerSessionAndProvider |

| internal/auth | Service.SetReauthentication, Manager.SetReauthentication | TestReauthenticationDefaultsOffAndProtectsPolicyChanges |

| `internal/policy` | `Grants.UpdateGrant`, `Service.UpdateGrant`, `Manager.UpdateGrant` | `TestGrantUpdateIsAtomicVersionedAndOwnerOnly` |

| internal/auth | Service.CreateLoginLink | TestLoginLinkValidationExpiryAndManagement |
| internal/auth | Service.LoginLinks | TestLoginLinkLifecycleAndAtomicRedemption |
| internal/auth | Service.RevokeLoginLink | TestLoginLinkValidationExpiryAndManagement |
| internal/auth | Service.RedeemLoginLink | TestLoginLinkLifecycleAndAtomicRedemption |
| internal/auth | Manager.CreateLoginLink | TestLoginLinkValidationExpiryAndManagement |
| internal/auth | Manager.LoginLinks | TestLoginLinkLifecycleAndAtomicRedemption |
| internal/auth | Manager.RevokeLoginLink | TestLoginLinkValidationExpiryAndManagement |
| internal/auth | Manager.RedeemLoginLink | TestLoginLinkLifecycleAndAtomicRedemption |
| `internal/auth` | `Manager.PreviewLoginLink`, `Service.PreviewLoginLink` | `TestLoginLinkPreviewDoesNotConsumeAndRejectsInvalidLinks` |


### 资源额度（2026-10-02）

| 包 | 公开入口 | 验证 |
| --- | --- | --- |
| `internal/policy` | `Grant.EffectiveConstraints` | `TestQuotaTypesAndExplicitUnlimited`, `TestPermissionRequestInputBoundaries` |
| `internal/policy` | `NetworkPeriod` | `TestCalendarQuotaPeriodsHandleTimezoneAndDST` |

### 多用户合同（2026-10-03）

| 包 | 公开入口 | 验证 |
| --- | --- | --- |
| `internal/auth` | `GroupFor`, `Manager.CreateUser`, `Manager.CurrentUser`, `Manager.RequireAdministrator`, `Manager.Groups`, `Manager.SaveGroup`, `Repository.AccountByID`, `Repository.AccountByUsername` | `TestMultipleUsersHaveIndependentAuthenticationAndSecurity`, `TestMultiUserProfileAndGroupLifecycle` |
| `internal/auth` | `Manager.UpdateUser`, `Manager.Users` | `TestDisableAccountRevokesSessionsAndLinksAndProtectsLastAdmin`, `TestMultipleUsersHaveIndependentAuthenticationAndSecurity` |
| `internal/auth` | `Manager.UpdateProfile`, `Manager.DeleteGroup` | `TestMultiUserProfileAndGroupLifecycle` |

| `internal/auth` | `Manager.WatchPrincipal`, `PrincipalWatch.Check` | `TestPrincipalWatchCommitRollbackAndOwnerScope`, `TestPrincipalWatchExpiryAndActivity`, `TestPrincipalWatchAccountTokenAndRevokeAll` |

| `internal/auth` | `Service.WatchPrincipal` | `TestPrincipalWatchCommitRollbackAndOwnerScope`, `TestPrincipalWatchExpiryAndActivity`, `TestPrincipalWatchAccountTokenAndRevokeAll` |

### 设备会话与认证回收

- `TestDeviceLoginReplacesCredentialsWithoutGrowingHistory`：重复密码／链接登录轮换凭证、敏感验证证明及长连接失效，退出后复用设备记录。
- `TestDeviceSessionsAreAccountScopedAndOtherDevicesRevokeIndependently`：Web／iOS 独立、账号隔离、批量撤销与闲置到期。
- `TestDeviceMFAIsBoundAndStaleChallengesCannotReplaceNewLogin`、`TestDeviceGenerationSerializesConcurrentIssuance`：MFA 设备绑定、较早 challenge 与并发登录的代际检查。
- `TestAuthCollectionBoundsAndPreservesLiveCredentials`：批量上限、失效保留期、有效凭证及待完成 MFA 的保留。
- `TestAuthenticationCookieScopesIncludeNonstandardPorts`：直连 HTTPS／HTTP 非默认端口的认证 Cookie 隔离。

| 包 | 公开入口 | 测试 |
| --- | --- | --- |
| `internal/auth` | `Manager.Collect` | `TestAuthCollectionBoundsAndPreservesLiveCredentials` |
| `internal/auth` | `Manager.RevokeOtherSessions`, `Service.RevokeOtherSessions` | `TestDeviceSessionsAreAccountScopedAndOtherDevicesRevokeIndependently` |
| `internal/auth` | `Manager.RunGC` | `internal/app.TestBuildRealMessageAgentAndRestart`, `internal/app.TestTDDApplicationCanceledContextReleasesResources` |

| 包 | 接口 / 方法 | 测试 |
| --- | --- | --- |
| `internal/auth` | `AccountStateSignal` | `TestAccountStateSignalOnlyCommittedLifecycleChanges` |

| `internal/policy` | `LoadPermissionRequest` | `internal/objectaccess.TestOwnershipUsesDomainRecordsAndAttachmentReferences` |

| internal/chatgpt | New, Client.Exchange, Client.Resolve, Client.Revoke | TestChatGPTIdentityExchangeRefreshAndRevocation, TestChatGPTRejectsUntrustedDiscoveryAndRefreshErrors |
