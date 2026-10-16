# Configuration and Access

## LAN HTTP

Added `http.tls_mode: "lan"`, which allows numeric private/loopback addresses and an explicit allowlist, without requiring certificates; the listener can be `0.0.0.0`. The default installation port starts at 8787 to avoid conflicts with llama.cpp's 8080. `public_url` uses the actual LAN IP, and `allowed_origins` stores the full addresses allowed to access. Update the configuration and restart when the address changes.

This mode rejects connections from public sources, unregistered Host/Origin headers, and forwarded headers, while retaining login, CSRF protection, and rate limiting; session cookies are HttpOnly, SameSite, and isolated by port. HTTP has no transport encryption and is suitable for trusted LANs; public networks should still use the HTTPS deployment methods described below. The loopback-only `local` mode remains available.

## Configuration and Initialization

Configuration uses strict JSON; refer to the [Direct TLS Example](../../../../runtime/configs/server.example.json). Modify the data directory, access address, and certificate paths; the data directory must belong to the user running the service and must not be placed in a public static directory.

Before starting the service, initialize the owner on the deployment host. The following `/path` and `/private` are example paths that must be replaced:

```bash
/path/molr init -config /private/server.json -username owner -password-stdin < /private/owner-password
```

Passwords are passed via standard input from a permission-restricted file or password manager, not via command-line arguments. The service will not listen if uninitialized; there is no HTTP registration or initialization endpoint.

Public access uses HTTPS; trusted LANs may also choose the `lan` HTTP mode described above. Direct TLS is optional, or TLS can be terminated at a trusted reverse proxy. Example Runtime configuration for proxy mode:

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

Below is a routing snippet within an existing HTTPS Nginx `server` block; certificate and domain configuration must be completed separately. The example trusts only the same-host reverse proxy; adjust according to the actual topology and do not expand trust to arbitrary sources.

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

`tls_mode: local` is used only for numeric loopback addresses; for trusted LAN HTTP across devices, use `lan` mode. For development integration and independent port proxying, see the [WebUI README](../../../../apps/webui/README.md).

## Model Network and Pricing Configuration

Model addresses are accessed from the device where the Runtime resides. By default, `network.allowed_private_ips` allows `127.0.0.1`; other private IPs must be explicitly added by the deployer in the configuration, followed by a restart. This configuration accepts specific IPs, not CIDRs; allowing an IP allows access to service ports on that IP. Metadata and link-local addresses remain prohibited.

Shell network policies separately configure address sets (domains, IPs, or multiple CIDRs) and port sets (single ports or multiple ranges), and do not inherit local permissions from model connections. See [Sandbox Network Configuration](../sandbox/linux.md#network-and-advanced-deployment) for examples and private network rules. User authorization cannot expand deployment network restrictions.

Manual model pricing takes precedence over built-in pricing; unknown prices block calls. Built-in prices and reference exchange rates are dated configurations, not real-time quotes. Files can be specified separately via `pricing.rules_file` and `pricing.exchange_rates_file`; for format and rule implementation details, see the [Runtime README](../../../../runtime/README.md). After changes, restart as required by the configuration and verify usage; do not treat reference billing as the final vendor bill.

After configuration is complete, proceed to [Installation and Service Management](installation.md#linux-services-and-command-environment).

## WeChat connectivity

The WeChat connection runs with the Runtime, receiving messages via public HTTPS long polling, and does not require public callbacks. The Runtime must be able to access the WeChat API and media CDN; the browser does not need to directly access the bot token. The bot token, reply credentials, and cursor are encrypted and stored using the local master key.

Molr links displayed in WeChat use `http.public_url` from the deployment configuration; recipients must be able to access this address and log in normally. Private LAN addresses do not automatically become public entry points because of this.

## Telegram configuration

Telegram uses outbound HTTPS long polling; no public webhook is required. The Runtime must reach `api.telegram.org`. Environment proxies are ignored by this integration. If a proxy is needed, configure `telegram.proxy_url`, for example `http://127.0.0.1:3128`, and restart the Runtime. Only HTTP(S) proxy origins without credentials, paths, queries or fragments are accepted. The proxy is a deployment-trusted egress; Telegram TLS validation remains enabled. Bot Tokens are entered through the authenticated assistant channel dialog and stored encrypted with the Runtime master key.

Links in Telegram use `http.public_url`; recipients must be able to access it and sign in. Long polling does not make a private Molr address publicly accessible. See [Telegram chat](../../user/chat-channels/telegram.md).
