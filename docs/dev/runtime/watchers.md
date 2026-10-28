# 持续关注与 watcher 协议

持续关注是 `assistant_watch` 计划。它进入全局与助理计划清单的「持续关注」分组，不生成日历发生点。助理负责准备、验证符合本文协议的程序；Runtime 不适配具体网站或第三方 CLI。使用方式见[计划与执行](../../user/guides/plans.md)。

## 创建与管理

使用 `plan_manage`，`action=create`、`mode=assistant_watch`，提供 `title`、`instructions` 和 `watcher`。`watcher` 包含 `compute_id`、`script`，以及可选的 `sandbox_id`、`directory`、`files`、`network_mode`、`network_ids`、`health_seconds`。文件和网络引用沿用 Shell 的资源规则；不接受 TTY、端口转发、Full Access 或 Shell 超时参数。当前 watcher 标准输入管道支持 Linux 和 macOS；Windows 执行端返回明确的能力错误。

只有用户指定期限时才填写 `start_at`／`end_at`。计划不设置期限不等于执行权限永久有效。创建登记后由 Runtime 异步启动；检查到 `running` 才能报告已就绪。`plan_read` 返回 watcher 配置、检查点、最近事件和受阻原因。`update` 支持修改配置及 `paused`，`execute` 请求恢复；有历史时保留计划并暂停，不删除历史。持续关注不接受重复规则、日历发生点或前置交接配置。

普通命令的超时、后台提醒和 TTY 行为不因 watcher 改变。Watcher 使用受管后台执行和现有并发额度；不发送普通 Shell 的周期运行提醒或完成事件。通过计划暂停／恢复管理 watcher，Shell 入口仅可查看日志。

修改 watcher 配置会创建新的计划修订、终止旧执行并清空旧检查点。仅修改说明或期限保留检查点，但仍重新启动监听。脚本引用的文件使用执行时的当前内容；需要固定版本时将脚本及依赖保存到版本化路径。代码修改由助理完成，不能把配置快照视为依赖环境快照。

## 标准输入与输出

每条消息是 UTF-8 JSON，独占一行并以换行结束。stdout 仅输出协议，日志写 stderr。启动时读取 stdin：

```json
{"type":"start","version":1,"state":null,"resumed":false}
```

`state` 是最近确认的检查点；首次为空。`resumed` 提示 Runtime 曾中断观察，程序需要核实数据源是否可以补取缺口，不能假设连续在线。

stdout 消息如下：

| 类型 | 字段 | 含义 |
| --- | --- | --- |
| `ready` | `version: 1` | 初始化完成；启动后 30 秒内必须发送 |
| `checkpoint` | `state` | 成功完成有效检查，或确认订阅有效；更新观察状态而不调用模型 |
| `event` | `key`、`data`，可选 `state` | 需要交给助理处理的业务变化 |
| `complete` | 可选 `state` | 用户关注目标完成，不再观察；已接收事件继续处理 |

示例：

```jsonl
{"type":"ready","version":1}
{"type":"checkpoint","state":{"cursor":123}}
{"type":"event","key":"issue-124-opened","data":{"number":124,"title":"Example issue"},"state":{"cursor":124}}
```

每条消息保存成功后，stdin 返回确认：

```json
{"type":"ack","for":"event","key":"issue-124-opened"}
```

程序应顺序发送并读取确认，不以打印成功代替持久化成功。`key` 在同一计划内标识一个不可变业务事件，重发相同 key 和 data 不重复入队，也不会用旧检查点覆盖新状态；同 key 不同 data 是协议错误。事件与其检查点在同一事务提交。无变化时提交检查点，不能把“无变化”当作错误。

未知消息类型、额外字段、版本不符、无效 JSON、缺失字段、非 JSON 日志和超长消息会停止执行并报告协议错误。`complete` 后不再输出协议消息。未发送 `complete` 就退出属于异常退出。

限制：单行 64 KiB，`state` 4 KiB，`data` 8 KiB，key 256 字节；每秒最多 32 条消息。待处理队列最多 128 条、合计 256 KiB 事件数据；达到上限明确失败并保留已确认状态，不静默丢弃事件。详情保留最近 100 条事件，去重记录独立持久化。脚本日志仍受计算环境的输出和存储限制。

## 模型处理与健康

无变化的检查不创建模型事件。Runtime 每批最多选择 8 条变化，冻结为带计划说明快照的一次助理执行。同一计划已有未结束的处理任务时，新变化继续排队；模型费用和权限沿用助理设置。处理失败不重跑 watcher，也不修改已经冻结的批次。

`health_seconds` 为可选的健康期限，零或省略表示只检查进程状态。配置后，程序必须通过有效协议消息证明进展；无业务变化也应在成功检查后提交 `checkpoint`。不要让独立心跳线程无条件报告健康。健康过期或启动未就绪会中断监听。

## 授权、失败与恢复

启动和运行期间沿用助理的计算、文件和网络授权。授权到期或撤销会停止 watcher，计划保留并显示受阻；Runtime 发出一次失败通知及助理事件，助理可解释情况或申请所需授权。授权记录变化后重新检查；用户已暂停或计划已到期时不启动。达到计算授权的单次执行时限同样进入受阻，不能通过自动重启绕过额度。

临时故障有限退避，连续三次失败后受阻；协议错误、队列满和不支持的执行平台不自动重复启动。用户修复后通过计划恢复。Runtime 重启时保留检查点，等待旧远程执行租约失效后再恢复，避免双实例。恢复程序必须能够从已确认状态继续，只进行观察；有副作用的业务操作交给事件处理阶段。

测试包括 `internal/business/watch` 的协议、队列边界、回滚和修订隔离，`internal/business/plan` 的清单／日历分离与批次快照，以及 `internal/app` 的真实沙盒通信和授权到期：

```bash
CGO_ENABLED=0 go test ./internal/business/watch ./internal/business/plan
bash scripts/test-sandbox-linux.sh -run TestWatcherReal -count=1 -v
```
