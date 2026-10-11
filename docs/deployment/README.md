# 部署与维护

当前支持从仓库构建内嵌 WebUI 的本地 Linux 发行包，也可分别构建 Go Runtime 与 WebUI。Runtime 是单实例服务，使用 SQLite 和本地文件；同一数据目录不能同时运行多个实例。本文面向 Linux 自托管，其他系统不具备同等目录工具与 Shell 能力。

## 本地发行包与快速安装

当前发行流程在本地运行。发行构建需要 Node.js、指定版本的 Go、Python 3 和系统 tar，构建检查仅使用 Python 标准库。发行构建在本地运行：

```bash
npm ci
npm run build:release
```

构建先检查待发布源码和公开静态文件，再生成 WebUI，随后以 `-trimpath` 静态编译带 `webui` build tag 的 Runtime，将页面与 API 放在同一服务。产物在 `dist/releases/`，包含 `molr_<版本>_linux_<架构>.tar.gz`、同名 `.sha256` ；推荐版本清单通过下述发布步骤单独生成。包内包含 Runtime、安装器、`LICENSE` 和完整的 `THIRD_PARTY_NOTICES.md`，不附带 BusyBox。默认版本为 `0.1.0-local`；`MOLR_VERSION`、`MOLR_ARCH`、`MOLR_DIST_DIR` 可覆盖。跨架构构建不需要准备目标架构 BusyBox；安装目标机通过系统包管理器提供静态 BusyBox。`GO` 可指定 Go 程序路径。`molr-server version` 显示构建版本与提交。归档只收集 `scripts/release-policy.json` 中明确列出的文件，验证通过后才生成最终归档和下载清单；不要分发构建失败后留下的目录，也不要直接压缩整个工作目录。

如需导出源码包，在已提交并审查过的版本上执行 `npm run build:source -- --ref HEAD`。该命令只导出指定 Git 提交，工作目录中的修改、未提交新文件、`.cache/` 和运行数据不会被打包。详细发布范围及检查限制见[开源发布与私有文件隔离](../../dev-docs/RELEASE_SAFETY.md)。

从安装包安装：

```bash
bash scripts/install.sh --archive dist/releases/molr_0.1.0-local_linux_amd64.tar.gz
# 独立实例示例
bash scripts/install.sh --archive /path/package.tar.gz --name molr-demo --port 8788 --public-host "<LAN_IP>"
```

以上命令中的 `<LAN_IP>` 需替换为安装或下载服务器的实际局域网 IP，不能原样执行。

`--source URL` 从下载目录读取当前架构的最新版本清单；`--version VERSION` 固定版本。校验失败或架构不匹配时不会执行包内安装器。HTTP 下载源只适合可信局域网，校验文件用于检测损坏，不构成独立的发布者签名。

官网构建始终输出 `/install.sh`，脚本与 `scripts/install.sh` 同源。需要完整本地下载站点时：

```bash
MOLR_RELEASE_DIR="$PWD/dist/releases" \
MOLR_RELEASE_URL="http://<LAN_IP>:5190/releases" \
VITE_SITE_MODE=live npm run build:website
npm run preview:website
```

上述环境变量让网站包含指定发行目录，并为脚本配置默认下载源；不设置时只输出脚本，需要安装者指定 `--source` 或 `--archive`。

省略配置文件的安装器会自动生成局域网 HTTP 配置、初始化所有者与 setup、安装服务并检查已认证 API。`--install-deps` 提供 apt 依赖安装，`--yes` 明确同意依赖安装，`--skip-dependencies` 跳过。缺少隔离依赖不自动开放 Full Access，沙盒能力按真实探针报告。安装与卸载需要 Python 3.11+，systemd 和基本安装工具仍需可用。安装器检查 BusyBox 必须是可执行、不可被组或其他用户写入的静态程序；只有动态 BusyBox 时仍会提示安装 `busybox-static`。不支持 apt 或离线安装时，请预先通过目标系统的包管理器安装静态 BusyBox 等依赖。安装器不复制 BusyBox 到 Molr 安装目录。

