# Configuration and Access

## Configuration and Initialization

Configuration uses strict JSON; refer to the [Direct TLS Example](../../../../runtime/configs/server.example.json). Modify the data directory, access address, and certificate paths; the data directory must belong to the user running the service and must not be placed in a public static directory.

Before starting the service, initialize the owner on the deployment host. The following uses the installed `molr` command; replace the `/private` example paths. For a source build, use the actual executable path:

```bash
molr init -config /private/server.json -username owner -password-stdin < /private/owner-password
```

Passwords are passed via standard input from a permission-restricted file or password manager, not via command-line arguments. The service will not listen if uninitialized; there is no HTTP registration or initialization endpoint.

## Enable LAN Access

Set `http.tls_mode: "lan"` to allow numeric private/loopback addresses and an explicit allowlist, without requiring certificates; the listener can be `0.0.0.0`. The default installation port starts at 8787 to avoid conflicts with llama.cpp's 8080. `public_url` uses the actual LAN IP, and `allowed_origins` stores the full addresses allowed to access. Update the configuration and restart when the address changes.

This mode rejects connections from public sources, unregistered Host/Origin headers, and forwarded headers, while retaining login, CSRF protection, and rate limiting; session cookies are HttpOnly, SameSite, and isolated by port. HTTP has no transport encryption and is suitable for trusted LANs; public networks should still use the HTTPS deployment methods described below. The loopback-only `local` mode remains available.

## Enable Public Access

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

## Connect Model Services

The Runtime sends model requests, so the service address must be reachable from the device running the Runtime. In Resources → Models → More → Connect provider, select a preset or custom service, enter the base URL and authentication details, test and save the connection, then refresh the model list. Configure capabilities, prices and group permissions using the [model connection overview](../resources/models.md#connecting-providers). Connection tests only read the model catalog; verify inference through a conversation.

Model connections use `network.allowed_private_ips` to control private-network access. Shell uses separate [sandbox network configuration](../sandbox/linux.md#network-and-advanced-deployment), which does not allow model addresses. User permissions cannot expand deployment network restrictions.

### Connect Local Models

Start the model service and prepare models on the device running the Runtime, then select a discovered local service or enter its address manually. For example, the [Ollama](../resources/providers/ollama.md) base URL is `http://127.0.0.1:11434/v1`. Local discovery only queries existing services; it does not start services or download models.

By default, `network.allowed_private_ips` includes `127.0.0.1`. To use another loopback address, such as `::1`, add it to the configuration and restart the Runtime. Local means the device running the Runtime. Inside a container, loopback refers to the container itself; to connect to a model on the host, use a host address reachable from the container and allow it as described below.

### Connect LAN Models

Configure the model service to listen on a LAN address reachable from the Runtime, and allow the Runtime to access its port through the model host's firewall. Replace the loopback address in the connection's base URL with the model host's LAN IP, keeping the port and API path required by the service.

Add the model host IP to `network.allowed_private_ips` in the Runtime configuration, retain existing addresses that are still needed, and restart the Runtime. Replace the example model host IP `192.168.1.20` below with the actual address:

```json
{
  "network": {
    "allowed_private_ips": ["127.0.0.1", "192.168.1.20"]
  }
}
```

The list accepts specific IPs, not domains or CIDRs. Allowing an IP allows access to service ports on that IP. When connecting through a domain, its resolved addresses must also pass network validation. Metadata and link-local addresses are always prohibited. Allowed private model services can use HTTP on trusted LANs; enter the appropriate key if the service requires authentication.

### Connect Cloud Services

Select the provider preset and enter its HTTPS base URL and API Key according to its connection guide. See the [model connection overview](../resources/models.md#supported-connection-methods) for supported methods and provider documentation. The Runtime must be able to resolve and reach the service address. Public HTTP addresses are rejected; public IPs do not need to be added to `network.allowed_private_ips`.

For a self-hosted gateway, configure its URL and key as a [custom service](../resources/providers/custom.md) and verify protocol compatibility. LAN gateways use the private-network configuration described above.

## Configure Model Prices

Manual prices take precedence over automatic pricing; unknown prices block calls. External pricing sources are disabled by default:

```json
"pricing": { "rules_file": "", "rules_url": "", "exchange_rates_file": "" }
```

`rules_file` accepts an absolute local path; `rules_url` accepts an HTTPS URL such as `https://prices.example.com/models.json`. Configure at most one. See the [Runtime README](../../../../runtime/README.md) for the external JSON format. The service checks the source at startup and every 24 hours. Administrators can immediately check through Resources → Models → More → Refresh model pricing. Valid updates apply without restarting, including to saved models; manual prices and historical receipts remain unchanged. With no external source, the button reports built-in pricing without a download.

Valid configurations are cached per source under the data directory’s `pricing-cache/`. An unavailable source, invalid JSON or cache-write failure retains the last valid rules. Without a valid cache, startup uses built-in rules. Remote requests follow deployment network restrictions, require HTTPS and reject redirects. Editing price content needs no restart; changing the configured source path/URL requires reloading the service configuration and restarting.

Reference exchange rates remain separately configured through `pricing.exchange_rates_file` and are read at startup. Both prices and exchange rates are dated reference configurations, not the vendor’s final invoice.

After configuration is complete, proceed to [Installation and Service Management](installation.md#linux-services-and-command-environment).
