# Runtime 的 TDD 测试

原公开接口的 TDD 工厂已经全部绑定真实实现。默认 `go test ./...` 和 `make check` 包含原行为合同及新增生产组合测试；不通过跳过、空断言或模拟被测服务来得到绿灯。SQLite、文件、队列、HTTP 和 Agent 均使用实际实现，仅模型供应商、时钟和故障源等外部依赖可受控。

## 运行方式

工具链由 `runtime/.go-version` 固定。仓库根目录可执行：

```bash
# 提交前的仓库检查；契约清单也可单独用 npm run check:contracts 检查
npm run check

# 全部测试
make -C runtime test

# 按模块推进，可在不同终端并行运行
make -C runtime test-tdd PACKAGES='./internal/im/...'
make -C runtime test-tdd PACKAGES='./internal/agent/...'
make -C runtime test-tdd PACKAGES='./internal/business/...'
make -C runtime test-tdd PACKAGES='./internal/auth ./internal/policy ./internal/credential'
make -C runtime test-tdd PACKAGES='./internal/storage/... ./internal/platform'

# 定位某个行为，用完整名称避免跨包同名测试误选
make -C runtime test PACKAGES='./internal/business/assistant' RUN='^TestTDDHireIdentityAtomicityAndIdempotency$'

# 不依赖服务实现的编译、格式、方法映射和静态检查
make -C runtime check-contracts

# 原有测试基线（明确排除 TestTDD，不代表完整测试已通过）
make -C runtime test-existing
```

`PACKAGES` 控制 Go 包选择；`TEST_FLAGS` 可传 `-count=1`、`-race` 等选项。Go 可并行检查不同包；用例中涉及共享时钟、故障注入或进程环境时，不强制 `t.Parallel()`。

## 测试结构与实现接入

每个模块的行为测试位于其 `contract_test.go`（部分模块另有 validator 测试），顶层命名为 `TestTDD...`。每个模块声明 `newContractFixture` 工厂；共享 `tests/testkit.RequireFactory` 在工厂未绑定时报告 `TDD implementation missing`，不会 Skip 或制造 nil 指针异常。

模块合同通过工厂接入实际实现；新增集成测试也直接构造生产 app.Build。工厂需要：

1. 创建该测试独立的真实被测服务、仓储或执行器，使用临时目录与数据库；用 `t.Cleanup` 释放资源。
2. 准备有效账号、可信主体、资源与必要种子。跨主体测试身份用于验证隔离，不要求开放多用户注册。
3. 提供测试需要的可控时钟、真实依赖故障注入和持久状态观测。可以替换外部供应商等依赖，不能用 fake/mock 代替被测 Service、Repository、Server 或 Application。
4. 保留测试已有的接口调用、结果和错误断言。生产实现缺陷通过修改生产代码解决，不能将 `RequireFactory` 改为 Skip，或让工厂直接返回预设被测结果。

行为测试在实现接入后验证身份隔离、幂等、版本、状态变迁、数据持久化、失败恢复与副作用。工厂字段的约定在各测试文件中说明，具体数据库驱动、HTTP 路由和系统实现可由适配器接入。

已实现的校验、配置加载与安全错误转换直接测试真实函数，因此这些 TDD 用例直接执行。编译检查必须通过；不得用语法错误、接口签名错误或无意义 panic 代替行为验证。

## 方法覆盖清单

- [IM、Agent 与事件](coverage-im-agent.md)
- [业务与搜索](coverage-business.md)
- [认证、安全与接入](coverage-security.md)
- [基础设施与公共类型](coverage-infrastructure.md)

`tests/contractcoverage` 使用 Go AST 枚举公开接口方法（包括嵌入展开）及公开函数／具体方法，检查每个入口有测试映射、引用的测试真实存在。这个检查证明 **测试规划未遗漏公开入口**，不证明行为断言已经执行、分支已覆盖或实现正确；多个方法共享一个完整流程测试时，清单会引用同一个用例。

新增、删除或改名公开入口时，同步更新对应表格并运行 `npm run check:contracts`。该命令不需要安装 npm 依赖或启动 Runtime，使用固定 Go 工具链和当前 worktree 的缓存，并通过 `-count=1` 每次重新扫描源码与清单。`make test`、`test-tdd`、`test-existing` 和 `test-compile` 会先运行此检查，即使只选择部分包或用例也不会漏掉其他模块的新增入口。CI 在安装 npm 依赖和完整构建前执行同一检查；提交前与 rebase 后执行 `npm run check`。直接调用 `go test` 的局部测试不会自动包含全仓库清单检查。

## 集成验证与边界

测试使用真实临时 SQLite、文件目录和 HTTP/TLS/SSE socket；没有访问真实模型供应商。`app.TestBuildRealMessageAgentAndRestart` 验证实际生产装配的账号登录、资源授权、用户消息唤醒、显式读历史、发布回复、费用回执与重启持久化。`tests/businessintegration` 验证真实认证/权限与各业务模块接线。

补充测试覆盖事务与并发、嵌套 savepoint、故障回滚、离线恢复撤销认证、被取消执行的历史费用结算、RRULE 与 DST、符号链接/元数据地址/连接复用撤权、聊天清理后的序号高水位，以及命令行初始化与维护。

需要允许测试创建本机 socket。容器或受限沙箱若禁止监听，应使用允许本机测试网络的环境；不应把这些用例改为 Skip。生产权限边界测试不能证明所有浏览器、供应商和操作系统的行为，也不等于独立安全审计。

完整检查使用 `make -C runtime check`；并发检查可另行运行 `go test -race -p 4 ./...`。实际结果须注明日期、提交、环境和跳过项；本地实例记录保存在不提交的内部文档目录。

## 调度唤醒与历史规模

`internal/agent/scheduler/wakeup_test.go` 覆盖空闲无队列轮询、回滚与提交失败不唤醒、重试到期唤醒、完成释放后续输入、做梦截止释放，以及越过多页受阻任务且不扫描历史。`sqlitestore.TestDispatchCandidatesUseOrderedStateIndex` 检查 SQLite 查询计划使用状态／顺序索引且不额外排序。

在 Runtime 目录运行 `go test ./internal/agent/scheduler -run '^$' -bench BenchmarkIdleClaimHistory -benchmem`，比较 1 千与 10 万条历史任务下的空领取开销。基准只衡量领取路径，不代表模型响应延迟或整个服务的 CPU；记录结果时注明日期和环境。
