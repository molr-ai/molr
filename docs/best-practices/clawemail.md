# 操作 ClawEmail

[网易 ClawEmail](https://claw.163.com/projects/doc/) 为助理提供独立邮箱，支持查询邮件、读取正文、下载附件和发送邮件。在 Molr 中推荐使用官方 CLI（`mail-cli`），由助理通过 Shell 按你的委托操作邮箱。

## 安装与授权

先完成[执行环境准备](README.md#准备执行环境)，在 [ClawEmail 官网](https://claw.163.com/)创建 Agent 邮箱，再参照官方文档的「Mail-CLI 操作指南」接入。可以这样委托：

> [!PROMPT]
> 按 https://claw.163.com/projects/doc/ 的官方 Mail-CLI 说明，将 mail-cli 和依赖安装到你的工作区，使用 CLI 方式接入 ClawEmail。检查 Node.js 版本和可用命令，引导我完成 API Key 配置与邮箱登录，最后列出少量邮件验证接入。

官方 CLI 要求 Node.js 18 或以上版本。安装来源与步骤以官网当前提供的说明为准，程序和配置保留在 `/work` 内；可通过 `--config <path>` 指定配置文件，通过 `--profile <name>` 选择邮箱。

Claw 邮箱操作需要 API Key，官方使用 `auth apikey set` 配置，再通过 `auth login` 登录。官方文档说明 Key 保存在系统钥匙串中；沙盒没有可用钥匙串时，先核实当前 CLI 是否提供适用的凭据方式，再继续配置。

## 可以这样委托

> [!PROMPT]
> 查看我的 ClawEmail 收件箱最近十封未读邮件，按主题整理摘要和待办，保留发件人、时间和邮件 ID。

<!-- -->

> [!PROMPT]
> 在指定的 ClawEmail 邮箱中搜索本周的项目周报，将相关附件下载到工作区，汇总进展与阻塞项，并注明来源邮件。

<!-- -->

> [!PROMPT]
> 根据我指定的邮件起草回复，先给我确认收件人、主题、正文和附件，再通过 ClawEmail 发送。

委托时说明目标邮箱、时间范围和数量。需要定期汇总时，先验证一次查询，再通过[计划与跟进](../guides/plans.md)安排执行。

## 验证与排查

安装和配置完成后，先检查认证与文件夹：

```bash
mail-cli auth test
mail-cli folder list --json
```

确认收件箱的实际 ID 后再查询；下面以 `1` 为例：

```bash
mail-cli mail list --fid 1 --unread --limit 5 --json
```

认证通过后，实际查询成功才验证了邮件访问能力。多邮箱场景检查当前 profile；认证失败时检查 API Key、登录状态及凭据存储是否可用。接收外部邮件受阻时，到 ClawEmail 控制台检查该邮箱的通讯规则与信任联系人配置。

命令参数先查 `mail-cli --help` 和对应子命令的帮助，完整能力与配置说明见[官方文档](https://claw.163.com/projects/doc/)。
