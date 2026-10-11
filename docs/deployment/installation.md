# 安装与服务管理

## 本地发行包与快速安装

发行包通过 GitHub Releases 分发，官网展示各渠道实际可用的平台、版本、安装命令与下载链接。正式版使用 stable，beta／rc 使用 preview，每日构建使用 nightly；尚未发布版本的渠道不会提供安装按钮。在线安装和离线安装使用所选 Release 的安装脚本；自行制作安装包见[源码构建](source-build.md)。

从安装包安装：

```bash
bash install.sh --archive /path/molr_0.1.0_linux_amd64.tar.gz
# 独立实例示例
bash install.sh --archive /path/package.tar.gz --name molr-demo --port 8788 --public-host "<LAN_IP>"
```

以上命令中的 `<LAN_IP>` 需替换为安装或下载服务器的实际局域网 IP，不能原样执行。

在线安装前准备 Python 3.11+。从官网安装区复制命令，或下载 `/install.sh` 后运行：

```bash
bash install.sh --channel stable
bash install.sh --channel preview
bash install.sh --channel nightly
# 明确安装固定版本，同时保存后续跟踪的渠道
bash install.sh --channel preview --version 0.2.0-rc.1
```

脚本默认使用 `https://molr.ai/releases`，`--source URL` 可指定其他发布源。默认渠道为 stable；不会因没有正式版而自动改装 preview 或 nightly。固定版本从历史清单读取，缺少目标系统／架构的包会停止安装。校验失败时不会执行包内安装器。HTTP 下载源只接受明确的私网或回环数字地址；SHA-256 校验不构成独立的发布者签名。

离线安装时，将版本包、同名 `.sha256` 文件和该 Release 的 `install.sh` 下载到目标机，运行 `bash install.sh --archive <包路径>`。每个 Release 保留安装脚本快照；实际安装逻辑随包发布。官网列出对应版本的 systemd、Python、cgroup 及系统依赖要求。新增或停止平台支持不改变历史发行包。

省略配置文件的安装器会自动生成局域网 HTTP 配置、初始化所有者与 setup、安装服务并检查已认证 API。`--install-deps` 提供 apt 依赖安装，`--yes` 明确同意依赖安装，`--skip-dependencies` 跳过。缺少隔离依赖不自动开放 Full Access，沙盒能力按真实探针报告。安装与卸载需要 Python 3.11+，systemd 和基本安装工具仍需可用。安装器检查 BusyBox 必须是可执行、不可被组或其他用户写入的静态程序；只有动态 BusyBox 时仍会提示安装 `busybox-static`。不支持 apt 或离线安装时，请预先通过目标系统的包管理器安装静态 BusyBox 等依赖。安装器不复制 BusyBox 到 Molr 安装目录。

`LICENSE` 和 `THIRD_PARTY_NOTICES.md` 保存在安装前缀根目录，例如默认实例的用户安装目录 `~/.local/lib/molr/default/` 或系统安装目录 `/opt/molr/default/`。内嵌 WebUI、独立 WebUI 和官网均提供 `/LICENSE` 与 `/THIRD_PARTY_NOTICES.md`；分发构建目录时应保留这两个文件。许可证正文集中维护在仓库根目录，不使用第三方 submodule。

用户服务未启用 linger 时，退出登录可能停止服务；要让系统在未登录时运行，由管理员执行 `loginctl enable-linger USER`。指定账户的系统服务不依赖 linger。

安装完成后可用以下命令检查或重新生成访问入口：

```bash
/path/molr-server doctor -config /path/server.json
/path/molr-server access -config /path/server.json
```

默认使用数据目录内的随机初始密码。已改密时增加 `-password-stdin`，启用 MFA 时增加 `-mfa-code`；这些命令通过正常认证 API 工作，不打开运行中的数据库，不绕过 MFA。链接有效期 15 分钟，实际交付链接不用于健康检查。

## Linux 服务与命令环境

Linux Shell 需要 Bubblewrap、静态 BusyBox、可用的 user namespace 和 cgroup v2 委派。安装路径与当前限制见 [Linux 命令环境](sandbox.md)。使用独立的 systemd 用户服务；启用沙盒的服务至少采用：

