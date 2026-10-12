# 配置与访问

## 局域网 HTTP

新增 `http.tls_mode: "lan"`，允许数字私网／回环地址和明确的来源列表，不要求证书；监听可为 `0.0.0.0`。默认安装端口从 8787 起，避免与 llama.cpp 的 8080 冲突。`public_url` 使用真实局域网 IP，`allowed_origins` 保存允许访问的完整地址。地址变化时更新配置并重启。

此模式拒绝公网来源连接、未登记 Host／Origin 和转发头，保留登录、CSRF 及限流；会话 Cookie 为 HttpOnly、SameSite，按端口隔离。HTTP 没有传输加密，适用于可信局域网；公网仍使用以下 HTTPS 部署方式。仅回环 `local` 模式也继续可用。

## 配置与初始化

配置采用严格 JSON，参考 [直接 TLS 示例](../../runtime/configs/server.example.json)。修改数据目录、访问地址和证书路径；数据目录需属于运行服务的用户，不能放在公开静态目录下。

服务启动前，在部署主机上初始化所有者。以下 `/path` 和 `/private` 均为需替换的示例路径：

```bash
/path/molr init -config /private/server.json -username owner -password-stdin < /private/owner-password
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

## 模型网络与价格配置

模型地址从 Runtime 所在设备访问。默认 `network.allowed_private_ips` 允许 `127.0.0.1`；其他私网 IP 需由部署者在配置中显式增加，修改后重启。该配置接受具体 IP，不是 CIDR；允许一个 IP 会允许访问该 IP 的服务端口。元数据与链路本地地址仍禁止。

Shell 网络策略单独配置地址集合（域名、IP 或多个 CIDR）和端口集合（单端口或多个范围），不继承模型连接的本机许可。示例与私网规则见 [沙箱网络配置](../sandbox/linux.md#网络与高级部署)。用户授权不能扩大部署网络限制。

手动模型价格优先于内置计价，未知价格阻止调用。内置价格与参考汇率是带日期的配置，不是实时报价。可以分别通过 `pricing.rules_file` 与 `pricing.exchange_rates_file` 指定文件；格式与规则实现见 [Runtime README](../../runtime/README.md)。变更后按配置要求重启并核对用量，不能将参考计费当作供应商最终账单。

配置完成后，继续[安装与服务管理](installation.md#linux-服务与命令环境)。
