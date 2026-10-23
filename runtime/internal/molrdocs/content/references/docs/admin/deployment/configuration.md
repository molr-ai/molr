# 配置与访问

## 局域网 HTTP

新增 `http.tls_mode: "lan"`，允许数字私网／回环地址和明确的来源列表，不要求证书；监听可为 `0.0.0.0`。默认安装端口从 8787 起，避免与 llama.cpp 的 8080 冲突。`public_url` 使用真实局域网 IP，`allowed_origins` 保存允许访问的完整地址。地址变化时更新配置并重启。

此模式拒绝公网来源连接、未登记 Host／Origin 和转发头，保留登录、CSRF 及限流；会话 Cookie 为 HttpOnly、SameSite，按端口隔离。HTTP 没有传输加密，适用于可信局域网；公网仍使用以下 HTTPS 部署方式。仅回环 `local` 模式也继续可用。

## 配置与初始化

配置采用严格 JSON，参考 [直接 TLS 示例](https://github.com/molr-ai/molr/blob/main/runtime/configs/server.example.json)。修改数据目录、访问地址和证书路径；数据目录需属于运行服务的用户，不能放在公开静态目录下。

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

`tls_mode: local` 仅用于数字回环地址；跨设备的可信局域网 HTTP 使用 `lan` 模式。开发联调与独立端口代理见 [WebUI README](https://github.com/molr-ai/molr/blob/main/apps/webui/README.md)。

## 模型网络与价格配置

模型地址从 Runtime 所在设备访问。默认 `network.allowed_private_ips` 允许 `127.0.0.1`；其他私网 IP 需由部署者在配置中显式增加，修改后重启。该配置接受具体 IP，不是 CIDR；允许一个 IP 会允许访问该 IP 的服务端口。元数据与链路本地地址仍禁止。

Shell 网络策略单独配置地址集合（域名、IP 或多个 CIDR）和端口集合（单端口或多个范围），不继承模型连接的本机许可。示例与私网规则见 [沙箱网络配置](https://molr.ai/zh/docs/admin/sandbox/linux/#网络与高级部署)。用户授权不能扩大部署网络限制。

手动模型价格优先于自动计价，未知价格阻止调用。默认不配置外部价表：

```json
"pricing": { "rules_file": "", "rules_url": "", "exchange_rates_file": "" }
```

`rules_file` 接受本地绝对路径，`rules_url` 接受 HTTPS URL，例如 `https://prices.example.com/models.json`；最多配置其中一项。外部 JSON 格式见 [Runtime README](https://github.com/molr-ai/molr/blob/main/runtime/README.md)。服务启动时及之后每 24 小时检查价表内容；管理员可以在「资源 → 模型 → 更多 → 刷新模型价格配置」立即刷新。检查成功后无需重启，已保存模型使用新规则；手动价格和历史账单不变。按钮在没有外部来源时提示正在使用内置规则，不执行网络下载。

有效配置按来源保存在数据目录的 `pricing-cache/`。来源不可用、JSON 无效或缓存写入失败时，继续使用最近有效配置；首次没有缓存时使用内置规则。远程请求遵守部署网络地址限制，仅接受 HTTPS，禁止重定向。改变价表内容无需重启；改变来源路径／URL 要重新加载服务配置并重启。

参考汇率通过 `pricing.exchange_rates_file` 独立配置，仍在启动时读取。价表与汇率都是带日期的参考配置，不能将参考计费当作供应商最终账单。

配置完成后，继续[安装与服务管理](https://molr.ai/zh/docs/admin/deployment/installation/#linux-服务与命令环境)。

## 微信连接配置

微信连接随 Runtime 运行，通过公网 HTTPS 长轮询接收消息，不需要公网回调。Runtime 必须能访问微信 API 和媒体 CDN；浏览器无需直接接触机器人令牌。机器人令牌、回复凭据和游标使用本机主密钥加密保存。

微信中展示的 Molr 链接使用部署配置中的 `http.public_url`，接收者需要能访问该地址并正常登录。私有局域网地址不会因此自动变成公网入口。

## Telegram 连接配置

Telegram 使用出站 HTTPS 长轮询，不需要公网回调。Runtime 必须能访问 `api.telegram.org`。此渠道忽略环境代理；需要代理时设置 `telegram.proxy_url`（例如 `http://127.0.0.1:3128`）并重启 Runtime。仅接受不含凭据、路径、查询或片段的 HTTP(S) 代理地址。代理属于部署者信任的网络出口，Telegram TLS 验证保持启用。Bot Token 在已认证的助理渠道弹窗中填写，由 Runtime 主密钥加密保存。

Telegram 中的 Molr 链接使用 `http.public_url`，用户需要能访问该地址并登录；长轮询不会让私有 Molr 地址自动变成公网入口。操作见[Telegram 聊天入口](https://molr.ai/zh/docs/user/chat-channels/telegram/)。
