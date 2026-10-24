# Docker 部署

后端和独立沙盒节点使用同一个普通版镜像 `ghcr.io/molr-ai/molr`，支持 Linux AMD64、ARM64。宿主机只需 Docker；选择 Compose 方式时还需 Docker Compose。镜像首次启动自动生成配置、账号或节点凭据，无需预先下载配置、运行 bootstrap 或安装 OpenSSL。

镜像基于 Debian 13 slim，包含静态 Molr 程序与内嵌 WebUI，以普通用户 `65532:65532` 运行。以下使用命名卷，Docker 自动创建并保留数据，不需要处理宿主目录所有权。Docker 与 Compose 两种方式任选一种。

## 镜像版本

| 标签 | 含义 |
| --- | --- |
| `latest` | 最新正式版；首个正式版镜像发布前不可用 |
| `0.1.0` | 固定正式版示例 |
| `0.2.0-beta.1` / `0.2.0-rc.1` | 固定预发布版示例 |
| `nightly` | 最新验证通过的每日构建，无固定每日版标签 |

以下默认使用 `latest`；首个正式版发布前，将它替换为 `nightly`。生产部署可固定正式版本或镜像 digest。镜像内部保留完整版本与 commit；Nightly 历史不承诺长期保留或可回退。公开镜像可匿名拉取。

## 部署后端

首次安装默认使用“个人”预设，允许接入服务账号可访问的目录、网络和计算资源，并开放可单独授权的完全访问。普通镜像不提供容器内受控计算；需要隔离执行时可连接支持该能力的沙盒节点。

### Docker 命令

```bash
docker run -d \
  --name molr \
  --init --restart unless-stopped \
  -v molr-data:/data \
  -p 8787:8787 \
  ghcr.io/molr-ai/molr:latest
```

启动后访问 `http://127.0.0.1:8787`。首次启动创建 `owner` 账号并把随机初始密码保存到 `/data/initial-password`（0600），不打印密码到日志。配置保存在 `/data/server.json`，重启和替换容器不会重置账号、配置或数据。

### Docker Compose

将以下内容保存为 `compose.yaml`：

```yaml
services:
  molr:
    image: ghcr.io/molr-ai/molr:latest
    init: true
    restart: unless-stopped
    ports:
      - "8787:8787"
    volumes:
      - molr-data:/data

volumes:
  molr-data:
```

运行 `docker compose up -d` 即可。仓库中的 `compose.prebuilt.yaml` 是对应模板，可通过 `MOLR_IMAGE` 环境变量选择镜像。Compose 的数据卷带项目名前缀；升级时保留同一项目名。

### 登录与检查

在私有终端读取初始密码，以 `owner` 登录网页：

```bash
# Docker
docker exec molr cat /data/initial-password
# Compose
docker compose exec molr cat /data/initial-password
```

也可生成一次性登录链接，默认有效 15 分钟：

```bash
# Docker
docker exec molr /usr/local/bin/molr access -config /data/server.json
# Compose
docker compose exec molr /usr/local/bin/molr access -config /data/server.json
```

容器需要能访问配置的 `public_url`；不能回连时使用前述密码登录。改密后 CLI 使用 `-password-stdin`，启用 MFA 时使用 `-mfa-code`。不要循环执行登录诊断，成功登录也计入限流。

服务就绪后执行 `docker exec molr /usr/local/bin/molr doctor -config /data/server.json`，或对应 Compose `exec` 命令。个人预设下检查 `Enabled=true`、`Storage=true`、`Capabilities.FullAccess=true`；普通镜像的 `Capabilities.Shell=false` 表示受控计算不可用。初次登录后按网页引导连接模型。

## 部署沙盒节点

### Docker 命令

```bash
docker run -d \
  --name molr-sandbox \
  --init --restart unless-stopped \
  -v molr-sandbox-data:/data \
  -p 8443:8443 \
  -e MOLR_MODE=sandbox \
  ghcr.io/molr-ai/molr:latest
```

`MOLR_MODE=sandbox` 选择独立节点模式，不创建账号或网页服务。首次启动自动生成 `/data/sandbox.json`、`/data/private/server.crt`、`/data/private/server.key` 和 `/data/private/sandbox-token`，并准备可写项目目录 `/data/projects`。证书、私钥和令牌在重启时保留；缺失已有证书的私钥时拒绝启动，不悄悄更换节点身份。

