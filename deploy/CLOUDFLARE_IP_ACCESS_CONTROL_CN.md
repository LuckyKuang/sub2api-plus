# Cloudflare + Nginx + 二进制部署的全局 IP 封禁教程

本文适用于以下生产拓扑：

```text
客户端 -> Cloudflare 橙云代理 -> 同机 Nginx -> 127.0.0.1:8080 Sub2API Plus
```

目标是让 Sub2API Plus 的全局 IP 访问控制始终识别真实客户端地址，避免把
Cloudflare 出口节点或 Nginx 本机地址当成用户地址。本文不适用于
Cloudflare Tunnel；Tunnel 的差异见文末。

## 工作边界

Sub2API Plus 的“全局 IP 封禁”覆盖应用的 HTTP、API 和长连接入口，但不会
调用 Cloudflare API，也不会自动创建 Cloudflare WAF 规则。应用封禁仍会让
请求到达源站。容量型攻击、TLS 洪泛、高频扫描和机器人流量应继续在
Cloudflare WAF、Rate Limiting 和源站防火墙处理。

整条可信链必须遵循两层信任：

- Nginx 只信任 Cloudflare 官方出口 CIDR，并从 `CF-Connecting-IP` 解析客户
  端地址。
- Sub2API Plus 只信任直接连接应用的 Nginx，即本教程中的
  `127.0.0.1/32`。

不要将 Cloudflare CIDR、`0.0.0.0/0` 或 `::/0` 写入
`SERVER_TRUSTED_PROXIES`。Cloudflare 并不直接连接本教程中的应用进程。

## 前置条件

- 域名已接入 Cloudflare，并准备开启橙云代理。
- 源站安装 Nginx，且构建包含 `http_realip_module`。
- PostgreSQL、Redis 和 Sub2API Plus 二进制部署满足常规运行条件。
- 已准备 Cloudflare Origin Certificate 或其他可被 Cloudflare 严格验证的
  源站证书。
- 建议准备一个固定的管理员公网出口 IP，作为部署级紧急恢复来源。

确认 Nginx 包含 Real IP 模块：

```bash
sudo nginx -V 2>&1 | grep -- --with-http_realip_module
```

没有输出时，应先安装发行版提供的完整 Nginx 构建，再继续配置。

## 1. 安装并限制二进制监听地址

运行二进制安装脚本：

```bash
curl -sSL https://raw.githubusercontent.com/LuckyKuang/sub2api-plus/main/deploy/install.sh | sudo bash
```

安装器询问服务器监听地址时输入 `127.0.0.1`，端口使用 `8080`。应用不得
监听公网地址；Cloudflare 流量必须先经过 Nginx。

安装后创建 systemd 覆盖配置：

```bash
sudo systemctl edit sub2api
```

写入以下内容：

```ini
[Service]
Environment="SERVER_HOST=127.0.0.1"
Environment="SERVER_PORT=8080"
Environment="SERVER_TRUSTED_PROXIES=127.0.0.1/32"
Environment="SECURITY_TRUST_FORWARDED_IP_FOR_API_KEY_ACL=false"
```

最后一项关闭旧版原始转发头兼容模式，使日志、会话绑定和 API Key ACL 也
使用显式可信代理链。全局 IP 访问控制无论该兼容开关为何值，都会独立使用
`SERVER_TRUSTED_PROXIES`。已有安装若曾在后台保存过“信任反代传递的客户端
IP”，数据库设置会优先于这个初始环境值；完成初始化后还应在后台系统设置中
关闭该选项。

如果管理员具有固定公网出口地址，可在同一个 `[Service]` 段加入部署级紧急
恢复规则。将示例地址替换为真实固定地址后再保存：

```ini
Environment="SERVER_IP_ACCESS_EMERGENCY_ALLOWLIST=203.0.113.10/32"
```

不要把动态家庭宽带地址、Cloudflare CIDR、`127.0.0.1/32` 或全局 `/0`
网络放入紧急恢复列表。修改 systemd 配置后重新加载：

```bash
sudo systemctl daemon-reload
```

重启应用：

```bash
sudo systemctl restart sub2api
```

确认服务正常：

```bash
sudo systemctl status sub2api
```

确认应用只监听本机：

