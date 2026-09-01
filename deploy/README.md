# Sub2API Plus Deployment Files

This directory contains files for deploying Sub2API Plus on Linux servers and Apple-silicon Macs.

## Release Version Mapping

Git tags and GitHub Releases use `vX.Y.Z+custom.NNN`. The release workflow
derives the OCI image tag by preserving the leading `v` and replacing only
`+` with `-`.

```text
Git/GitHub: v0.1.185+custom.001
GHCR:       ghcr.io/luckykuang/sub2api-plus:v0.1.185-custom.001
```

Pin the GHCR version tag for reproducible deployments. See
[`UPSTREAM.md`](../UPSTREAM.md) for iteration and upstream-baseline rules.

## Deployment Methods

| Method | Best For | Setup Wizard |
|--------|----------|--------------|
| **Docker Compose** | Quick setup, all-in-one | Not needed (auto-setup) |
| **Apple container** | Native local stack on macOS 26 | Not needed (auto-setup) |
| **Binary Install** | Production servers, systemd | Web-based wizard |

## Files

| File | Description |
|------|-------------|
| `docker-compose.yml` | Docker Compose configuration (named volumes) |
| `docker-compose.local.yml` | Docker Compose configuration (local directories, easy migration) |
| `docker-deploy.sh` | **One-click Docker deployment script (recommended)** |
| `apple-container.sh` | Native Apple `container` lifecycle script |
| `APPLE_CONTAINER.md` | Apple `container` deployment and operations guide |
| `.env.example` | Container environment variables template |
| `DOCKER.md` | Docker Hub documentation |
| `install.sh` | One-click binary installation script |
| `install-datamanagementd.sh` | datamanagementd 一键安装脚本 |
| `sub2api.service` | Systemd service unit file |
| `sub2api-datamanagementd.service` | datamanagementd systemd service unit file |
| `DATAMANAGEMENTD_CN.md` | datamanagementd 部署与联动说明（中文） |
| `config.example.yaml` | Example configuration file |
| `EDGE_SECURITY.md` | Reverse proxy, CDN/WAF, trusted proxy, and ingress hardening guide |
| `CLOUDFLARE_IP_ACCESS_CONTROL_CN.md` | Cloudflare + Nginx binary deployment and global IP blocking tutorial (Chinese) |

---

## Apple container Deployment

Apple-silicon Macs running macOS 26 can run the complete Sub2API Plus, PostgreSQL, and Redis stack with Apple `container` 1.1.0 or newer:

```bash
./apple-container.sh init
```

```bash
./apple-container.sh up
```

```bash
./apple-container.sh status
```

```bash
./apple-container.sh logs app -f
```

The script uses Apple named volumes by default, starts dependencies in order,
replaces the application container on every `up` so its writable layer does
not accumulate, and performs live readiness checks. Use
`./apple-container.sh upgrade` to update only the application image while
retaining one rollback image, and `./apple-container.sh disk-usage` to inspect
Apple Containers disk use. Set the optional `APPLE_CONTAINER_*_DATA_DIR`
variables in `.env` to replace an individual named volume with a host bind
mount; leaving them empty preserves the original named-volume behavior.
Persistent mounts are never cleared by application redeployment. The script
does not provide a continuous restart supervisor; run
`./apple-container.sh up` after a host reboot. Docker Compose remains the
recommended production deployment path.

See [APPLE_CONTAINER.md](./APPLE_CONTAINER.md) for configuration, upgrades, persistence, networking behavior, and limitations.

---

## Docker Deployment (Recommended)

### Method 1: One-Click Deployment (Recommended)

Use the automated preparation script for the easiest setup:

```bash
# Download and run the preparation script
curl -sSL https://raw.githubusercontent.com/luckykuang/sub2api-plus/main/deploy/docker-deploy.sh | bash

# Or download first, then run
curl -sSL https://raw.githubusercontent.com/luckykuang/sub2api-plus/main/deploy/docker-deploy.sh -o docker-deploy.sh
chmod +x docker-deploy.sh
./docker-deploy.sh
```