### Docker Compose

将以下内容保存为单独目录中的 `compose.yaml`：

```yaml
services:
  molr-sandbox:
    image: ghcr.io/molr-ai/molr:latest
    init: true
    restart: unless-stopped
    environment:
      MOLR_MODE: sandbox
    ports:
      - "8443:8443"
    volumes:
      - molr-sandbox-data:/data

volumes:
  molr-sandbox-data:
```

运行 `docker compose up -d` 即可。仓库中的对应模板是 `compose.sandbox.yaml`。每个后端使用自己的节点和数据卷；多个实例还需不同的容器名或 Compose 项目名、端口。

### 连接与权限

在私有终端导出连接码：

```bash
# Docker
docker exec molr-sandbox molr connection-code --config /data/sandbox.json
# Compose
docker compose exec molr-sandbox molr connection-code --config /data/sandbox.json
```

在后端按[连接沙盒](https://molr.ai/zh/docs/admin/resources/sandbox/#在-webui-连接)填写 `https://<节点宿主机IP>:8443`，粘贴连接码，测试连接通过后保存。地址由用户填写，不包含在连接码中；后端自动校验连接码格式版本和节点实际协议版本，不要求发行版本号完全一致。

连接码包含公钥指纹和服务令牌，只在主动执行导出命令时输出，不写入启动日志。它是连接凭据，不能公开分享。重启、升级和证书续期不改变连接码；更换私钥或令牌后需重新导出并更新后端连接凭据。

自动生成的自签名证书有效期为一年，默认启用 `http.auto_renew_certificate`。启动时和运行期间每小时检查，剩余有效期不足 30 天时沿用原私钥续期，原子替换证书；新 TLS 连接使用新证书，已有连接不中断。过期后启动也会先续期。自定义证书默认由管理员维护；只有自签名证书可显式开启自动续期，CA 证书不由此机制签发。后端固定验证公钥指纹及证书有效期。

普通版容器节点提供文件访问和显式授权的 Full Access 命令，不提供容器内进程、文件、网络隔离或命令级资源限额。Docker 隔离容器与宿主机；Full Access 命令可访问服务用户能读写的容器文件，包括私有凭据和其他授权目录。`restrict_directories` 限制文件工具登记范围，不限制 Full Access 命令。

连接后验证已认证节点信息的 `Enabled=true`、`Storage=true`、`Capabilities.FullAccess=true`，再按需为助理单独授予完全访问。完全访问包括服务账号可访问的文件、命令与网络；普通调用仍使用分别授予的存储、计算和网络权限。需要按助理隔离命令时使用原生沙盒，或本文最后的源码隔离目标。

## 地址与自定义配置

| 环境变量 | 用途 |
| --- | --- |
| `MOLR_RESOURCE_PRESET` | 首次生成配置时使用 `permissive`（默认，个人）或 `least-privilege`（最小权限）；已有配置不重新应用预设 |
| `MOLR_MODE` | `server`（默认）或 `sandbox` |
| `MOLR_PUBLIC_URL` | 显式设置访问地址；启动时更新配置中的公开地址、监听端口，后端同时替换原允许来源 |

后端默认只接受本机地址。局域网访问时，在 `docker run` 中增加 `-e MOLR_PUBLIC_URL=http://<宿主机局域网IP>:8787`；Compose 在服务的 `environment` 中配置同名变量。使用数字局域网 IP。修改端口时，公开 URL、容器监听和端口映射保持一致，例如 `MOLR_PUBLIC_URL=http://192.168.1.20:9898` 搭配 `9898:9898`。

没有设置 `MOLR_PUBLIC_URL` 时保留已有配置；显式设置时保留其他自定义字段与额外允许来源。HTTP 快速启动限本机与可信局域网，不自动信任任意 Host／Origin。公网 HTTPS、反向代理和信任范围按[配置与访问](https://molr.ai/zh/docs/admin/deployment/configuration/)设置。

配置文件位于数据卷内，可在停止容器后复制出来修改，再写回同一路径，保持 0600 和 `65532:65532` 所有权。重新启动后读取修改内容。错误配置不会被默认值覆盖。需要完全自行管理配置时，仍可使用原有 `serve -config`、`bootstrap` 等命令和只读配置挂载。

后端和独立节点首次安装复用同一资源预设。个人预设启用命令执行和 Full Access，允许登记任意容器内目录，并预登记互联网、局域网、宿主服务网络资源；这些网络权限用于受控命令，不决定 Runtime 自身联网能力。预设不会自动授予助理权限，也不会让普通镜像获得容器内隔离能力。“本机”指容器环境，文件和网络仍受容器挂载、运行账号及外部网络策略约束。

选择最小权限时，Docker 增加 `-e MOLR_RESOURCE_PRESET=least-privilege`；预构建 Compose 模板可执行 `MOLR_RESOURCE_PRESET=least-privilege docker compose -f compose.prebuilt.yaml up -d`。这只影响新数据卷首次生成的配置。已有实例修改卷内 `server.json` 或 `sandbox.json` 后重启，重装和修改该环境变量不会覆盖已有权限设置。

容器内不自动安装工具，额外软件在派生镜像构建阶段安装，最后恢复 `USER 65532:65532`。

绑定宿主目录替代命名卷时，目录必须可由 UID/GID `65532:65532` 读写；容器不会以 root 递归修改宿主文件所有权。需要宿主资料时只挂载明确授权的目录，不挂载宿主根目录或 Docker socket。只读资料使用只读挂载。

可按部署要求增加 `read_only: true`、`cap_drop: [ALL]`、`security_opt: [no-new-privileges:true]`，同时为 `/tmp` 配置可写 tmpfs；启动配置、凭据及持久数据仍写入 `/data`。Docker 的 CPU／内存限制作用于整个容器，不是按助理分配的额度。

## 更新、备份与卸载

Runtime 与 WebUI 随镜像一起更新。预构建后端统一检查 GHCR 的 `latest` 正式版镜像，不随当前安装版本切换到 Nightly 或预发布。正式版高于当前版本时提示更新；当前版本更高时不推荐降级。没有正式版时明确显示暂无适用正式版，不回退其他渠道。需要继续使用 Nightly 时，管理员可手动拉取 `nightly` 并重建容器。版本详情提供目标镜像与宿主操作指引，不在容器内替换程序。独立节点由管理员随后端版本更新。

修改 Compose 中的目标镜像版本后：

```bash
docker compose pull
docker compose stop
# Back up the original data volume while stopped.
docker compose up -d
```

Docker 命令方式先拉取目标镜像，再停止原容器、备份命名卷，删除原容器后使用前文相同参数和原卷重建。只执行 `restart` 不会采用新镜像。自动入口会保留账号、数据库和节点身份，无需再次初始化。

后端备份整个数据卷；节点也备份整个卷，其中包含配置、私有凭据及项目文件。更新后重新验证登录、版本与启用的能力。固定版本或记录的 digest 可用于选择旧镜像；需要恢复数据时使用对应备份，不把换镜像等同于恢复数据。

`docker compose down` 保留数据卷，`down -v` 会删除项目管理的卷。Docker `rm` 不删除本文的命名卷；只有明确要清空实例时才删除卷。

## 从源码构建

```bash
git clone https://github.com/molr-ai/molr.git
cd molr
cp deploy/docker/server.example.json deploy/docker/server.json
MOLR_COMMIT="$(git rev-parse HEAD)" docker compose build
docker compose run --rm molr bootstrap -config /etc/molr/server.json
docker compose up -d
```

本地 `compose.yaml` 保留显式配置与手工初始化；预构建模板使用自动入口 `molr container`。普通 Dockerfile 默认目标自动启动，显式传入其他 CLI 命令时保持原命令行为。默认本地版本为 `0.1.0-local`，不自动比较发布版本。Node、Go、Debian slim 固定多架构摘要；代理必须保留同一摘要。静态 Go 程序去除调试信息，最终镜像不含 Node／Go 工具链，保留许可证与第三方声明。

## 从源码构建容器内隔离目标

此目标仅保留源码本地构建，不发布官方隔离版镜像，也不属于官网普通版 Docker／Compose 安装入口。先按本页“从源码构建”检出仓库，再执行以下步骤。

`isolated` 镜像目标预装 systemd、Bubblewrap、静态 BusyBox、util-linux 和 CA 证书。容器内 systemd 以普通用户运行 Molr，并通过 `Delegate=yes` 与 `DelegateSubgroup=supervisor` 分配专属 cgroup；进程、文件、网络隔离及资源限额使用原有沙盒实现，`allow_full_access` 保持 false。

本模板面向原生 Linux 上的 rootful Docker、cgroup v2、AppArmor 4.0+ 和 `/dev/net/tun`。容器必须使用私有 PID/cgroup namespace；不挂载宿主 cgroup、根目录或 Docker socket。Rootless Docker、userns-remap、Docker Desktop 和无 AppArmor 的宿主不在此模板的验收范围内，可使用默认容器加独立 Linux 沙盒。

先在宿主加载专用策略，再选择隔离配置和镜像目标。宿主需要 `apparmor_parser`；该策略只作用于显式选择它的容器，不替换 `docker-default`，也不修改全局 sysctl。

```bash
sudo apparmor_parser -r -W deploy/docker/molr-docker-isolated.apparmor
cp deploy/docker/server.isolated.example.json deploy/docker/server.json
```

按前文编辑配置中的访问地址。下面的函数固定选择两个 Compose 文件，后续初始化、启动和管理均使用同一组合：

```bash
dc() { docker compose -f compose.yaml -f compose.isolated.yaml "$@"; }
MOLR_COMMIT="$(git rev-parse HEAD)" dc build
dc run --rm molr bootstrap -config /etc/molr/server.json
dc up -d
dc logs --tail=50 molr
```

等待 HTTP 就绪，再以服务用户检查能力并签发登录链接：

```bash
dc exec --user 65532:65532 molr /usr/local/bin/molr doctor -config /etc/molr/server.json
dc exec --user 65532:65532 molr /usr/local/bin/molr access -config /etc/molr/server.json
```

要求 `Enabled=true`、`FullAccess=false`、`Reason=available`，且 `Shell`、`ProcessIsolation`、`FileIsolation`、`NetworkIsolation`、`ResourceLimits` 全部为 true。页面可访问而能力检查失败时，命令环境尚未就绪；不要改为 Full Access 来掩盖失败。助理仍须获得对应计算、目录和网络授权。

容器 PID 1 使用 `SYS_ADMIN` 等明确列出的 capability 准备其私有 cgroup 并启动服务；Molr 服务的 permitted、effective、bounding、ambient capability 集合均清空，并启用 `NoNewPrivileges`。专用 AppArmor 允许嵌套 namespace 与挂载，Unix 通信和进程检查对端限定为同一策略。外层 seccomp 保留固定版本的 Docker 默认规则，只补充 `pivot_root` 以及受限参数的 `pidfd_getfd`、`process_vm_readv`，供 Bubblewrap 和受信任的 socket broker 使用；助理命令的内层 seccomp 仍禁止这些调用。

`systempaths=unconfined` 移除妨碍嵌套 procfs 的 Docker 覆盖挂载，对应敏感 `/proc`、`/sys` 路径改由专用 AppArmor 拒绝规则保护。因此该选项必须与所提供的 AppArmor 策略一起使用，不能单独移植到普通容器。`/dev/net/tun` 只提供给受信任网络 helper，助理看到的是独立的 `/dev`。配置没有使用 `privileged`、`seccomp=unconfined` 或 `apparmor=unconfined`。

策略文件需要在宿主重启后重新加载；长期部署可由管理员安装到 `/etc/apparmor.d/` 并使用宿主的 AppArmor 服务加载。停止所有使用该策略的容器后，可用 `sudo apparmor_parser -R deploy/docker/molr-docker-isolated.apparmor` 卸载。不同实例通过 Compose 项目名使用各自的数据卷和 cgroup。

真实内核回归使用独立测试镜像与临时卷，不连接模型服务，不使用已有实例的数据：

```bash
docker compose -p molr-isolation-tests -f compose.yaml -f compose.isolated.yaml -f compose.isolation-test.yaml build
docker compose -p molr-isolation-tests -f compose.yaml -f compose.isolated.yaml -f compose.isolation-test.yaml up --abort-on-container-exit --exit-code-from molr --no-build
docker compose -p molr-isolation-tests -f compose.yaml -f compose.isolated.yaml -f compose.isolation-test.yaml down --volumes
```

测试覆盖隔离文件与 PID、只读挂载、cgroup 限额、网络允许与拒绝、取消及撤权。需要外网、GPU 或额外浏览器组件的测试按其环境开关执行，未启用项明确跳过。