```bash
sudo ss -lntp | grep '127.0.0.1:8080'
```

如果只看到 `0.0.0.0:8080` 或公网地址，不要继续启用全局封禁，应先修正
systemd 配置并重启。

## 2. 配置 Cloudflare

在 Cloudflare 控制台完成以下配置：

1. 将业务域名的 DNS 记录设为“已代理”，即橙云状态。
2. 将 SSL/TLS 加密模式设为 `完全（严格）/ 完全严格 / Full (strict)`，不要使用 `Flexible`。
3. 保持 WebSocket 支持开启，网络（Network）→ WebSockets。
4. 根据业务设置 WAF、机器人防护和 Rate Limiting。
5. 在云安全组或主机防火墙中，只允许 Cloudflare 官方 CIDR 访问源站
   `80/443`；不要对公网开放 `8080`。
6. 在限制 Web 端口前，先保留经过验证的 SSH、VPN 或控制台恢复通道。

`set_real_ip_from` 只控制 Nginx 是否信任请求头，不是防火墙规则。即使 Real
IP 配置正确，未隔离的源站仍可能被绕过 Cloudflare 直接访问。

## 3. 配置 Cloudflare Real IP

Cloudflare 官方地址列表：

- <https://www.cloudflare.com/ips-v4>
- <https://www.cloudflare.com/ips-v6>

下面的 CIDR 于 2026-07-30 从上述官方地址核对。上线前应再次核对，之后也
应定期同步 Cloudflare 的变更。

编辑 Nginx `http` 上下文中加载的配置文件：

```bash
sudoedit /etc/nginx/conf.d/cloudflare-realip.conf
```

写入：

```nginx
underscores_in_headers on;

real_ip_header CF-Connecting-IP;
real_ip_recursive on;

set_real_ip_from 173.245.48.0/20;
set_real_ip_from 103.21.244.0/22;
set_real_ip_from 103.22.200.0/22;
set_real_ip_from 103.31.4.0/22;
set_real_ip_from 141.101.64.0/18;
set_real_ip_from 108.162.192.0/18;
set_real_ip_from 190.93.240.0/20;
set_real_ip_from 188.114.96.0/20;
set_real_ip_from 197.234.240.0/22;
set_real_ip_from 198.41.128.0/17;
set_real_ip_from 162.158.0.0/15;
set_real_ip_from 104.16.0.0/13;
set_real_ip_from 104.24.0.0/14;
set_real_ip_from 172.64.0.0/13;
set_real_ip_from 131.0.72.0/22;

set_real_ip_from 2400:cb00::/32;
set_real_ip_from 2606:4700::/32;
set_real_ip_from 2803:f800::/32;
set_real_ip_from 2405:b500::/32;
set_real_ip_from 2405:8100::/32;
set_real_ip_from 2a06:98c0::/29;
set_real_ip_from 2c0f:f248::/32;

map $http_upgrade $sub2api_connection_upgrade {
    default upgrade;
    '' close;
}

limit_conn_zone $binary_remote_addr zone=sub2api_conn:20m;
limit_req_zone $binary_remote_addr zone=sub2api_auth:20m rate=5r/s;
limit_req_zone $binary_remote_addr zone=sub2api_api:40m rate=30r/s;
```

Debian、Ubuntu 等常见发行版默认在 `http` 块中加载
`/etc/nginx/conf.d/*.conf`。如果当前 Nginx 配置没有该 `include`，应将上面
内容直接放入 `nginx.conf` 的 `http` 块，不能放在 `stream` 块。

当前 Codex 客户端使用 `session-id`；旧版 Codex 和 CRS 兼容客户端仍可能发送
`session_id`。`underscores_in_headers on;` 用于保留这个旧版下划线请求头，
否则 Nginx 默认会将其丢弃，破坏这些客户端的多账号粘性会话。

## 4. 统一覆盖上游客户端 IP 请求头

创建所有 Sub2API 代理路由共用的请求头配置：

```bash
sudoedit /etc/nginx/snippets/sub2api-proxy.conf
```

写入：

