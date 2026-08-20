<div align="center">

<img src="assets/logo.svg" alt="Sub2API Plus Logo" width="128" />

# Sub2API Plus

[![CI](https://github.com/LuckyKuang/sub2api-plus/actions/workflows/backend-ci.yml/badge.svg)](https://github.com/LuckyKuang/sub2api-plus/actions/workflows/backend-ci.yml)
[![License](https://img.shields.io/badge/license-LGPL--3.0--or--later-blue.svg)](LICENSE)

**AI API gateway for subscription quota distribution**

English | [中文](README_CN.md) | [日本語](README_JA.md)

</div>

<!-- readme-section:notice -->
## Important Notice

Sub2API Plus is an independently maintained community fork of Sub2API. It is
not an official upstream release and does not imply upstream affiliation,
endorsement, support, or trademark permission.

- Using subscription accounts through a gateway may conflict with provider
  terms. Review the applicable agreements before deployment.
- Deployers are responsible for legal, privacy, security, and operational
  compliance.
- The project is provided without warranty under LGPL-3.0-or-later.

<!-- readme-section:overview -->
## Overview

Sub2API Plus distributes and manages access to supported AI providers through
platform-issued API keys. It provides authentication, billing, account
scheduling, quota controls, auditing, and request forwarding.

<!-- readme-section:features -->
## Features

- Multiple OAuth and API-key account types
- User API-key and group management
- Token-level usage and billing
- Account scheduling, failover, and session affinity
- Quotas, subscriptions, redemption codes, and payment integrations
- OpenAI-, Claude-, and Gemini-compatible gateway interfaces
- Operational monitoring, audit, and security controls
- Optional simple mode for individual or internal deployments

Simple mode uses `RUN_MODE=simple`. Production also requires
`SIMPLE_MODE_CONFIRM=true`.

<!-- readme-section:quick-start -->
## Quick Start

### Linux binary lifecycle

The installer supports fresh installation, version pinning or rollback, and
uninstallation. Published binary tags use the immutable
`vX.Y.Z+custom.NNN` format.

```bash
curl -sSL https://raw.githubusercontent.com/LuckyKuang/sub2api-plus/main/deploy/install.sh | sudo bash
```

List published versions:

```bash
curl -sSL https://raw.githubusercontent.com/LuckyKuang/sub2api-plus/main/deploy/install.sh | bash -s -- list-versions
```

Install or switch to an exact published version. The command below is directly
usable; replace its immutable tag with another value returned by
`list-versions` when needed:

```bash
curl -sSL https://raw.githubusercontent.com/LuckyKuang/sub2api-plus/main/deploy/install.sh | sudo bash -s -- install --version 'v0.1.178+custom.001'
```

Roll back an existing binary installation to an earlier published version:

```bash
curl -sSL https://raw.githubusercontent.com/LuckyKuang/sub2api-plus/main/deploy/install.sh | sudo bash -s -- rollback 'v0.1.177+custom.002'
```

Remove the service and binary while preserving `/etc/sub2api`:

```bash
curl -sSL https://raw.githubusercontent.com/LuckyKuang/sub2api-plus/main/deploy/install.sh | sudo bash -s -- uninstall --yes
```

Also remove `/etc/sub2api`. Review backups first; this cannot be undone:

```bash
curl -sSL https://raw.githubusercontent.com/LuckyKuang/sub2api-plus/main/deploy/install.sh | sudo bash -s -- uninstall --yes --purge
```

Then open `http://YOUR_SERVER_IP:8080` and complete the setup wizard.

### Nginx reverse-proxy notes

When Nginx runs on the same host, bind Sub2API to `127.0.0.1` and configure
only the Nginx peer under `server.trusted_proxies`. Nginx must overwrite, not
append, client-IP headers. Preserve streaming and WebSocket traffic with HTTP/1.1
upgrade headers, disabled proxy buffering, long read/send timeouts, and no gzip
for `text/event-stream`.

For Codex CLI or CRS-compatible clients, add this directive to the Nginx
`http` block:

```nginx
underscores_in_headers on;
```

Current Codex clients use the hyphenated `session-id`; legacy Codex/CRS-compatible
clients may still send `session_id`. Nginx drops underscore headers by default,
so keep this directive to preserve sticky session routing for those clients.
Validate the complete configuration before reloading:

```bash
sudo nginx -t
```

After the configuration test succeeds, reload Nginx:

```bash
sudo systemctl reload nginx
```

Use the complete [Nginx baseline and trusted-proxy guidance](deploy/EDGE_SECURITY.md)
before exposing the service publicly.

<!-- readme-section:deployment -->
## Deployment Options

| Method | Documentation |
| --- | --- |
| Linux installation script or binary | [Deployment guide](deploy/README.md) |
| Docker Compose | [Docker guide](deploy/DOCKER.md) |
| Apple container on macOS | [Apple container guide](deploy/APPLE_CONTAINER.md) |
| Edge proxy and trusted client IPs | [Edge security](deploy/EDGE_SECURITY.md) |
| Optional datamanagementd service | [datamanagementd guide](deploy/DATAMANAGEMENTD_CN.md) |

The full example configuration is
[`deploy/config.example.yaml`](deploy/config.example.yaml).

<!-- readme-section:providers -->
<!-- readme-capabilities:openai,anthropic,gemini,antigravity,grok,async-images,sora-unavailable -->
## Provider Support

| Provider or capability | Notes |
| --- | --- |
| OpenAI / Codex | OpenAI-compatible requests, Responses, and optional client WebSocket ingress |
| Anthropic / Claude | Claude Messages-compatible gateway traffic |
| Google Gemini | Gemini-compatible traffic and supported OAuth/API-key accounts |
| Antigravity | Dedicated and optional hybrid Claude/Gemini routing |
| Grok / xAI | OAuth subscription and API-key accounts |
| Asynchronous images | Submit and poll long-running image generation/edit tasks |
| Sora | Temporarily unavailable; do not depend on it in production |

Details:

- [Grok / xAI](docs/providers/GROK.md)
- [Antigravity](docs/providers/ANTIGRAVITY.md)
- [Sora status](docs/providers/SORA.md)
- [OpenAI Responses and WebSocket ingress](docs/protocols/OPENAI_RESPONSES.md)
- [Asynchronous image tasks](docs/ASYNC_IMAGE_TASKS.md)

<!-- readme-section:release-tags -->
<!-- readme-release-format:vX.Y.Z+custom.NNN|vX.Y.Z-custom.NNN -->
## Release and Image Tags

Custom releases use the following formats:

```text
Git/GitHub: vX.Y.Z+custom.NNN
Application: X.Y.Z+custom.NNN
GHCR:        ghcr.io/luckykuang/sub2api-plus:vX.Y.Z-custom.NNN
```

Pin the immutable GHCR version tag for reproducible production deployments.
`latest` is a moving convenience tag. See [UPSTREAM.md](UPSTREAM.md) for the
upstream mapping and [the release process](docs/RELEASING.md) for maintainer
rules.

<!-- readme-section:documentation -->
## Documentation

- [Documentation index](docs/README.md)
- [Deployment](deploy/README.md)
- [Development and contributions](CONTRIBUTING.md)
- [Release process](docs/RELEASING.md)
- [Upstream mapping](UPSTREAM.md)
- [Security policy](SECURITY.md)

<!-- readme-section:license -->
## License

Licensed under the [GNU Lesser General Public License v3.0](LICENSE) or later.
Original upstream copyright and license notices are retained.

Original upstream work: Copyright (c) 2026 Wesley Liddick

Sub2API Plus modifications: Copyright (c) 2026 LuckyKuang
