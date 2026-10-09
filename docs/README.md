# Documentation

README files provide the project overview and quick start. Use the documents
below for detailed configuration and maintenance instructions.

Keep current behavior and operational contracts here. Completed implementation
plans and upstream merge narratives belong in Git history; per-release changes
belong in GitHub Release notes.

## Providers

- [DeepSeek, Kimi, Zhipu and MiniMax official model catalog](CN_PROVIDER_MODELS.md)
- [Claude / Anthropic reset-credit status](providers/CLAUDE.md)
- [Grok / xAI](providers/GROK.md)
- [Sora status and reserved configuration](providers/SORA.md)
- [Antigravity](providers/ANTIGRAVITY.md)
- [DeepSeek empty-mapping whitelist](providers/DEEPSEEK.md)
- [Kimi / Moonshot](providers/KIMI.md)
- [MiniMax coding-plan quota origins](providers/MINIMAX.md)
- [Zhipu / GLM ZCode account link](providers/ZHIPU.md)
- [TypeSafe / Jev native System One](providers/TYPESAFE.md)
- [Cline, Command Code and compatible aggregator contracts](providers/COMPATIBLE_AGGREGATORS.md)

## Protocols and Tasks

- [OpenAI Responses and WebSocket ingress](protocols/OPENAI_RESPONSES.md)
- [Codex client profile restrictions](protocols/CODEX_CLIENT_PROFILES.md)
- [Asynchronous image tasks and Gemini/Vertex batches](ASYNC_IMAGE_TASKS.md)
- [Usage timing and completion semantics](USAGE_TIMING.md)

## Access, Pricing and Administration

- [Authentication and Passkeys](AUTHENTICATION.md)
- [Security audit content coverage](SECURITY_AUDIT_CONTENT_COVERAGE.md)
- [Model Plaza visibility](MODEL_PLAZA.md)
- [Available-channel model catalog](AVAILABLE_CHANNELS.md)
- [Channel pricing](CHANNEL_PRICING.md)
- [Composite groups](COMPOSITE_GROUPS.md)
- [Administrator user support view](USER_SUPPORT_VIEW.md)
- [Payment integration](PAYMENT.md)
- [Payment integration — Chinese](PAYMENT_CN.md)
- [Admin payment integration API](ADMIN_PAYMENT_INTEGRATION_API.md)

## Deployment and Operations

- [Outbound client identity and account inheritance](OUTBOUND_IDENTITY.md)
- [Deployment guide](../deploy/README.md)
- [Local deployment and image lifecycle](../deploy/DEPLOYMENT_LIFECYCLE.md)
- [Docker](../deploy/DOCKER.md)
- [Apple container](../deploy/APPLE_CONTAINER.md)
- [Edge and ingress security](../deploy/EDGE_SECURITY.md)
- [datamanagementd](../deploy/DATAMANAGEMENTD_CN.md)
- [Client disconnect risk control](CLIENT_DISCONNECT_RISK_CONTROL.md)
- [Channel Monitor V1/V2/V3](CHANNEL_MONITOR.md)

## Development and Maintenance

- [Contributing](../CONTRIBUTING.md)
- [Release process](RELEASING.md)
- [Upstream mapping](../UPSTREAM.md)
- [Database migrations and upgrade prerequisites](../backend/migrations/README.md)
- [Plugin development](PLUGIN_DEVELOPMENT.md)
- [Plugin host API](../backend/pkg/pluginapi/README.md)