```nginx
proxy_http_version 1.1;
proxy_set_header Host $host;
proxy_set_header X-Real-IP $remote_addr;
proxy_set_header X-Forwarded-For $remote_addr;
proxy_set_header X-Forwarded-Proto $scheme;
proxy_set_header CF-Connecting-IP "";
proxy_set_header Upgrade $http_upgrade;
proxy_set_header Connection $sub2api_connection_upgrade;
proxy_buffering off;
proxy_request_buffering off;
proxy_read_timeout 1800s;
proxy_send_timeout 1800s;
```

Cloudflare Real IP 模块完成校验后，`$remote_addr` 就是真实客户端地址。这里
清除原始 `CF-Connecting-IP`，再用单一地址覆盖 `X-Forwarded-For` 和
`X-Real-IP`，确保应用不会收到用户伪造值或多跳地址链。

绝对不要使用下面的追加式配置：

```nginx
proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
```

全局 IP 策略只接受一个经过直接可信代理清洗的地址。逗号分隔、重复头或缺少
转发地址都会被视为不安全；启用强制拦截后相关请求将返回 `503`。

## 5. 配置 Nginx 虚拟主机

编辑站点配置：

```bash
sudoedit /etc/nginx/sites-available/sub2api
```

以下配置中的域名和证书路径必须替换为实际值：

```nginx
server {
    listen 443 ssl http2;
    server_name api.example.com;

    ssl_certificate /etc/nginx/ssl/api.example.com.pem;
    ssl_certificate_key /etc/nginx/ssl/api.example.com.key;

    client_header_timeout 10s;
    client_max_body_size 256m;
    large_client_header_buffers 4 16k;
    limit_conn sub2api_conn 40;

    location ~ ^/(auth|api/auth)/ {
        limit_req zone=sub2api_auth burst=10 nodelay;
        include /etc/nginx/snippets/sub2api-proxy.conf;
        proxy_pass http://127.0.0.1:8080;
    }

    location ~ ^/(v1/)?(embeddings|alpha/search)$ {
        client_max_body_size 32m;
        limit_req zone=sub2api_api burst=60 nodelay;
        include /etc/nginx/snippets/sub2api-proxy.conf;
        proxy_pass http://127.0.0.1:8080;
    }

    location / {
        limit_req zone=sub2api_api burst=60 nodelay;
        include /etc/nginx/snippets/sub2api-proxy.conf;
        proxy_pass http://127.0.0.1:8080;
    }
}
```

每一个包含 `proxy_pass` 的 `location` 都必须加载同一份请求头配置。否则认证
路由可能缺少真实客户端地址，而普通 API 路由看似正常，导致登录或启用全局
策略后间歇性返回 `503`。

如果启用了 Nginx gzip，不得使用 `gzip_types *`，并确保
`text/event-stream` 不在压缩类型中。普通响应可以使用显式列表：

```nginx
gzip on;
gzip_types text/plain text/css application/json application/javascript application/xml image/svg+xml;
```

启用站点。若发行版不使用 `sites-enabled`，应将虚拟主机放入其实际加载的
`http` 配置目录：

```bash
sudo ln -s /etc/nginx/sites-available/sub2api /etc/nginx/sites-enabled/sub2api
```

检查完整配置：

```bash
sudo nginx -t
```

只有检查通过后才重载：

```bash
sudo systemctl reload nginx
```

## 6. 启用前验证可信代理链

通过 Cloudflare 域名登录后台，打开：

```text
https://api.example.com/admin/ip-access-control
```

在“可信代理状态”中确认：

- 配置状态为“已配置”。
- 已加载可信代理为 `127.0.0.1/32`。
- 直连对端 IP 为 `127.0.0.1`，且直连对端可信。
- 当前解析的客户端 IP 是管理员真实公网 IP，不是 `127.0.0.1`，也不是
  Cloudflare CIDR 中的地址。
- 可信代理已经应用。
- 封禁身份状态为“可安全用于全局封禁与自动计数”。
- 全局拦截和手动封禁的启用条件均显示“当前请求满足安全条件”。
- 建议“旧转发头兼容模式”显示“未启用”；如果仍为“已启用”，在后台系统
  设置中关闭“信任反代传递的客户端 IP”。该兼容项不参与全局封禁判断，但
  关闭后旧版日志、会话绑定和 API Key ACL 也会使用显式可信代理链。

检查源站健康接口：

```bash
curl -i https://api.example.com/health
```