**What the script does:**
- Downloads `docker-compose.local.yml` and `.env.example`
- Automatically generates secure secrets (JWT_SECRET, TOTP_ENCRYPTION_KEY, POSTGRES_PASSWORD)
- Creates `.env` file with generated secrets
- Creates necessary data directories (data/, postgres_data/, redis_data/)
- **Displays generated credentials** (POSTGRES_PASSWORD, JWT_SECRET, etc.)

**After running the script:**
```bash
# Start services
docker compose -f docker-compose.local.yml up -d

# View logs
docker compose -f docker-compose.local.yml logs -f sub2api

# If admin password was auto-generated, find it in logs:
docker compose -f docker-compose.local.yml logs sub2api | grep "admin password"

# Access Web UI
# http://localhost:8080
```

### Method 2: Manual Deployment

If you prefer manual control:

```bash
# Clone repository
git clone https://github.com/luckykuang/sub2api-plus.git
cd sub2api-plus/deploy

# Configure environment
cp .env.example .env
chmod 600 .env
nano .env  # Set POSTGRES_PASSWORD and other required variables

# Generate persistent secrets (required for durable TOTP, backup secrets, and Prompt Audit endpoint tokens)
JWT_SECRET=$(openssl rand -hex 32)
TOTP_ENCRYPTION_KEY=$(openssl rand -hex 32)
echo "JWT_SECRET=${JWT_SECRET}" >> .env
echo "TOTP_ENCRYPTION_KEY=${TOTP_ENCRYPTION_KEY}" >> .env

# Create data directories
mkdir -p data postgres_data redis_data

# Start all services using local directory version
docker compose -f docker-compose.local.yml up -d

# View logs (check for auto-generated admin password)
docker compose -f docker-compose.local.yml logs -f sub2api

# Access Web UI
# http://localhost:8080
```

### Deployment Version Comparison

| Version | Data Storage | Migration | Best For |
|---------|-------------|-----------|----------|
| **docker-compose.local.yml** | Local directories (./data, ./postgres_data, ./redis_data) | ✅ Easy (tar entire directory) | Production, need frequent backups/migration |
| **docker-compose.yml** | Named volumes (/var/lib/docker/volumes/) | ⚠️ Requires docker commands | Simple setup, don't need migration |

**Recommendation:** Use `docker-compose.local.yml` (deployed by `docker-deploy.sh`) for easier data management and migration.

### How Auto-Setup Works

When using Docker Compose with `AUTO_SETUP=true`:

1. On first run, the system automatically:
   - Connects to PostgreSQL and Redis
   - Applies database migrations (SQL files in `backend/migrations/*.sql`) and records them in `schema_migrations`
   - Generates JWT secret (if not provided)
   - Creates admin account (password auto-generated if not provided)
   - Writes config.yaml

2. No manual Setup Wizard needed - just configure `.env` and start

3. If `ADMIN_PASSWORD` is not set, check logs for the generated password:
   ```bash
   docker compose logs sub2api | grep "admin password"
   ```

### Startup and Database Recovery

Sub2API applies database migrations during application startup. PostgreSQL can
remain in its recovery/startup phase briefly after a host or Docker daemon
restart. The application retries transient PostgreSQL startup and connection
errors with bounded exponential backoff, then starts automatically when the
database becomes ready. Authentication errors, migration checksum mismatches,
SQL errors, and other permanent configuration or data errors fail immediately.

The Compose example also uses a PostgreSQL health check that verifies both
server readiness and a simple SQL query. `depends_on: condition: service_healthy`
controls dependency ordering for a fresh Compose start, but it is not a
replacement for application-level retries when Docker restores existing
containers after a host restart.

For systemd deployments, keep `Restart=always` and `RestartSec` configured in
`sub2api.service`; the application retry covers transient database startup,
while systemd remains the supervisor for permanent process exits. For
Kubernetes, use a PostgreSQL readiness probe and retain the Sub2API startup
retry behavior; configure the application liveness probe separately so a
database recovery period is not treated as a permanent process failure.

### Database Migration Notes (PostgreSQL)

