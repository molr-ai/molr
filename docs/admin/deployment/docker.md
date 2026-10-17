# Docker 部署

仓库根目录的 Dockerfile 构建完整 Molr：先构建 WebUI，再将页面内嵌进静态 Go 程序。一个容器通过同一个端口提供网页和 API，无需另启前端服务。Node、Go 与 Debian 基础镜像均固定版本和多架构 SHA-256 摘要；Go 版本与 `runtime/.go-version` 一致。

当前通过源码本地构建镜像，尚未提供可直接拉取的官方 Molr 镜像。需要 Git、Docker Engine 或使用 Linux 容器的 Docker Desktop，以及 Docker Compose。宿主机无需安装 Go 或 Node.js。Linux 容器支持 AMD64、ARM64；构建器默认选择本机架构。

## 构建与配置

```bash
git clone https://github.com/molr-ai/molr.git
cd molr
cp deploy/docker/server.example.json deploy/docker/server.json
MOLR_COMMIT="$(git rev-parse HEAD)" docker compose build
```

构建固定源码版本时，先检出所需 tag 或 commit，再构建。基础镜像的固定摘要不会随上游标签更新；更新基础镜像时同步核验并修改版本与摘要。可通过 Docker 构建参数 `NODE_IMAGE`、`GO_IMAGE`、`RUNTIME_IMAGE` 使用维护者信任的镜像代理，保留相同摘要。

编辑 `deploy/docker/server.json`。默认只登记 `http://127.0.0.1:8787`；局域网使用时，将 `http.public_url` 和 `http.allowed_origins` 中的地址改为 `http://<LAN_IP>:8787`，其中 `<LAN_IP>` 是 Docker 宿主机的数字局域网 IP。需要同时允许本机与局域网访问时，在 `allowed_origins` 中显式列出两个地址。

`http.listen` 保持 `0.0.0.0:8787`。LAN 模式要求公开地址、容器监听和 Compose 映射使用相同端口；改端口时同时修改三处。配置以只读文件挂载，修改后重启容器。HTTPS 与反向代理见[配置与访问](configuration.md)。

## 初始化与启动

首次运行先初始化账号，再启动服务：

```bash
docker compose run --rm molr bootstrap -config /etc/molr/server.json
docker compose up -d
docker compose logs --tail=50 molr
```

初始化自动创建 `owner`，将随机初始密码保存到数据卷中的 `/data/initial-password`（0600），不会打印密码。重复 bootstrap 保留已有账号；应在服务停止时执行，不能同时打开正在使用的数据库。

确认日志中的监听已启动后，生成一次性登录链接：

```bash
docker compose exec molr /usr/local/bin/molr access -config /etc/molr/server.json
```

链接默认有效 15 分钟。容器必须能访问配置的 `public_url`；Docker Desktop 或宿主机防火墙阻止容器回连宿主端口时，可通过浏览器访问配置的地址，并使用 `owner` 和初始密码登录：

```bash
docker compose exec molr cat /data/initial-password
```

此命令会显示密码，请在私有终端使用。改密后使用 `access -password-stdin` 输入当前密码；启用 MFA 时还需 `-mfa-code`。不要循环执行登录诊断，成功登录同样计入限流。首次引导、模型和成员配置见[搭建实例](../getting-started.md)。

## 命令执行方式

默认示例显式设置 `sandbox.enabled: false`。网页、聊天、工作区、文件工具和记忆照常可用，命令执行按以下方式选择。

### 独立 Linux 沙盒

在另一台 Linux 设备或宿主机上按[独立沙盒服务](../sandbox/remote.md)部署 `molr serve --mode sandbox`，在 WebUI 中连接资源，再为助理授予计算、目录和网络权限。主容器保持默认配置；远程节点需通过自己的隔离能力检查。

连接局域网模型或沙盒时，在主容器配置的 `network.allowed_private_ips` 中显式加入目标数字 IP。容器的 `127.0.0.1` 指向容器自身，不能用于访问宿主机上的模型或沙盒。远程节点不会自动获得主容器的 `/work` 文件；需要访问的远端目录按资源授权登记。

### 容器内 Full Access

镜像包含 `/bin/sh`，可在配置中显式启用：

```json
{
  "sandbox": {
    "enabled": true,
    "allow_full_access": true
  }
}
```

将这两个字段合并到现有配置后执行 `docker compose restart molr`，再在 WebUI 中为指定助理授予本机沙盒的 Full Access 计算权限。只修改部署开关不会自动授权所有助理。

命令以容器内的普通用户 `65532:65532` 运行。Compose 保留只读根文件系统、移除 Linux capabilities，并禁止提权；可写内容主要是数据卷和临时目录。Full Access 不提供助理之间的命令级文件／进程／网络隔离，命令可以访问该服务用户可读写的数据，包括 Runtime 私有数据和其他工作区。Docker 的边界保护容器与宿主机，不能替代容器内不同助理的隔离。不要挂载 Docker socket、宿主根目录或不准备授权的资料。

工具需求超出基础系统时，可基于本镜像制作派生镜像，在构建阶段以 root 安装依赖，最后恢复 `USER 65532:65532`；容器运行时不安装系统软件。

### 容器内隔离执行

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


## 验证与维护

在服务就绪后执行一次已认证检查：

```bash
docker compose exec molr /usr/local/bin/molr doctor -config /etc/molr/server.json
```

默认模式应为 `Sandbox.Enabled=false`、`Storage=true`；显式开启 Full Access 时要求 `Capabilities.FullAccess=true`，不能把它解释为隔离能力可用。完整隔离部署还需 `Reason=available`，以及 `Shell`、`ProcessIsolation`、`FileIsolation`、`NetworkIsolation`、`ResourceLimits` 全部为 true。登录失败或限流属于验证未完成，不代表容器需要重启。

`molr-data` 命名卷保存数据库、密钥、账号、工作区及初始密码；Compose 会加项目名前缀。重建或替换容器保留数据卷。更新时先停止本实例并备份数据卷与配置，再检出目标源码版本、重新构建并启动；固定源码和镜像 tag 可用于回退，数据库备份仍需单独保留。容器更新通过重建镜像完成，不在 WebUI 内替换只读程序。

```bash
docker compose stop
# Back up the data volume and deploy/docker/server.json while stopped.
MOLR_COMMIT="$(git rev-parse HEAD)" docker compose build
docker compose up -d
```

卸载容器使用 `docker compose down`，保留持久卷；`docker compose down -v` 会删除实例数据，仅在明确需要清空时使用。镜像保留 Molr 的 MIT 与第三方声明，Debian 软件的许可材料保留在 `/usr/share/doc/`。
