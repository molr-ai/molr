# 安装与服务管理

## 环境要求

以下为运行 Molr 服务（Runtime 与 WebUI）、连接云端或其他设备上的模型时的初步容量建议，尚未经系统性性能基准验证。最低配置用于启动服务和单人少量对话；推荐配置为日常使用和少量并发任务预留余量。

CPU 架构须为 64 位 x86（amd64）或 ARM（arm64）。

| 硬件 | 最低配置 | 推荐配置 |
| --- | --- | --- |
| CPU | 1 核 | 4 核及以上 |
| 内存 | 512 MiB | 8 GiB 及以上 |
| 可用磁盘空间 | 5 GiB | 20 GiB 及以上，优先使用 SSD |
| GPU | 无需独立显卡 | 根据本地模型部署需求选择 |

在同一设备上运行本地模型时，需要额外预留模型权重、推理内存及可能需要的显存，具体取决于模型大小、量化方式和上下文长度。运行沙盒任务需要额外的 CPU 与内存，尤其是浏览器自动化、代码构建或多个沙盒并发执行；附件、工作区和备份会持续占用磁盘空间，应按实际任务规模扩容。沙盒资源配置见 [Linux 命令环境](../sandbox/linux.md)。

## 本地发行包与快速安装

Linux 发行包提供 AMD64、ARM64 两种架构，包含主程序 `molr` 和安装维护程序 `molr-setup`。下载匹配架构的归档和 SHA-256 文件，校验后解压，在包目录运行：

```bash
sha256sum -c molr_0.1.0_linux_amd64.tar.gz.sha256
tar -xzf molr_0.1.0_linux_amd64.tar.gz
cd molr
./bin/molr-setup
```

默认安装或升级到当前包的版本，无需指定渠道或安装子命令。已有同版本实例时显示维护入口；需要重新应用文件时指定 `--repair`。制作安装包见[源码构建](source-build.md)。

首次在线安装也可以使用官网下载器：

```bash
curl -fsSL https://molr.ai/install.sh | sh
curl -fsSL https://molr.ai/install.sh | sh -s -- --channel preview
curl -fsSL https://molr.ai/install.sh | sh -s -- --channel nightly
# Pin a version and keep its update subscription
curl -fsSL https://molr.ai/install.sh | sh -s -- --channel preview --version 0.2.0-rc.1
```

官网安装区可分别选择发布渠道、操作系统、处理器架构及 curl／wget，生成安装命令。`--arch amd64` 或 `--arch arm64` 用于校验所选架构；与运行设备不符时，在下载和安装前停止。省略时自动检测。下载安装包方式展示对应文件地址、校验文件和可复制的安装命令，并将所选渠道传给 `molr-setup` 作为更新订阅。

stable 为正式版，preview 为 beta／rc 和后续正式版，nightly 为每日构建。未发布版本的渠道不会自动回退到其他渠道。下载器显示版本、架构、包名、来源、摘要及下载进度，校验后调用包内安装器。它使用已有的 curl 或 wget、tar、gzip 和 SHA-256 工具，不自动安装下载工具；安装维护程序本身无需 Python、curl 或 iproute2。

下载器默认源为 `https://molr.ai/releases`，`--source URL` 可指定其他 HTTPS 发布源。本地归档可由同一下载器校验并安装：

```bash
sh install.sh --archive /path/package.tar.gz
```

相邻的 `package.tar.gz.sha256` 必须存在。直接运行已解压的安装包不需要网络；系统依赖应提前准备。SHA-256 用于核对下载内容，不是独立的发布者签名。

官网发布源只提供近期筛选出的正式版、预发布及 nightly 版本清单；`--version` 必须是当前目录中包含的版本。安装其他历史版本时，从 GitHub Releases 下载对应归档和 SHA-256 文件，按上文离线安装。

## 交互式与静默安装

有控制终端时默认交互安装，先展示安装位置、账户、依赖及权限问题，再确认执行。也可显式选择模式：