- Migrations are applied in lexicographic order (e.g. `001_...sql`, `002_...sql`).
- `schema_migrations` tracks applied migrations (filename + checksum).
- Migrations are forward-only; rollback requires a DB backup restore or a manual compensating SQL script.
- Published Plus `v0.1.178+custom.001` introduced migrations `224`, `225`, `226`,
  and `228` in lexical order. Migration `227` is intentionally unused; `228`
  is the next unique prefix for the platform-constraint correction.
- Upgrades from the latest published Plus release, `v0.1.178+custom.005`, to
  `v0.1.183+custom.001` additionally apply migrations `229` through `233`:
  usage-log indexes, Composite CN providers, channel pricing multipliers,
  OAuth transport plugins, and plugin artifacts.
- Before upgrading to a release that adds migrations, back up PostgreSQL. The
  application rollback commands below only change the application image or
  binary; they do not reverse schema, data, functions, or triggers. Restoring
  the previous database behavior requires a backup restore or an audited
  compensating SQL migration.

**Verify `users.allowed_groups` → `user_allowed_groups` backfill**

During the incremental GORM→Ent migration, `users.allowed_groups` (legacy `BIGINT[]`) is being replaced by a normalized join table `user_allowed_groups(user_id, group_id)`.

Run this query to compare the legacy data vs the join table:

```sql
WITH old_pairs AS (
  SELECT DISTINCT u.id AS user_id, x.group_id
  FROM users u
  CROSS JOIN LATERAL unnest(u.allowed_groups) AS x(group_id)
  WHERE u.allowed_groups IS NOT NULL
)
SELECT
  (SELECT COUNT(*) FROM old_pairs)           AS old_pair_count,
  (SELECT COUNT(*) FROM user_allowed_groups) AS new_pair_count;
```

### datamanagementd（数据管理）联动

如需启用管理后台“数据管理”功能，请额外部署宿主机 `datamanagementd`：

- 主进程固定探测 `/tmp/sub2api-datamanagement.sock`
- Docker 场景下需把宿主机 Socket 挂载到容器内同路径
- 详细步骤见：`deploy/DATAMANAGEMENTD_CN.md`

### Commands

For **local directory version** (docker-compose.local.yml):

```bash
# Start services
docker compose -f docker-compose.local.yml up -d

# Stop services
docker compose -f docker-compose.local.yml down

# View logs
docker compose -f docker-compose.local.yml logs -f sub2api

# Restart Sub2API Plus only
docker compose -f docker-compose.local.yml restart sub2api

# Update to latest version
docker compose -f docker-compose.local.yml pull
docker compose -f docker-compose.local.yml up -d

# Remove all data (caution!)
docker compose -f docker-compose.local.yml down
rm -rf data/ postgres_data/ redis_data/
```

For **named volumes version** (docker-compose.yml):

```bash
# Start services
docker compose up -d

# Stop services
docker compose down

# View logs
docker compose logs -f sub2api

# Restart Sub2API Plus only
docker compose restart sub2api

# Update to latest version
docker compose pull
docker compose up -d

# Remove all data (caution!)
docker compose down -v
```

