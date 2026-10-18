# Runtime 技术底座

更新：2026-10-11。本文记录当前技术选择与交付边界；模块职责由 [Runtime 架构](overview.md) 维护，部署步骤由 [产品部署指南](../../admin/README.md) 维护。

## 当前技术选择

| 部分 | 当前实现与入口 |
| --- | --- |
| 后端 | 单一 Go `molr`；版本以 [runtime/.go-version](../../../runtime/.go-version) 为准 |
| 客户端 | React / TypeScript WebUI；依赖及 Node 要求由根目录 [package.json](../../../package.json) 和锁文件维护 |
| 客户端接口 | HTTP JSON + SSE，显式路由；[HTTP API](../reference/http-api.md) 与 [TypeScript SDK](../../../packages/sdk/README.md) |
| 模块通信 | 进程内 Go 接口；IM / Agent / 业务及公共支撑 |
| 存储 | 纯 Go SQLite 驱动、本地文件、版本化领域记录与持久事件 |
| 执行 | 每助理独立状态与串行 Loop，跨助理有界并发，操作回执和检查点恢复 |
| 模型 | 内置协议适配，供应商 SSE 与 JSON 响应，受保护的凭证使用 |
| Linux Shell | Bubblewrap、seccomp、cgroup v2、受控出口与静态 helper |
| 发布 | 核心功能统一构建升级；`CGO_ENABLED=0` 静态运行产物 |

SQLite 已实际使用，不再是待选存储方案。HTTP、命令行入口、真实业务与 WebUI 均已实现，UI Design 继续作为模拟展示。

## 扩展边界

后端不加载第三方业务模块。新增内置工具、连接类型与模型适配器通过更新 Go 程序交付。用户可管理现有模型、资源、授权、预算与计划。

Agent 已能在获准 Linux 沙盒中运行脚本，并从自己的工作区读取项目指令和 Skills；这不同于给 Runtime 动态安装 Go 模块。客户端已有 SDK、内置主题与插件主题机制；管理员可导入本地 ZIP 插件包或从公开 HTTPS 链接添加，并授权启用。详见 [上下文更新](../runtime/agent-context.md) 和 [UI 包设计](../frontend/themes.md)。

当前只设计单实例运行，没有多副本调度、跨 Runtime 自动接管、服务发现或 PostgreSQL 适配。macOS 使用 Seatbelt，Windows 使用 AppContainer；两者提供受控 Shell、文件工具及独立授权的 Full Access，按实际能力报告文件/网络隔离与配额限制，见[沙盒设计](../security/sandbox.md)。

## 存储与恢复原则

同一数据目录由实例锁保证只有一个服务使用。事务保存业务状态、事件和幂等信息；模型与外部工具调用不占用数据库长事务。内存通知仅唤醒执行，持久记录支持重启补查。

额度核算集中在 `internal/quota`，网络账本使用实际执行端的 SQLite 事务；查询按账号、资源、状态和时间利用复合索引，聊天分页使用会话与序号索引，模型回执索引只保存 ID 并直接查键。

SQLite 使用 WAL，本机磁盘由单一 Runtime 管理，不作为多服务器共享数据库。附件与数据库通过暂存、发布和恢复检查协调，不声称一个 SQL 事务能回滚所有文件变化。

记录的按键读取、写入与删除复用数据库实例持有的预编译语句，每个事务单独绑定，嵌套保存点沿用当前事务；关闭实例时释放语句。查询索引在同一事务内创建，避免新库初始化时逐索引提交。`sqlitestore` 的 `BenchmarkStoreOpen` 和 `BenchmarkRecordWrites` 分别衡量新库初始化及每事务一千次记录写入，运行方式为 `go test ./internal/persistence/sqlitestore -run '^$' -bench 'Benchmark(StoreOpen|RecordWrites)$' -benchmem`。

附件对象按文件 ID 串行写入、提交和删除；不同对象可同时传输，慢上传不占用其他对象的发布锁。等待同一对象的操作响应上下文取消，最后一个使用者退出后释放锁记录。发布阶段仍串行核对存储总额度、幂等回执和元数据，并在提交前验证对象大小与摘要；并发上传不能绕过额度或制造重复文件。对象恢复仅在启动／离线阶段运行，完成前不接收上传。

恢复时先补结算已有回执，再结束上一进程遗留的本地调用占用；未知外部结果保留记录，不自动重试。数据库快照不是完整灾备；文件、工作区、配置和主密钥需单独保留。操作步骤见 [备份恢复与升级](../../admin/deployment/maintenance.md#备份恢复与升级)。

## 构建与验证

`runtime/Makefile` 使用 `.go-version` 固定测试工具链。运行产物需显式静态构建并检查最终链接方式与权限；部署主服务还需确认构建提交和干净状态，不能仅凭 `go build` 成功判断可部署。

`make -C runtime check` 包含完整 Go 行为测试与静态检查；`check-contracts` 仅检查编译、格式、接口库存和静态规则。旧的“预期红灯”描述已不适用，原 TDD 工厂已绑定真实实现。测试入口与验证边界见 [测试说明](../../../runtime/tests/README.md)。

Shell 的真实内核隔离另由 [沙盒测试](../security/sandbox.md#7-验证) 验证。每个实际服务启动后还需通过认证检查 `/api/sandbox`，单元测试不替代部署能力探针。