`LICENSE` 和 `THIRD_PARTY_NOTICES.md` 保存在安装前缀根目录，例如默认实例的用户安装目录 `~/.local/lib/molr/default/` 或系统安装目录 `/opt/molr/default/`。内嵌 WebUI、独立 WebUI 和官网均提供 `/LICENSE` 与 `/THIRD_PARTY_NOTICES.md`；分发构建目录时应保留这两个文件。许可证正文集中维护在仓库根目录，不使用第三方 submodule。

用户服务未启用 linger 时，退出登录可能停止服务；要让系统在未登录时运行，由管理员执行 `loginctl enable-linger USER`。指定账户的系统服务不依赖 linger。

安装完成后可用以下命令检查或重新生成访问入口：

```bash
/path/molr-server doctor -config /path/server.json
/path/molr-server access -config /path/server.json
```

默认使用数据目录内的随机初始密码。已改密时增加 `-password-stdin`，启用 MFA 时增加 `-mfa-code`；这些命令通过正常认证 API 工作，不打开运行中的数据库，不绕过 MFA。链接有效期 15 分钟，实际交付链接不用于健康检查。

## 版本检查与发布

用户设置 → 关于显示 WebUI 与 Runtime 的版本、commit 和一致性状态，同时展示发布源的推荐版本。登录后每五分钟读取服务端版本状态，页面重新可见或连接恢复时补查；关闭自动发布检查仍保留前后端一致性检查。版本或 commit 不一致时显示提示，由用户保存输入后自行刷新，或由管理员核对分离部署的版本。

Runtime 配置支持：

```json
{"updates": {"source": "https://downloads.example.com/releases", "auto_check": true}}
```

在线安装使用的发布源自动保存到配置；离线安装保留已有值。无发布源时不会联网。`auto_check: false` 关闭后台及关于页自动发布检查，仍可手动检查。开发／本地构建默认不自动检查。公网源要求 HTTPS；HTTP 源必须是明确配置的私网或回环数字地址。

后台启动后延迟 30～60 秒检查，随后每 24 小时加少量随机延迟检查，失败一小时后重试。进入关于页或页面恢复可见时，超过五分钟未检查则刷新发布信息。手动检查最短间隔一分钟；全实例合并并发请求。检查失败保留上次成功结果及时间，检查本身不下载或安装程序。

构建只生成版本包和校验文件，不改变推荐版本。发布前准备版本包、校验文件和 HTTPS 更新说明页面，再生成推荐清单：

```bash
npm run release:promote -- --version 0.2.0 --arch amd64 \
  --notes-url https://example.com/releases/0.2.0
```

默认读取 `dist/releases/`，可通过 `--directory` 指定目录。命令验证包的版本、架构和校验值后生成 `latest-linux-<架构>.json`，包含版本、commit、发布时间、更新说明 URL、包文件名和 SHA-256；不执行上传。先上传并验证包及更新说明，最后发布清单。推荐版本可以是合法预发布版，不接受开发／本地版本。已发布版本的包应保持不可变，修改后使用新版本号。

管理员在发现更新后可查看固定目标版本的升级命令。运行账户、实例名和程序路径取自安装记录；源码部署或缺少记录时按本指南操作。示例：

```bash
bash install.sh --source https://downloads.example.com/releases --version 0.2.0 --name molr
```

升级前按本页备份流程保留数据。安装器核对下载包的版本和架构，保留已有账号、数据和配置，保存 `molr-server.previous` 与 `server.previous.json` 后替换程序并重启。服务重启会中断运行中的任务。启动后先等待 HTTP 就绪，再执行一次认证检查，核对实际 Runtime 版本及沙盒能力；认证需要当前密码／MFA或被限流时，输出“验证待完成”和 `doctor` 命令，不循环登录。失败保留备份和错误信息，不自动回滚；上一份程序及配置不包含完整数据备份。

## 局域网 HTTP

新增 `http.tls_mode: "lan"`，允许数字私网／回环地址和明确的来源列表，不要求证书；监听可为 `0.0.0.0`。默认安装端口从 8787 起，避免与 llama.cpp 的 8080 冲突。`public_url` 使用真实局域网 IP，`allowed_origins` 保存允许访问的完整地址。地址变化时更新配置并重启。