```ini
[Service]
Type=exec
ExecStart=/path/molr-server serve -config /private/server.json
Delegate=yes
DelegateSubgroup=supervisor
KillMode=control-group
Restart=on-failure
```

使用真实绝对路径，多个实例分别命名、配置和分配端口及数据目录。Bubblewrap/helper 必须是可执行普通文件，路径及父目录不能通过符号链接，文件不能组可写或其他用户可写。不要为了启动成功放宽隔离条件。

### 安装时选择运行账户

[Linux 安装脚本](../../runtime/scripts/install-linux.sh) 默认安装当前普通用户的 `molr.service` 用户服务。指定 `--user USER` 时要求 root，安装系统服务，通过 `User=` / `Group=` 让整个 Runtime、文件操作及本机沙盒使用目标身份；Molr 程序不以 root 启动，也不在运行中切换用户。本机无需额外沙盒服务、端口或连接凭证。

首次安装可直接初始化所有者，密码从标准输入读取：

```bash
# 当前账户，无需 sudo。依赖须预先安装。
bash runtime/scripts/install-linux.sh --init-owner owner \
  .cache/build/molr-server /path/server.json < /private/owner-password

# 指定账户，须由管理员执行；账户不存在时自动创建。
sudo bash runtime/scripts/install-linux.sh --user molr-sandbox --init-owner owner \
  .cache/build/molr-server /path/server.json < /private/owner-password
```

`--user` 可以指定已有普通用户，保留其 HOME、登录 shell 和附加组；新用户创建为无交互登录的系统账户，不创建 HOME，不添加额外组或 sudo 权限。root 执行必须显式指定 `--user`，拒绝 root 及其他 UID 0 账户。账户与数据目录的预检查失败时会停止，不递归 chown 现有目录。

| 内容 | 当前用户安装 | 指定用户安装 |
| --- | --- | --- |
| 程序、许可证、卸载器 | `~/.local/lib/molr/<实例>/` | `/opt/molr/<实例>/`，root 管理 |
| 配置与安装记录 | `${XDG_CONFIG_HOME:-$HOME/.config}/molr/<实例>/` | `/etc/molr/<实例>/` |
| 默认持久数据 | `${XDG_DATA_HOME:-$HOME/.local/share}/molr/<实例>/` | `/var/lib/molr/<实例>/` |
| 缓存与服务临时文件 | `${XDG_CACHE_HOME:-$HOME/.cache}/molr/<实例>/` | `/var/cache/molr/<实例>/` |
| 服务文件 | `${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user/<服务名>.service` | `/etc/systemd/system/<服务名>.service` |
| 管理方式 | `systemctl --user … <服务名>.service` | `sudo systemctl … <服务名>.service` |

默认服务名为 `molr`，实例目录名为 `default`；`--name molr-demo` 使用实例目录 `demo` 和服务 `molr-demo.service`。`molr-default` 为保留名称，请使用 `molr`。用户程序目录默认以安装账户的主目录为基准，不随 `XDG_DATA_HOME` 改变。可用 `--program-root /path/programs` 更改程序根目录，程序安装到其 `<实例>/` 子目录；更新时沿用相同参数。程序入口是程序目录下的 `bin/molr-server`；配置文件为配置目录下的 `server.json`，用户安装权限为 `0600`，系统安装为 root 管理、目标组可读的 `0640`。

数据库、密钥、附件、助理项目、技能、记忆和工作区内安装的工具均属于持久数据。服务使用独立缓存目录的 `tmp/` 作为 `TMPDIR`，`XDG_CACHE_HOME` 指向该实例缓存目录；重建缓存不影响持久数据。程序、配置、数据和缓存目录不能重叠。

配置模板指定的 `data_dir` 保留不变；已有数据必须归目标账户所有且可访问，否则报错。首次复制配置时默认设置 `sandbox.host_tools: true`；显式 `false` 或自定义 rootfs 会保留。配置中的证书、外部工作区等路径也必须允许目标账户访问，安装器不会修改这些文件的权限。