### Environment Variables

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `POSTGRES_PASSWORD` | **Yes** | - | PostgreSQL password |
| `JWT_SECRET` | **Recommended** | *(auto-generated)* | JWT secret (fixed for persistent sessions) |
| `TOTP_ENCRYPTION_KEY` | **Required when using encrypted secrets** | *(auto-generated)* | Fixed encryption key for persistent 2FA, backup secrets, and Prompt Audit endpoint tokens. |
| `SKIP_SETUP` | No | `false` | Recovery-only bypass for first-run admin setup. Do not enable for a new or normal deployment. |
| `SERVER_PORT` | No | `8080` | Server port |
| `ADMIN_EMAIL` | No | `admin@sub2api.local` | Admin email |
| `ADMIN_PASSWORD` | No | *(auto-generated)* | Admin password |
| `TZ` | No | `Asia/Shanghai` | Timezone |
| `UPDATE_GITHUB_TOKEN` | No | *(empty)* | Token for `api.github.com` release checks only; asset downloads remain anonymous. |
| `WEBAUTHN_ENABLED` | No | `false` | Enables the WebAuthn deployment boundary; Passkey remains off until an administrator enables it in System Settings. |
| `WEBAUTHN_RP_DISPLAY_NAME` | No | `Sub2API` | Relying-party name shown by the authenticator. |
| `WEBAUTHN_RP_ID` | When WebAuthn is enabled | *(empty)* | Exact relying-party domain without scheme or port. |
| `WEBAUTHN_RP_ORIGINS` | When WebAuthn is enabled | *(empty)* | Comma-separated allowed HTTPS origins; localhost HTTP is supported for local testing. |
| `SERVER_TRUSTED_PROXIES` | No | *(empty)* | Comma-separated direct reverse-proxy/container CIDRs trusted for client IP recovery. Never use a public CDN range or `/0`. |
| `SERVER_IP_ACCESS_EMERGENCY_ALLOWLIST` | No | *(empty)* | Fixed administrator egress CIDRs that can bypass global IP blocks during proxy recovery. |
| `SECURITY_TRUST_FORWARDED_IP_FOR_API_KEY_ACL` | No | `false` | Legacy raw forwarded-header compatibility. Keep disabled with `SERVER_TRUSTED_PROXIES`; an existing database setting can override the initial environment value. |
| `GATEWAY_OPENAI_PROXY_STREAM_CIRCUIT_DISABLED` | No | `false` | Disables the bounded OpenAI proxy stream/transport-failure circuit only for incident diagnosis. |
| `GEMINI_OAUTH_CLIENT_ID` | No | *(builtin)* | Google OAuth client ID (Gemini OAuth). Leave empty to use the built-in Gemini CLI client. |
| `GEMINI_OAUTH_CLIENT_SECRET` | No | *(builtin)* | Google OAuth client secret (Gemini OAuth). Leave empty to use the built-in Gemini CLI client. |
| `GEMINI_OAUTH_SCOPES` | No | *(default)* | OAuth scopes (Gemini OAuth) |
| `GEMINI_QUOTA_POLICY` | No | *(empty)* | JSON overrides for Gemini local quota simulation (Code Assist only). |

See `.env.example` for all available options.

After changing a WebAuthn variable, recreate the application container. Once
the configuration is valid, explicitly enable Passkey in administrator System
Settings; deployment configuration alone never exposes Passkey login.

> **Note:** The `docker-deploy.sh` script automatically generates `JWT_SECRET`, `TOTP_ENCRYPTION_KEY`, and `POSTGRES_PASSWORD` for you. Keep the generated `TOTP_ENCRYPTION_KEY` stable: saving a Prompt Audit endpoint token without a fixed key is rejected, because it would be unreadable after restart.

### Reverse Proxy and Upstream Egress Hardening

For a reverse-proxy deployment, list only the IP/CIDR of the proxy that connects
directly to Sub2API Plus in `SERVER_TRUSTED_PROXIES`, then keep
`SECURITY_TRUST_FORWARDED_IP_FOR_API_KEY_ACL=false`. This makes API-key ACL and
session client-IP handling use the explicit trusted-proxy chain instead of raw
client headers. A direct deployment should leave `SERVER_TRUSTED_PROXIES` empty.
Use `SERVER_IP_ACCESS_EMERGENCY_ALLOWLIST` only for fixed administrator egress
addresses while repairing a proxy chain; it is not a general bypass list.

URL allowlist is an additional egress control, not a replacement for the
Responses/Gemini upstream path validation in the application. Before enabling
it in production, inventory every upstream hostname and configure the complete
`SECURITY_URL_ALLOWLIST_UPSTREAM_HOSTS` list. A configured list replaces the
built-in defaults. Then set `SECURITY_URL_ALLOWLIST_ENABLED=true`,
`SECURITY_URL_ALLOWLIST_ALLOW_INSECURE_HTTP=false`, and
`SECURITY_URL_ALLOWLIST_ALLOW_PRIVATE_HOSTS=false` unless the deployment has an
intentional, separately reviewed private relay.

### Pricing Release