此模式拒绝公网来源连接、未登记 Host／Origin 和转发头，保留登录、CSRF 及限流；会话 Cookie 为 HttpOnly、SameSite，按端口隔离。HTTP 没有传输加密，适用于可信局域网；公网仍使用以下 HTTPS 部署方式。仅回环 `local` 模式也继续可用。

## 构建

使用 [runtime/.go-version](../../runtime/.go-version) 指定的 Go 版本，以及根目录 [package.json](../../package.json) 要求的 Node.js 版本。以下命令在自己的部署检出目录执行；仓库开发任务遵守 [worktree 约定](../../AGENTS.md)。

```bash
mkdir -p .cache/build
cd runtime
CGO_ENABLED=0 GOTOOLCHAIN="go$(cat .go-version)" go build -o ../.cache/build/molr-server ./cmd/molr-server
chmod 0755 ../.cache/build/molr-server
file ../.cache/build/molr-server
go version -m ../.cache/build/molr-server
cd ..
npm ci
npm run build:webui
```

确认 Runtime 为静态链接，构建信息包含 `CGO_ENABLED=0`，最终文件权限为 `0755`。部署主服务时还需核对目标提交及 `vcs.modified=false`。WebUI 产物位于 `apps/webui/dist/`，由静态服务器提供；不要将 Vite 开发服务当作公网生产入口。

## 配置与初始化

配置采用严格 JSON，参考 [直接 TLS 示例](../../runtime/configs/server.example.json)。修改数据目录、访问地址和证书路径；数据目录需属于运行服务的用户，不能放在公开静态目录下。

服务启动前，在部署主机上初始化所有者。以下 `/path` 和 `/private` 均为需替换的示例路径：

```bash
/path/molr-server init -config /private/server.json -username owner -password-stdin < /private/owner-password
```

密码从权限受限文件或密码管理器送入标准输入，不放入命令行参数。未初始化时服务不会监听；没有 HTTP 注册或初始化入口。

公网访问使用 HTTPS，可信局域网也可选择上文的 `lan` HTTP 模式。可选直接 TLS，或在受信任反向代理处终止 TLS。代理模式的 Runtime 示例：

```json
{
  "data_dir": "/private/molr-data",
  "http": {
    "listen": "127.0.0.1:8080",
    "public_url": "https://molr.example.com",
    "tls_mode": "proxy",
    "trusted_proxies": ["127.0.0.1/32"],
    "allowed_origins": ["https://molr.example.com"]
  }
}
```

下面是已有 HTTPS Nginx `server` 内的路由片段，证书和域名配置需另行完成。示例只信任同机反向代理，按实际拓扑调整，不能扩大为信任任意来源。

```nginx
location / {
    root /srv/molr/webui;
    try_files $uri $uri/ /index.html;
}
location /api/ {
    proxy_pass http://127.0.0.1:8080;
    proxy_set_header Host $http_host;
    proxy_set_header X-Forwarded-Proto https;
    proxy_set_header X-Forwarded-For $remote_addr;
    proxy_buffering off;
    proxy_read_timeout 1h;
}
```

`tls_mode: local` 仅用于数字回环地址；跨设备的可信局域网 HTTP 使用 `lan` 模式。开发联调与独立端口代理见 [WebUI README](../../apps/webui/README.md)。

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

## 启动后的检查

1. 通过实际 WebUI 地址登录，验证页面、消息与后端连接。
2. 在已登录浏览器通过同源代理请求 `/api/status` 和 `/api/sandbox`，不要只检查端口或进程日志。
3. 未明确关闭沙盒时，要求 `Enabled=true`、`Capabilities.Reason=available`，以及 `Shell`、`ProcessIsolation`、`FileIsolation`、`NetworkIsolation`、`ResourceLimits` 均为 `true`。
4. 用测试助理和非敏感资料验证权限、模型调用与实际结果；未授权能力不能使用。