检查受全局策略保护的 readiness 接口：

```bash
curl -i https://api.example.com/ready
```

`/health` 是始终豁免的存活检查；`/ready` 会经过全局 IP 策略。代理链不安全
或安全快照不可用时，`/ready` 可能返回 `503`，此时不要启用全局拦截。

## 7. 启用全局 IP 封禁

确认可信代理状态全部正确后，在“IP 访问控制”页面按以下顺序操作：

1. 开启“启用全局 IP 拦截”并保存。
2. 根据需要开启“登录失败自动封禁”。
3. 设置登录失败阈值、统计窗口和封禁时长。
4. 在规则列表中添加手动 IP 或 CIDR 黑名单。
5. 仅在明确的运维恢复场景创建 allow 规则；allow 优先于 block。

登录失败自动封禁只统计本地账号密码和二次验证失败，不把所有 API 错误都
计入。手动封禁和自动封禁生效后，普通 HTTP/API 请求返回 `403` 和
`IP_BANNED`；`/health` 仍返回存活状态，`/ready` 对被封禁来源返回 `403`。

首次测试应使用手机流量、测试 VPN 等第二个公网 IP 创建短期手动封禁，不要
直接封禁当前管理员 IP。确认测试地址访问页面和 API 都返回 `403` 后，在后台
执行解封并重置计数。

## 8. 故障排查与恢复

### 后台显示客户端 IP 为 `127.0.0.1`

检查所有 `proxy_pass` 路由是否加载 `sub2api-proxy.conf`，并确认应用已加载
`SERVER_TRUSTED_PROXIES=127.0.0.1/32`。

### 后台显示 Cloudflare 出口地址

检查 `real_ip_header CF-Connecting-IP;`、Cloudflare 官方 CIDR 和 Real IP
模块。不要用 Cloudflare CIDR 替代应用的 `127.0.0.1/32`。

### 启用后请求返回 `503`

这通常表示代理传入了多个 `X-Forwarded-For` 地址、重复请求头、缺少转发头，
或 Sub2API 收到的直连对端不在可信代理列表。确认使用覆盖式
`X-Forwarded-For $remote_addr`，不要使用 `$proxy_add_x_forwarded_for`。

查看应用日志：

```bash
sudo journalctl -u sub2api -n 200
```

查看 Nginx 展开的最终配置：

```bash
sudo nginx -T
```

### 管理员误封自己

优先从已配置的固定紧急恢复地址访问后台并创建 allow 规则或解封。若没有可用
恢复地址，使用云主机控制台或 SSH 隧道进入源站，先关闭全局拦截或修正规则。
不要为了恢复而将 `SERVER_TRUSTED_PROXIES` 改成 `0.0.0.0/0`。

### 更新 Cloudflare CIDR

分别检查官方 IPv4 和 IPv6 列表：

```bash
curl -fsS https://www.cloudflare.com/ips-v4
```

```bash
curl -fsS https://www.cloudflare.com/ips-v6
```

将变更同步到 `cloudflare-realip.conf`，然后再次执行 `sudo nginx -t`，检查
通过后再执行 `sudo systemctl reload nginx`。同时同步云安全组或主机防火墙的
允许列表。

## Cloudflare Tunnel 的差异

使用 `cloudflared` Tunnel 时，Cloudflare 公网出口 CIDR 不会直接连接本机
Nginx，因此不能照搬本教程中的 `set_real_ip_from` 列表。推荐仍保留 Nginx：

```text
客户端 -> Cloudflare -> cloudflared -> 本机 Nginx -> Sub2API Plus
```

此时 Nginx 只信任 `cloudflared` 的实际本地或私网直连地址，从
`CF-Connecting-IP` 解析客户端 IP，再以单一 `X-Forwarded-For` 地址转发给
Sub2API Plus。Sub2API Plus 仍然只信任直接连接它的 Nginx。Tunnel 的监听
地址、端口和进程隔离方式必须根据实际 `cloudflared` 部署单独确定，不能将
Cloudflare 公网 CIDR 或不受约束的本机网段直接写入可信代理列表。

## 相关文档

- [边缘与 HTTP 入口安全](EDGE_SECURITY.md)
- [部署说明](README.md)
- [完整配置示例](config.example.yaml)