Model pricing affects billing. It is not refreshed from a mutable repository
branch. With the default configuration, the application first loads a
validated local cache or the bundled repository pricing, then automatically
checks the latest GitHub Release manifest. The manifest binds the Release
version, immutable pricing asset URL, and SHA-256. No pricing key or additional
deployment variable is required.

The sole-maintainer GitHub Release publication boundary is the source of trust;
SHA-256 provides manifest-to-data integrity, not an independent authenticity
guarantee. Keep `github.com`, `release-assets.githubusercontent.com`, and
`objects.githubusercontent.com` in `security.url_allowlist.pricing_hosts`.
Pricing always checks this dedicated list and rejects HTTP, private hosts,
oversized responses, invalid JSON, version rollback, and redirects outside the
list even when the general URL allowlist is disabled. A failed refresh keeps
the last validated cache, or the bundled pricing when no cache exists.

### Content Security Policy

The default CSP permits local, `data:`, `blob:`, and HTTPS images. It does not
permit arbitrary HTTP images. Deployments that know their image CDN origins may
replace the broad `https:` item in `security.csp.policy` with explicit HTTPS
origins. CSP is a deployment boundary and intentionally has no administrator
system-setting control.

Compose enables `no-new-privileges:true` for the application service. Apple
Containers still relies on its entrypoint to drop to the `sub2api` user; it is
not assumed to provide the same runtime hardening flag.

### Easy Migration (Local Directory Version)

When using `docker-compose.local.yml`, all data is stored in local directories, making migration simple:

```bash
# On source server: Stop services and create archive
cd /path/to/deployment
docker compose -f docker-compose.local.yml down
cd ..
tar czf sub2api-complete.tar.gz deployment/

# Transfer to new server
scp sub2api-complete.tar.gz user@new-server:/path/to/destination/

# On new server: Extract and start
tar xzf sub2api-complete.tar.gz
cd deployment/
docker compose -f docker-compose.local.yml up -d
```

Your entire deployment (configuration + data) is migrated!

---

## Gemini OAuth Configuration

Sub2API Plus supports three methods to connect to Gemini:

### Method 1: Code Assist OAuth (Recommended for GCP Users)

**No configuration needed** - always uses the built-in Gemini CLI OAuth client (public).

1. Leave `GEMINI_OAUTH_CLIENT_ID` and `GEMINI_OAUTH_CLIENT_SECRET` empty
2. In the Admin UI, create a Gemini OAuth account and select **"Code Assist"** type
3. Complete the OAuth flow in your browser

> Note: Even if you configure `GEMINI_OAUTH_CLIENT_ID` / `GEMINI_OAUTH_CLIENT_SECRET` for AI Studio OAuth,
> Code Assist OAuth will still use the built-in Gemini CLI client.

**Requirements:**
- Google account with access to Google Cloud Platform
- A GCP project (auto-detected or manually specified)