每次启动、重建或重启后都重新核对。失败原因和处理入口见 [沙盒检查](sandbox.md#检查与验证)；不通过关闭沙盒或回退宿主执行来掩盖异常。

## 模型网络与价格配置

模型地址从 Runtime 所在设备访问。默认 `network.allowed_private_ips` 允许 `127.0.0.1`；其他私网 IP 需由部署者在配置中显式增加，修改后重启。该配置接受具体 IP，不是 CIDR；允许一个 IP 会允许访问该 IP 的服务端口。元数据与链路本地地址仍禁止。

Shell 网络策略单独配置地址集合（域名、IP 或多个 CIDR）和端口集合（单端口或多个范围），不继承模型连接的本机许可。示例与私网规则见 [沙箱网络配置](sandbox.md#网络与高级部署)。用户授权不能扩大部署网络限制。

手动模型价格优先于内置计价，未知价格阻止调用。内置价格与参考汇率是带日期的配置，不是实时报价。可以分别通过 `pricing.rules_file` 与 `pricing.exchange_rates_file` 指定文件；格式与规则实现见 [Runtime README](../../runtime/README.md)。变更后按配置要求重启并核对用量，不能将参考计费当作供应商最终账单。

## 备份恢复与升级

旧单账号安装升级到多用户版本，需要显式执行[一次性账号转换](multi-user-upgrade.md)；新版启动不会自动转换旧账号。

数据目录权限为 `0700`，主密钥 `keys/master.key` 为 `0600`。已有数据库缺少主密钥时服务拒绝启动，不能用新密钥替代旧密钥。

停止对应实例后执行维护命令：

```bash
/path/molr-server backup -config /private/server.json > /private/backup.json
/path/molr-server restore -config /private/server.json -backup-manifest /private/backup.json
```

`backup.json` 是备份描述，命令只生成数据库快照，不打包完整实例。完整灾备应在停服后保留配置及数据目录的完整副本；若使用数据库快照恢复，还须保留快照文件及其描述，并同时恢复以下数据：

| 内容 | 备份位置与要求 |
| --- | --- |
| 附件与交付文件 | 数据目录的 `files/` |
| 加密主密钥 | 数据目录的 `keys/`，按秘密材料保护 |
| 助理工作区 | 默认 `sandbox/work/`；配置了 `sandbox.work_root` 时备份实际外置路径 |
| 部署配置及外置文件 | 配置文件、证书、外置价格／汇率文件等，保留实际路径和权限 |

网络额度用量和未结算预留已包含在实际执行端数据库中：本机为 `runtime.db`，独立 Sandbox 为 `sandbox.db`。

快照与上述文件须来自同一停服备份窗口。外部授权目录和远程 Sandbox 的数据不包含在 Runtime 数据库快照中，需在对应机器独立备份；恢复工作区项目配额时还需核对文件系统的 project ID 与配额配置。仅复制 WAL 使用中的主数据库文件不是完整备份。

恢复要求服务停止，成功后撤销恢复副本中的旧会话、访问令牌和挑战。恢复后先确认账号、资源与凭证可用，再核对未完成操作，避免重复执行结果未知的外部动作。

忘记密码时，由有主机管理权的操作者停服后执行 `recover -password-stdin`；需要重置二次验证时另加 `-reset-mfa`。本机恢复会撤销全部会话与令牌。

升级前保留旧二进制、配置和完整备份。使用目标提交的干净构建，核对静态链接、权限和摘要后，用临时文件原子替换二进制，再重启对应实例。代码回退不自动撤销数据迁移或外部副作用。升级完成后重新做页面、认证、模型及沙盒能力检查。

独立 Linux 执行机器的部署与连接见 [远程 Sandbox 服务](remote-sandbox.md)。本机沙盒仍内嵌于 Runtime，无需额外服务。


## 模型响应诊断日志

原始模型 HTTP／SSE 响应默认不保存，也不进入数据库。排障时可在服务配置中设置：

```json
{
  "diagnostics": { "model_responses": true }
}
```

重启后生效。响应写入 `<data_dir>/logs/model-responses/responses.jsonl`，最多保留当前文件和两份轮转文件，每份不超过 64 MiB，文件权限为 0600。日志包含响应正文及调用 ID，可能包含对话内容；不记录请求认证头。日志写入失败不影响模型执行。关闭开关停止新增记录，已有文件需要按部署方的保留要求删除。

此开关与 `debug.enabled` 独立；工作台仍可展示摘要、标准消息内容、HTTP 状态、耗时及用量，无需开启原始响应日志。
