# Sora

> Sora-related features are temporarily unavailable because of unresolved
> upstream integration and media-delivery issues. Do not depend on Sora in
> production until a release explicitly restores support.

Existing `gateway.sora_*` keys are reserved and may not take effect while the
provider is unavailable.

## Reserved Media Access Configuration

When support is restored, `gateway.sora_media_signing_key` and
`gateway.sora_media_signed_url_ttl_seconds` can enable expiring signed media
URLs:

```yaml
gateway:
  sora_media_require_api_key: false
  sora_media_signing_key: "replace-with-a-secret"
  sora_media_signed_url_ttl_seconds: 900
```

- `/sora/media` can require a Sub2API Plus API key.
- `/sora/media-signed` uses a path/query signature and expiry.
- Without a signing key, the signed-media endpoint is unavailable.

Do not enable or document these settings as production-ready until the provider
status changes in a published release.