升级再次执行相同命令，省略 `--init-owner`：保留安装位置的配置、数据、凭证和用户，程序原子替换并保留 `molr-server.previous`，配置备份为 `server.previous.json`。同名实例不能静默切换账户；需显式迁移数据，或使用新的实例名。迁移前停止原实例并备份，不对运行中数据库直接改属主或复制。

可用 `--name molr-<名称>` 安装独立实例；配置、端口和数据目录也必须独立。`--no-start` 只准备文件，不启用或启动服务，并输出以正确身份初始化和启动的命令。未初始化的数据必须使用 `--init-owner` 或 `--no-start`，不会将一个无法监听的服务报告为安装成功。

安装器检查 systemd ≥254、Python 3、Bubblewrap、静态 BusyBox、CA 证书、file、util-linux；系统安装另需 useradd/runuser。缺失依赖时明确报错，不在普通用户安装过程中调用 sudo 或包管理器。两种 unit 都含 `Type=exec`、`Delegate=yes`、`DelegateSubgroup=supervisor`、`KillMode=control-group`、`NoNewPrivileges=yes`。用户服务需要现有用户管理器；若要求未登录时也启动，管理员可另行配置 `loginctl enable-linger USER`；系统服务无需 linger。

安装后仍须完成下节的已认证沙盒检查。安装器的 `is-active` 只确认服务状态，不代替真实隔离能力验收。

现有 Docker 镜像为精简 Runtime 镜像，尚不包含完整自动命令环境；不能认定在容器中启动 HTTP 后即可执行 Shell。

## 启动后的检查

1. 通过实际 WebUI 地址登录，验证页面、消息与后端连接。
2. 在已登录浏览器通过同源代理请求 `/api/status` 和 `/api/sandbox`，不要只检查端口或进程日志。
3. 未明确关闭沙盒时，要求 `Enabled=true`、`Capabilities.Reason=available`，以及 `Shell`、`ProcessIsolation`、`FileIsolation`、`NetworkIsolation`、`ResourceLimits` 均为 `true`。
4. 用测试助理和非敏感资料验证权限、模型调用与实际结果；未授权能力不能使用。

每次启动、重建或重启后都重新核对。失败原因和处理入口见 [沙盒检查](sandbox.md#检查与验证)；不通过关闭沙盒或回退宿主执行来掩盖异常。

## 卸载

安装包会将离线卸载入口保存为程序目录下的 `uninstall.sh`，无需源码或网络。例如默认用户实例：

```bash
bash ~/.local/lib/molr/default/uninstall.sh --dry-run
bash ~/.local/lib/molr/default/uninstall.sh
```

默认卸载停止对应服务与任务、移除服务注册、程序和实例缓存，保留配置、安装记录及持久数据。重新运行相同实例的安装命令即可复用这些数据；卸载输出会列出保留路径。

彻底清除该实例的受管理配置和数据：

```bash
bash ~/.local/lib/molr/default/uninstall.sh --purge
# 无交互执行时，显式确认数据删除
bash ~/.local/lib/molr/default/uninstall.sh --purge --yes
# 系统安装的实例
sudo bash /opt/molr/demo/uninstall.sh --purge
```

`--purge` 先展示删除计划，再要求输入服务名确认；`--dry-run --purge` 只预览。自定义 `data_dir`、外置 `sandbox.work_root`、授权的外部目录、系统账户、系统依赖和 linger 设置不会被删除。仅安装器登记为受管理的默认数据目录纳入清除，目录挂载了其他文件系统时需先卸载挂载。无法确认服务归属、停止失败或路径校验失败时，中止删除并报告原因。

源码检出提供 `bash scripts/uninstall.sh --name molr-demo`；系统实例加 `--system` 并以 root 执行。也可以使用已解压发行包的 `bash scripts/uninstall-linux.sh --name molr-demo`。这两个入口可在默认卸载后继续清除保留数据；自定义 XDG 目录时沿用安装时的环境变量，或通过 `--record /path/installation.json` 指定保留的安装记录。安装记录中不保存密码和密钥。