```bash
./bin/molr-setup --interactive
./bin/molr-setup --non-interactive --install-deps \
  --directory-permissions fix --no-browser
./bin/molr-setup --dry-run
```

静默模式不提问、不打开浏览器；`--install-deps` 授权系统依赖准备，所需管理员权限应预先配置。已手动准备依赖时使用 `--skip-dependencies`。`--dry-run` 展示计划，不修改账户、权限、配置或服务；通过下载器预览仍会下载临时包。

目录可被组或其他用户写入时，可选择修复、忽略或退出。修复只移除该目录的组／其他用户写权限，不递归修改内容；静默模式以 `--directory-permissions fix|ignore` 明确选择。`--yes` 跳过一般确认，不代替权限选择。所有者不符、符号链接和受管理文件可被其他用户修改时会报错。

省略 `--config` 时自动生成局域网 HTTP 配置、初始化所有者、启动服务并验证已认证 API 与沙盒。输出按检查、准备、安装、验证、结果排列。结果为 `verified` 才表示认证检查通过；`verification_pending` 表示已安装但需要使用当前密码完成检查，退出码为 3；`not_started` 对应 `--no-start`。失败输出包含当前阶段和可能发生的变更。

## Linux 服务与命令环境

当前服务适配使用 systemd。启用 Linux 沙盒需要 systemd 254+、cgroup v2 委派、Bubblewrap、静态 BusyBox、CA 证书及 util-linux 的 nsenter／setpriv。它们是运行能力要求，不按发行版名称或版本拒绝安装。当前自动依赖准备支持 APT；其他环境可按[沙盒要求](../sandbox/linux.md)手动准备。systemd 用户管理器需要正常的 SSH／桌面登录会话。

启用 AppArmor 非特权用户命名空间限制时，安装器以服务账户及 `NoNewPrivileges` 检查 Bubblewrap，先尝试已加载的 `chrome` profile，再尝试原有受限路径；已有配置可用时无需安装新 profile。两条路径均失败且获得授权后可安装并加载发行版的 `bwrap-userns-restrict` 策略，再次验证。已有策略不覆盖，冲突需管理员处理；不会关闭系统隔离限制。隔离依赖缺失不会自动切换为 Full Access。

### 安装时选择运行账户

默认安装当前普通用户的 `molr.service`。root 安装必须显式指定非 root 的 `--user USER`，创建系统服务；不存在的账户会创建为无登录权限的系统账户。已有账户的 HOME、shell、附加组保持原样，不递归改变已有数据目录的所有者。

```bash
./bin/molr-setup --name molr-demo --port 8788 --public-host "<LAN_IP>"
sudo ./bin/molr-setup --user molr-sandbox --install-deps
# Custom configuration; initialize using a password from stdin
./bin/molr-setup --config /path/server.json --init-owner owner \
  < /private/owner-password
```

`<LAN_IP>` 替换为实际局域网 IPv4 地址。自定义配置首次安装需指定 `--init-owner` 或使用 `--no-start` 稍后初始化。服务和沙盒始终以同一普通账户运行。

| 内容 | 当前用户安装 | 系统安装 |
| --- | --- | --- |
| 程序、安装记录与许可证 | `~/.local/lib/molr/<实例>/` | `/opt/molr/<实例>/` |
| 配置 | `${XDG_CONFIG_HOME:-$HOME/.config}/molr/<实例>/` | `/etc/molr/<实例>/` |
| 默认持久数据 | `${XDG_DATA_HOME:-$HOME/.local/share}/molr/<实例>/` | `/var/lib/molr/<实例>/` |
| 缓存与临时文件 | `${XDG_CACHE_HOME:-$HOME/.cache}/molr/<实例>/` | `/var/cache/molr/<实例>/` |
| 服务文件 | `${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user/<服务名>.service` | `/etc/systemd/system/<服务名>.service` |
| 管理命令 | `systemctl --user` | `sudo systemctl` |