**How to get Project ID (if auto-detection fails):**
1. Go to [Google Cloud Console](https://console.cloud.google.com/)
2. Click the project dropdown at the top of the page
3. Copy the Project ID (not the project name) from the list
4. Common formats: `my-project-123456` or `cloud-ai-companion-xxxxx`

### Method 2: AI Studio OAuth (For Regular Google Accounts)

Requires your own OAuth client credentials.

**Step 1: Create OAuth Client in Google Cloud Console**

1. Go to [Google Cloud Console - Credentials](https://console.cloud.google.com/apis/credentials)
2. Create a new project or select an existing one
3. **Enable the Generative Language API:**
   - Go to "APIs & Services" → "Library"
   - Search for "Generative Language API"
   - Click "Enable"
4. **Configure OAuth Consent Screen** (if not done):
   - Go to "APIs & Services" → "OAuth consent screen"
   - Choose "External" user type
   - Fill in app name, user support email, developer contact
   - Add scopes: `https://www.googleapis.com/auth/generative-language.retriever` (and optionally `https://www.googleapis.com/auth/cloud-platform`)
   - Add test users (your Google account email)
5. **Create OAuth 2.0 credentials:**
   - Go to "APIs & Services" → "Credentials"
   - Click "Create Credentials" → "OAuth client ID"
   - Application type: **Web application** (or **Desktop app**)
   - Name: e.g., "Sub2API Plus Gemini"
   - Authorized redirect URIs: Add `http://localhost:1455/auth/callback`
6. Copy the **Client ID** and **Client Secret**
7. **⚠️ Publish to Production (IMPORTANT):**
   - Go to "APIs & Services" → "OAuth consent screen"
   - Click "PUBLISH APP" to move from Testing to Production
   - **Testing mode limitations:**
     - Only manually added test users can authenticate (max 100 users)
     - Refresh tokens expire after 7 days
     - Users must be re-added periodically
   - **Production mode:** Any Google user can authenticate, tokens don't expire
   - Note: For sensitive scopes, Google may require verification (demo video, privacy policy)

**Step 2: Configure Environment Variables**

```bash
GEMINI_OAUTH_CLIENT_ID=your-client-id.apps.googleusercontent.com
GEMINI_OAUTH_CLIENT_SECRET=GOCSPX-your-client-secret

# 可选：如需使用 Gemini CLI 内置 OAuth Client（Code Assist / Google One）
# 安全说明：本仓库不会内置该 client_secret，请在运行环境通过环境变量注入。
# GEMINI_CLI_OAUTH_CLIENT_SECRET=GOCSPX-your-built-in-secret
```

**Step 3: Create Account in Admin UI**

1. Create a Gemini OAuth account and select **"AI Studio"** type
2. Complete the OAuth flow
   - After consent, your browser will be redirected to `http://localhost:1455/auth/callback?code=...&state=...`
   - Copy the full callback URL (recommended) or just the `code` and paste it back into the Admin UI

### Method 3: API Key (Simplest)

1. Go to [Google AI Studio](https://aistudio.google.com/app/apikey)
2. Click "Create API key"
3. In Admin UI, create a Gemini **API Key** account
4. Paste your API key (starts with `AIza...`)

### Comparison Table

| Feature | Code Assist OAuth | AI Studio OAuth | API Key |
|---------|-------------------|-----------------|---------|
| Setup Complexity | Easy (no config) | Medium (OAuth client) | Easy |
| GCP Project Required | Yes | No | No |
| Custom OAuth Client | No (built-in) | Yes (required) | N/A |
| Rate Limits | GCP quota | Standard | Standard |
| Best For | GCP developers | Regular users needing OAuth | Quick testing |

---

## Binary Installation

For production servers using systemd.

### One-Line Installation

```bash
curl -sSL https://raw.githubusercontent.com/luckykuang/sub2api-plus/main/deploy/install.sh | sudo bash
```

### Manual Installation

1. Download the latest release from [GitHub Releases](https://github.com/luckykuang/sub2api-plus/releases)
2. Extract and copy the binary to `/opt/sub2api/`
3. Copy `sub2api.service` to `/etc/systemd/system/`
4. Run:
   ```bash
   sudo systemctl daemon-reload
   sudo systemctl enable sub2api
   sudo systemctl start sub2api
   ```
5. Open the Setup Wizard in your browser to complete configuration

### Installer Commands

The streamed form works without keeping a local copy of the script. Use a tag
reported by `list-versions`; the installer accepts only the canonical
`vX.Y.Z+custom.NNN` release format.

```bash
curl -sSL https://raw.githubusercontent.com/LuckyKuang/sub2api-plus/main/deploy/install.sh | sudo bash
```

List available published versions:

```bash
curl -sSL https://raw.githubusercontent.com/LuckyKuang/sub2api-plus/main/deploy/install.sh | bash -s -- list-versions
```

Fresh-install or switch an existing installation to the shown exact version.
Replace the immutable tag with another value reported by `list-versions` when
needed:

```bash
curl -sSL https://raw.githubusercontent.com/LuckyKuang/sub2api-plus/main/deploy/install.sh | sudo bash -s -- install --version 'v0.1.185+custom.001'
```

Roll back an existing binary installation to an earlier published version:

```bash
curl -sSL https://raw.githubusercontent.com/LuckyKuang/sub2api-plus/main/deploy/install.sh | sudo bash -s -- rollback 'v0.1.183+custom.004'
```

Upgrade to the latest release:

```bash
curl -sSL https://raw.githubusercontent.com/LuckyKuang/sub2api-plus/main/deploy/install.sh | sudo bash -s -- upgrade
```

Remove the service and binary while preserving `/etc/sub2api`:

```bash
curl -sSL https://raw.githubusercontent.com/LuckyKuang/sub2api-plus/main/deploy/install.sh | sudo bash -s -- uninstall --yes
```

Also remove `/etc/sub2api`. Back up required configuration first:

```bash
curl -sSL https://raw.githubusercontent.com/LuckyKuang/sub2api-plus/main/deploy/install.sh | sudo bash -s -- uninstall --yes --purge
```

For a downloaded `install.sh`, invoke one operation at a time. For example:

```bash
sudo ./install.sh install --version 'v0.1.185+custom.001'
```

Roll back a downloaded-script installation one operation at a time:

```bash
sudo ./install.sh rollback 'v0.1.183+custom.004'
```

Or uninstall while preserving `/etc/sub2api`:

```bash
sudo ./install.sh uninstall
```

Without `--purge`, uninstall removes the systemd unit, `/opt/sub2api`, and the
service user but preserves `/etc/sub2api`. With `--purge`, it also removes that
configuration directory and any data stored there; this is not reversible.

### Nginx Reverse Proxy

For Nginx on the same host, select `127.0.0.1` as the server listen address
during binary installation. Configure only the Nginx peer in
`server.trusted_proxies`, overwrite forwarded client-IP headers, and preserve
SSE/WebSocket behavior by disabling proxy buffering and excluding
`text/event-stream` from gzip. Start from the complete
[Nginx baseline](EDGE_SECURITY.md#nginx-baseline).

For a complete Cloudflare orange-cloud, same-host Nginx, systemd binary, and
global IP access-control walkthrough, see the
[Chinese Cloudflare IP blocking tutorial](CLOUDFLARE_IP_ACCESS_CONTROL_CN.md).

Current Codex clients send `session-id`; legacy Codex and CRS-compatible clients
may still send `session_id`. Nginx drops underscore headers by default, which
breaks sticky session routing for those clients in multi-account setups. Add
this directive to the Nginx `http` block:

```nginx
underscores_in_headers on;
```

Then validate the complete configuration:

```bash
sudo nginx -t
```

Reload only after validation succeeds:

```bash
sudo systemctl reload nginx
```

### Service Management

```bash
# Start the service
sudo systemctl start sub2api

# Stop the service
sudo systemctl stop sub2api

# Restart the service
sudo systemctl restart sub2api

# Check status
sudo systemctl status sub2api

# View logs
sudo journalctl -u sub2api -f

# Enable auto-start on boot
sudo systemctl enable sub2api
```

### Configuration

#### Server Address and Port

During installation, you will be prompted to configure the server listen address and port. These settings are stored in the systemd service file as environment variables.

To change after installation:

1. Edit the systemd service:
   ```bash
   sudo systemctl edit sub2api
   ```

2. Add or modify:
   ```ini
   [Service]
   Environment=SERVER_HOST=0.0.0.0
   Environment=SERVER_PORT=3000
   ```

3. Reload and restart:
   ```bash
   sudo systemctl daemon-reload
   sudo systemctl restart sub2api
   ```

#### Gemini OAuth Configuration

If you need to use AI Studio OAuth for Gemini accounts, add the OAuth client credentials to the systemd service file:

1. Edit the service file:
   ```bash
   sudo nano /etc/systemd/system/sub2api.service
   ```

2. Add your OAuth credentials in the `[Service]` section (after the existing `Environment=` lines):
   ```ini
   Environment=GEMINI_OAUTH_CLIENT_ID=your-client-id.apps.googleusercontent.com
   Environment=GEMINI_OAUTH_CLIENT_SECRET=GOCSPX-your-client-secret
   ```

   如需使用“内置 Gemini CLI OAuth Client”（Code Assist / Google One），还需要注入：
   ```ini
   Environment=GEMINI_CLI_OAUTH_CLIENT_SECRET=GOCSPX-your-built-in-secret
   ```

3. Reload and restart:
   ```bash
   sudo systemctl daemon-reload
   sudo systemctl restart sub2api
   ```

> **Note:** Code Assist OAuth does not require any configuration - it uses the built-in Gemini CLI client.
> See the [Gemini OAuth Configuration](#gemini-oauth-configuration) section above for detailed setup instructions.

#### Application Configuration

The main config file is at `/etc/sub2api/config.yaml` (created by Setup Wizard).

### Prerequisites

- Linux server (Ubuntu 20.04+, Debian 11+, CentOS 8+, etc.)
- PostgreSQL 14+
- Redis 6+
- systemd

### Directory Structure

```
/opt/sub2api/
├── sub2api              # Main binary
├── sub2api.backup       # Backup (after upgrade)
└── data/                # Runtime data

/etc/sub2api/
└── config.yaml          # Configuration file
```

---

## Troubleshooting

### Docker

For **local directory version**:

```bash
# Check container status
docker compose -f docker-compose.local.yml ps

# View detailed logs
docker compose -f docker-compose.local.yml logs --tail=100 sub2api

# Check database connection
docker compose -f docker-compose.local.yml exec postgres pg_isready

# Check Redis connection
docker compose -f docker-compose.local.yml exec redis redis-cli ping

# Restart all services
docker compose -f docker-compose.local.yml restart

# Check data directories
ls -la data/ postgres_data/ redis_data/
```

For **named volumes version**:

```bash
# Check container status
docker compose ps

# View detailed logs
docker compose logs --tail=100 sub2api

# Check database connection
docker compose exec postgres pg_isready

# Check Redis connection
docker compose exec redis redis-cli ping

# Restart all services
docker compose restart
```

### Binary Install

```bash
# Check service status
sudo systemctl status sub2api

# View recent logs
sudo journalctl -u sub2api -n 50

# Check config file
sudo cat /etc/sub2api/config.yaml

# Check PostgreSQL
sudo systemctl status postgresql

# Check Redis
sudo systemctl status redis
```

### Common Issues

1. **Port already in use**: Change `SERVER_PORT` in `.env` or systemd config
2. **Database connection failed**: Check PostgreSQL is running and credentials are correct
3. **Redis connection failed**: Check Redis is running and password is correct
4. **Permission denied**: Ensure proper file ownership for binary install

---

## TLS Fingerprint Configuration

Sub2API Plus supports TLS fingerprint simulation to make requests appear as if they come from the official Claude CLI (Node.js client).

### Default Behavior

- Built-in `claude_cli_v2` profile simulates Node.js 20.x + OpenSSL 3.x
- JA3 Hash: `1a28e69016765d92e3b381168d68922c`
- JA4: `t13d5911h1_a33745022dd6_1f22a2ca17c4`
- Profile selection: `accountID % profileCount`

### Configuration

```yaml
gateway:
  tls_fingerprint:
    enabled: true  # Global switch
    profiles:
      # Simple profile (uses default cipher suites)
      profile_1:
        name: "Profile 1"

      # Profile with custom cipher suites (use compact array format)
      profile_2:
        name: "Profile 2"
        cipher_suites: [4866, 4867, 4865, 49199, 49195, 49200, 49196]
        curves: [29, 23, 24]
        point_formats: 0

      # Another custom profile
      profile_3:
        name: "Profile 3"
        cipher_suites: [4865, 4866, 4867, 49199, 49200]
        curves: [29, 23, 24, 25]
```

### Profile Fields

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | Display name (required) |
| `cipher_suites` | []uint16 | Cipher suites in decimal. Empty = default |
| `curves` | []uint16 | Elliptic curves in decimal. Empty = default |
| `point_formats` | []uint8 | EC point formats. Empty = default |

### Common Values Reference

**Cipher Suites (TLS 1.3):** `4865` (AES_128_GCM), `4866` (AES_256_GCM), `4867` (CHACHA20)

**Cipher Suites (TLS 1.2):** `49195`, `49196`, `49199`, `49200` (ECDHE variants)

**Curves:** `29` (X25519), `23` (P-256), `24` (P-384), `25` (P-521)
