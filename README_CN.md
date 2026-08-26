<div align="center">

<img src="assets/logo.svg" alt="Sub2API Plus Logo" width="128" />

# Sub2API Plus

[![CI](https://github.com/LuckyKuang/sub2api-plus/actions/workflows/backend-ci.yml/badge.svg)](https://github.com/LuckyKuang/sub2api-plus/actions/workflows/backend-ci.yml)
[![License](https://img.shields.io/badge/license-LGPL--3.0--or--later-blue.svg)](LICENSE)

**用于订阅配额分发的 AI API 网关**

[English](README.md) | 中文 | [日本語](README_JA.md)

</div>

<!-- readme-section:notice -->
## 重要提醒

Sub2API Plus 是基于 Sub2API 的独立社区维护分支，不是上游官方版本，也不表示
获得上游的合作、背书、支持或商标授权。

- 通过网关使用订阅账户可能与服务商条款冲突，部署前请自行核对相关协议。
- 部署者应自行承担法律、隐私、安全和运营合规责任。
- 本项目依据 LGPL-3.0-or-later 提供，不作任何担保。

<!-- readme-section:overview -->
## 项目概述

Sub2API Plus 通过平台签发的 API Key 分发和管理多个 AI 服务商账户，提供鉴权、
计费、账号调度、额度控制、审计和请求转发能力。

<!-- readme-section:features -->
## 核心功能

- 支持多种 OAuth 和 API Key 账户
- 用户 API Key 与分组管理
- Token 级用量统计和计费
- 账号调度、故障转移和会话粘性
- 额度、订阅、兑换码与支付集成
- OpenAI、Claude 和 Gemini 兼容网关
- 运维监控、审计和安全控制
- 面向个人或内部部署的可选简易模式

简易模式设置 `RUN_MODE=simple`；生产环境还必须设置
`SIMPLE_MODE_CONFIRM=true`。

<!-- readme-section:quick-start -->
## 快速开始

### Linux 二进制安装、指定版本与卸载

安装脚本支持全新安装、固定版本或回退，以及卸载。已发布的二进制版本使用不可变的
`vX.Y.Z+custom.NNN` 标签格式。

```bash
curl -sSL https://raw.githubusercontent.com/LuckyKuang/sub2api-plus/main/deploy/install.sh | sudo bash
```

列出已发布版本：

```bash
curl -sSL https://raw.githubusercontent.com/LuckyKuang/sub2api-plus/main/deploy/install.sh | bash -s -- list-versions
```

全新安装或切换到指定版本。下面的命令可以直接执行；需要其他版本时，将其中的不可变
标签替换为 `list-versions` 返回的标签：

```bash
curl -sSL https://raw.githubusercontent.com/LuckyKuang/sub2api-plus/main/deploy/install.sh | sudo bash -s -- install --version 'v0.1.183+custom.002'
```

将现有二进制安装回退到较早的已发布版本：

```bash
curl -sSL https://raw.githubusercontent.com/LuckyKuang/sub2api-plus/main/deploy/install.sh | sudo bash -s -- rollback 'v0.1.183+custom.001'
```

卸载服务和二进制，保留 `/etc/sub2api`：

```bash
curl -sSL https://raw.githubusercontent.com/LuckyKuang/sub2api-plus/main/deploy/install.sh | sudo bash -s -- uninstall --yes
```

同时删除 `/etc/sub2api`；请先确认备份，此操作不可恢复：

```bash
curl -sSL https://raw.githubusercontent.com/LuckyKuang/sub2api-plus/main/deploy/install.sh | sudo bash -s -- uninstall --yes --purge
```

安装后访问 `http://你的服务器IP:8080`，完成初始化向导。

### Nginx 反向代理注意事项

Nginx 与 Sub2API 同机部署时，应让 Sub2API 只监听 `127.0.0.1`，并在
`server.trusted_proxies` 中只配置 Nginx 的直连地址。Nginx 必须覆盖客户端 IP
请求头，不能追加不可信的传入值。SSE 与 WebSocket 还需要 HTTP/1.1 升级请求头、
关闭代理缓冲、足够长的读写超时，并确保 `text/event-stream` 不启用 gzip。

使用 Codex CLI 或 CRS 兼容客户端时，必须在 Nginx 的 `http` 块中加入：

```nginx
underscores_in_headers on;
```

当前 Codex 客户端使用带连字符的 `session-id`，旧版 Codex/CRS 兼容客户端仍可能发送
`session_id`。Nginx 默认会丢弃带下划线的请求头，因此仍需保留该配置，避免这些客户端
在多账号场景下的粘性会话路由失效。重载前先检查完整配置：

```bash
sudo nginx -t
```

检查通过后再重载 Nginx：

```bash
sudo systemctl reload nginx
```

生产环境对外开放前，请使用完整的
[Nginx 基线与可信代理指南](deploy/EDGE_SECURITY.md)。

<!-- readme-section:deployment -->
## 部署方式

| 方式 | 文档 |
| --- | --- |
| Linux 安装脚本或二进制 | [部署指南](deploy/README.md) |
| Docker Compose | [Docker 指南](deploy/DOCKER.md) |
| macOS Apple container | [Apple container 指南](deploy/APPLE_CONTAINER.md) |
| 边缘代理与可信客户端 IP | [边缘安全](deploy/EDGE_SECURITY.md) |
| 可选 datamanagementd 服务 | [datamanagementd 指南](deploy/DATAMANAGEMENTD_CN.md) |

完整配置示例见
[`deploy/config.example.yaml`](deploy/config.example.yaml)。

<!-- readme-section:providers -->
<!-- readme-capabilities:openai,anthropic,gemini,antigravity,grok,async-images,sora-unavailable -->
## 服务商与能力支持

| 服务商或能力 | 说明 |
| --- | --- |
| OpenAI / Codex | OpenAI 兼容请求、Responses 和可选客户端 WebSocket 入口 |
| Anthropic / Claude | Claude Messages 兼容网关 |
| Google Gemini | Gemini 兼容请求及支持的 OAuth/API Key 账户 |
| Antigravity | Claude/Gemini 专用路由和可选混合调度 |
| Grok / xAI | OAuth 订阅账户和 API Key 账户 |
| 异步图片任务 | 提交和轮询长时间运行的图片生成/编辑任务 |
| Sora | 暂不可用，请勿在生产环境依赖 |

详细文档：

- [Grok / xAI](docs/providers/GROK.md)
- [Antigravity](docs/providers/ANTIGRAVITY.md)
- [Sora 状态](docs/providers/SORA.md)
- [OpenAI Responses 与 WebSocket 入口](docs/protocols/OPENAI_RESPONSES.md)
- [异步图片任务](docs/ASYNC_IMAGE_TASKS.md)

<!-- readme-section:release-tags -->
<!-- readme-release-format:vX.Y.Z+custom.NNN|vX.Y.Z-custom.NNN -->
## 版本与镜像标签

自定义版本使用以下格式：

```text
Git/GitHub: vX.Y.Z+custom.NNN
应用版本:    X.Y.Z+custom.NNN
GHCR:       ghcr.io/luckykuang/sub2api-plus:vX.Y.Z-custom.NNN
```

生产环境建议固定不可变的 GHCR 版本标签；`latest` 只是滚动标签。上游映射见
[UPSTREAM.md](UPSTREAM.md)，维护者发布规则见
[发布流程](docs/RELEASING.md)。

<!-- readme-section:documentation -->
## 文档

- [文档索引](docs/README.md)
- [部署指南](deploy/README.md)
- [开发与贡献](CONTRIBUTING.md)
- [发布流程](docs/RELEASING.md)
- [上游映射](UPSTREAM.md)
- [安全策略](SECURITY.md)

<!-- readme-section:license -->
## 许可证

本项目依据 [GNU 宽通用公共许可证 v3.0](LICENSE) 或更高版本授权，并保留上游
版权和许可证声明。

原始上游作品：Copyright (c) 2026 Wesley Liddick

Sub2API Plus 修改：Copyright (c) 2026 LuckyKuang