服务 `molr` 对应实例 `default`，`molr-demo` 对应 `demo`；`molr-default` 为保留名称。`--program-root /path/programs` 调整程序根目录，实际安装到其实例子目录。两个命令位于程序目录的 `bin/` 下；配置为 `server.json`。用户配置权限为 `0600`，系统配置为 root 所有、服务组可读的 `0640`。

程序、配置、数据和缓存目录不能重叠。自定义 `data_dir` 和外置工作区沿用配置，需允许服务账户访问。数据库、密钥、附件和助理工作区属于持久数据；缓存可以重建。许可证随程序保存，WebUI 也提供 `/LICENSE` 和 `/THIRD_PARTY_NOTICES.md`。

用户服务要在退出登录后运行，由管理员执行 `loginctl enable-linger USER`；系统服务无需 linger。现有精简 Docker 镜像不包含完整的自动命令环境。

## 更新与重复运行

运行已安装实例的安装维护程序，会显示状态并进入维护菜单；无终端时只显示状态。它通过自身位置识别实例，因此无需切换到特定工作目录。

```bash
~/.local/lib/molr/default/bin/molr-setup
~/.local/lib/molr/default/bin/molr-setup status
~/.local/lib/molr/default/bin/molr-setup update
~/.local/lib/molr/default/bin/molr-setup update --version 0.2.0-rc.1
~/.local/lib/molr/default/bin/molr-setup update --channel preview
```

更新使用安装记录中保存的发布源和渠道，显示目标包并下载校验。`--source`／`--channel` 指定此次更新及成功后继续使用的订阅。系统实例使用 `sudo /opt/molr/default/bin/molr-setup update`。Go 更新器也允许显式的私网或回环数字 HTTP 镜像地址。

重新运行官网下载命令、运行另一份安装包均可升级已有实例；保留配置、数据、凭证和账户。程序原子替换，保留 `molr.previous`、`molr-setup.previous` 与 `server.previous.json`。同版本默认不重装，`--repair` 重新应用当前包。降级必须显式使用 `--allow-downgrade`，并先自行备份数据。

安装未完成或记录损坏时会明确报错。不要删除安装记录来绕过检测；使用完整安装包对原实例修复。`--record /path/installation.json` 可明确指定管理的实例。

浏览器等组件的安装、更新与修复见[组件管理](../administration/components.md)。

## 启动后的检查

安装、更新、重启后核对实际页面与已认证沙盒能力：

```bash
~/.local/lib/molr/default/bin/molr doctor -config /path/server.json
~/.local/lib/molr/default/bin/molr access -config /path/server.json
```

默认使用数据目录的初始密码；已改密时增加 `-password-stdin`，启用 MFA 时增加 `-mfa-code`。命令通过正常认证 API 工作。`access` 生成 15 分钟一次性登录链接。

未关闭沙盒时，要求 `/api/sandbox` 返回 `Enabled=true`、`Capabilities.Reason=available`，以及 `Shell`、`ProcessIsolation`、`FileIsolation`、`NetworkIsolation`、`ResourceLimits` 全部为 `true`。端口监听和服务 active 状态不能代替此检查。排查见[沙盒检查](../sandbox/linux.md#检查与验证)。

## 卸载

离线卸载使用已安装的维护程序：

```bash
~/.local/lib/molr/default/bin/molr-setup uninstall --dry-run
~/.local/lib/molr/default/bin/molr-setup uninstall
~/.local/lib/molr/default/bin/molr-setup uninstall --purge --yes
sudo /opt/molr/demo/bin/molr-setup uninstall --purge
```

默认停止对应服务，移除程序、服务文件和实例缓存，保留配置、持久数据及安装记录。重新安装同一实例可复用数据。`--purge` 删除受管理配置与默认数据；交互模式需输入实例名确认，静默模式必须带 `--yes`。

自定义数据、外置工作区、未知文件、系统账户、系统依赖和 linger 设置保留。无法确认服务归属、服务停止失败或删除目录包含挂载点时中止。默认卸载后维护程序已移除；需要继续清除数据时，使用完整包的维护程序指定保留的安装记录：

```bash
./bin/molr-setup uninstall --record /path/installation.json --purge --yes
```
