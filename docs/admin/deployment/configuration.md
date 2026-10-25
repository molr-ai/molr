# 配置与访问

## 配置与初始化

配置采用严格 JSON，参考 [直接 TLS 示例](../../../runtime/configs/server.example.json)。修改数据目录、访问地址和证书路径；数据目录需属于运行服务的用户，不能放在公开静态目录下。

服务启动前，在部署主机上初始化所有者。以下使用安装后的 `molr` 命令，`/private` 为需替换的示例路径；源码构建时使用实际程序路径：

```bash
molr init -config /private/server.json -username owner -password-stdin < /private/owner-password
```

密码从权限受限文件或密码管理器送入标准输入，不放入命令行参数。未初始化时服务不会监听；没有 HTTP 注册或初始化入口。

## 开放局域网访问

设置 `http.tls_mode: "lan"`，允许数字私网／回环地址和明确的来源列表，不要求证书；监听可为 `0.0.0.0`。默认安装端口从 8787 起，避免与 llama.cpp 的 8080 冲突。`public_url` 使用真实局域网 IP，`allowed_origins` 保存允许访问的完整地址。地址变化时更新配置并重启。

此模式拒绝公网来源连接、未登记 Host／Origin 和转发头，保留登录、CSRF 及限流；会话 Cookie 为 HttpOnly、SameSite，按端口隔离。HTTP 没有传输加密，适用于可信局域网；公网仍使用以下 HTTPS 部署方式。仅回环 `local` 模式也继续可用。

## 开放公网访问

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

`tls_mode: local` 仅用于数字回环地址；跨设备的可信局域网 HTTP 使用 `lan` 模式。开发联调与独立端口代理见 [WebUI README](../../../apps/webui/README.md)。

## 连接模型服务

模型请求由 Runtime 发出，服务地址必须能从 Runtime 所在设备访问。在「资源 → 模型 → 更多 → 连接提供商」中选择预设或自定义服务，填写基础地址与认证信息，测试并保存连接，再刷新模型列表。模型能力、价格与用户组授权按[模型接入概览](../resources/models.md#连接提供商)配置；连接测试只读取模型目录，实际推理需通过对话验证。

模型连接使用 `network.allowed_private_ips` 控制私网访问；Shell 使用独立的[沙盒网络配置](../sandbox/linux.md#网络与高级部署)，不会放行模型地址。用户授权不能扩大部署网络限制。

### 连接本地模型

先在 Runtime 所在设备启动模型服务并准备模型，再使用本机发现结果或手动填写地址。例如，[Ollama](../resources/providers/ollama.md) 的基础地址为 `http://127.0.0.1:11434/v1`。本机发现仅查询已有服务，不会启动服务或下载模型。

默认 `network.allowed_private_ips` 包含 `127.0.0.1`；使用其他回环地址（例如 `::1`）时，需将该地址加入配置并重启 Runtime。这里的本机指 Runtime 所在设备；容器内的回环地址指容器自身，访问宿主机模型时应使用容器可达的宿主机地址，并按下一节放行。

### 连接局域网模型

让模型服务监听 Runtime 可达的局域网地址，并在模型主机的防火墙中允许 Runtime 访问服务端口。连接时将基础地址中的回环地址替换为模型主机的局域网 IP，保留服务要求的端口和 API 路径。

在 Runtime 配置中将模型主机 IP 加入 `network.allowed_private_ips`，保留仍需访问的已有地址，然后重启 Runtime。以下 `192.168.1.20` 为示例模型主机 IP，需替换为实际地址：

```json
{
  "network": {
    "allowed_private_ips": ["127.0.0.1", "192.168.1.20"]
  }
}
```

该列表接受具体 IP，不接受域名或 CIDR；允许一个 IP 会允许访问该 IP 的服务端口。使用域名连接时，其解析地址也必须通过网络校验。元数据与链路本地地址始终禁止。放行后的私网模型可使用 HTTP，适用于可信局域网；服务要求认证时仍需填写对应密钥。

### 连接云服务

选择对应提供商预设，按其接入说明填写 HTTPS 基础地址和 API Key；支持方式和各提供商文档见[模型接入概览](../resources/models.md#支持的接入方式)。Runtime 需要能够解析并访问该服务地址，公网 HTTP 地址会被拒绝，公网 IP 无需加入 `network.allowed_private_ips`。

使用自建网关时，按[自定义服务](../resources/providers/custom.md)配置地址与密钥，并确认网关提供兼容协议；局域网网关沿用上一节的私网访问配置。

## 配置模型价格

手动模型价格优先于自动计价，未知价格阻止调用。默认不配置外部价表：

```json
"pricing": { "rules_file": "", "rules_url": "", "exchange_rates_file": "" }
```

`rules_file` 接受本地绝对路径，`rules_url` 接受 HTTPS URL，例如 `https://prices.example.com/models.json`；最多配置其中一项。外部 JSON 格式见 [Runtime README](../../../runtime/README.md)。服务启动时及之后每 24 小时检查价表内容；管理员可以在「资源 → 模型 → 更多 → 刷新模型价格配置」立即刷新。检查成功后无需重启，已保存模型使用新规则；手动价格和历史账单不变。按钮在没有外部来源时提示正在使用内置规则，不执行网络下载。

有效配置按来源保存在数据目录的 `pricing-cache/`。来源不可用、JSON 无效或缓存写入失败时，继续使用最近有效配置；首次没有缓存时使用内置规则。远程请求遵守部署网络地址限制，仅接受 HTTPS，禁止重定向。改变价表内容无需重启；改变来源路径／URL 要重新加载服务配置并重启。

参考汇率通过 `pricing.exchange_rates_file` 独立配置，仍在启动时读取。价表与汇率都是带日期的参考配置，不能将参考计费当作供应商最终账单。

配置完成后，继续[安装与服务管理](installation.md#linux-服务与命令环境)。
