<div align="center">

<img src="assets/logo.svg" alt="Sub2API Plus Logo" width="128" />

# Sub2API Plus

[![CI](https://github.com/LuckyKuang/sub2api-plus/actions/workflows/backend-ci.yml/badge.svg)](https://github.com/LuckyKuang/sub2api-plus/actions/workflows/backend-ci.yml)
[![License](https://img.shields.io/badge/license-LGPL--3.0--or--later-blue.svg)](LICENSE)

**サブスクリプションクォータ配分向け AI API ゲートウェイ**

[English](README.md) | [中文](README_CN.md) | 日本語

</div>

<!-- readme-section:notice -->
## 重要なお知らせ

Sub2API Plus は Sub2API を基に独立してコミュニティ管理されるフォークです。
上流の公式リリースではなく、提携、承認、サポート、商標利用許諾を意味しません。

- ゲートウェイ経由のサブスクリプション利用は、プロバイダー規約と競合する
  可能性があります。デプロイ前に該当規約を確認してください。
- 法令、プライバシー、セキュリティ、運用上の責任はデプロイ主体が負います。
- 本プロジェクトは LGPL-3.0-or-later に基づき、無保証で提供されます。

<!-- readme-section:overview -->
## 概要

Sub2API Plus は、プラットフォーム発行の API キーを通じて対応 AI
プロバイダーへのアクセスを配分・管理します。認証、課金、アカウント
スケジューリング、クォータ制御、監査、リクエスト転送を提供します。

<!-- readme-section:features -->
## 機能

- 複数の OAuth/API キーアカウントタイプ
- ユーザー API キーとグループ管理
- トークン単位の使用量追跡と課金
- アカウントスケジューリング、フェイルオーバー、セッション固定
- クォータ、サブスクリプション、引換コード、決済連携
- OpenAI、Claude、Gemini 互換ゲートウェイ
- 運用監視、監査、セキュリティ制御
- 個人・社内利用向けのオプションのシンプルモード

シンプルモードは `RUN_MODE=simple` を使用します。本番環境では
`SIMPLE_MODE_CONFIRM=true` も必要です。

<!-- readme-section:quick-start -->
## クイックスタート

Linux バイナリインストール：

```bash
curl -sSL https://raw.githubusercontent.com/LuckyKuang/sub2api-plus/main/deploy/install.sh | sudo bash
```

インストール後、`http://YOUR_SERVER_IP:8080` を開いてセットアップウィザードを
完了してください。

本番公開前にデプロイおよびエッジセキュリティ文書を確認してください。

<!-- readme-section:deployment -->
## デプロイ方法

| 方法 | ドキュメント |
| --- | --- |
| Linux インストールスクリプト／バイナリ | [デプロイガイド](deploy/README.md) |
| Docker Compose | [Docker ガイド](deploy/DOCKER.md) |
| macOS Apple container | [Apple container ガイド](deploy/APPLE_CONTAINER.md) |
| エッジプロキシと信頼済みクライアント IP | [エッジセキュリティ](deploy/EDGE_SECURITY.md) |
| オプションの datamanagementd | [datamanagementd ガイド](deploy/DATAMANAGEMENTD_CN.md) |

完全な設定例は
[`deploy/config.example.yaml`](deploy/config.example.yaml) を参照してください。

<!-- readme-section:providers -->
<!-- readme-capabilities:openai,anthropic,gemini,antigravity,grok,async-images,sora-unavailable -->
## プロバイダーと機能

| プロバイダー／機能 | 概要 |
| --- | --- |
| OpenAI / Codex | OpenAI 互換リクエスト、Responses、任意のクライアント WebSocket |
| Anthropic / Claude | Claude Messages 互換ゲートウェイ |
| Google Gemini | Gemini 互換通信と対応 OAuth/API キーアカウント |
| Antigravity | Claude/Gemini 専用ルートと任意のハイブリッドスケジューリング |
| Grok / xAI | OAuth サブスクリプションおよび API キーアカウント |
| 非同期画像タスク | 長時間の画像生成・編集を送信してポーリング |
| Sora | 一時的に利用不可。本番環境では依存しないでください |

詳細：

- [Grok / xAI](docs/providers/GROK.md)
- [Antigravity](docs/providers/ANTIGRAVITY.md)
- [Sora ステータス](docs/providers/SORA.md)
- [OpenAI Responses と WebSocket 入口](docs/protocols/OPENAI_RESPONSES.md)
- [非同期画像タスク](docs/ASYNC_IMAGE_TASKS.md)

<!-- readme-section:release-tags -->
<!-- readme-release-format:vX.Y.Z+custom.NNN|vX.Y.Z-custom.NNN -->
## リリースとイメージタグ

カスタムリリースは次の形式を使用します：

```text
Git/GitHub: vX.Y.Z+custom.NNN
アプリ:      X.Y.Z+custom.NNN
GHCR:       ghcr.io/luckykuang/sub2api-plus:vX.Y.Z-custom.NNN
```

再現可能な本番デプロイでは不変の GHCR バージョンタグを固定してください。
`latest` は可変の便宜タグです。上流マッピングは
[UPSTREAM.md](UPSTREAM.md)、メンテナー向け規則は
[リリース手順](docs/RELEASING.md) を参照してください。

<!-- readme-section:documentation -->
## ドキュメント

- [ドキュメント索引](docs/README.md)
- [デプロイ](deploy/README.md)
- [開発とコントリビューション](CONTRIBUTING.md)
- [リリース手順](docs/RELEASING.md)
- [上流マッピング](UPSTREAM.md)
- [セキュリティポリシー](SECURITY.md)

<!-- readme-section:license -->
## ライセンス

[GNU Lesser General Public License v3.0](LICENSE) またはそれ以降の
バージョンで提供され、上流の著作権およびライセンス表示を保持します。

元の上流作品：Copyright (c) 2026 Wesley Liddick
Sub2API Plus の変更：Copyright (c) 2026 LuckyKuang
