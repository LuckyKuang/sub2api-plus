# Asynchronous and Batch Image Tasks

Asynchronous image tasks let clients submit long-running OpenAI-compatible image requests without keeping one HTTP connection open. This avoids proxy/CDN response timeouts such as Cloudflare 524 while preserving the existing image routing, billing, moderation, concurrency, and failover behavior.

OpenAI/Grok tasks use `/images/tasks` with S3-compatible output storage. Gemini
and Vertex provider batches use `/images/batches`, described in
[Gemini and Vertex batches](#gemini-and-vertex-batches) below, with separate
feature gates, provider storage and billing settlement.

## Endpoints

The authenticated gateway exposes both `/v1` paths and their existing no-prefix aliases:

```text
POST /v1/images/generations/async
POST /v1/images/edits/async
GET  /v1/images/tasks
GET  /v1/images/tasks/{task_id}
GET  /v1/images/tasks/{task_id}/download
DELETE /v1/images/tasks/{task_id}
```

Every endpoint also has the equivalent no-prefix `/images/...` alias.

Only OpenAI and Grok groups are supported. Requests use the same JSON or multipart payload as the corresponding synchronous endpoint. Streaming image requests are rejected because a polled task returns one final JSON result.

## Enabling the feature (object storage)

Asynchronous image tasks are **disabled by default** and gated on object storage. When the switch is off — or the S3 credentials are incomplete — the submission endpoints return `404` and never create a task or write to Redis. This is deliberate: without offloading, large `b64_json` results (several MB each, e.g. `gpt-image-1`) would accumulate in Redis and exhaust its memory. List, detail, download, and failed-task deletion remain available for existing tasks after the switch is turned off; ZIP downloads still require the previously configured object-storage credentials to remain complete.

### From the admin UI (recommended)

**Admin → Backup → Async image object storage.** Saving the form takes effect immediately — the object-storage client is rebuilt on the next request, so there is no container restart.

Because the async image storage and the database backup share one S3 client, the form defaults to **reusing the backup S3 configuration**: it borrows the endpoint, region and credentials already configured above and keeps only its own bucket, prefix, and date-path choice, so backups stay under `backups/` while images go to `images/`. Leave the bucket empty to use the backup bucket as well. Untick the box to point images at a completely separate account.

Both prefix fields have an independent **Append date path** switch. The stored value remains a stable base such as `images/`; when enabled, each new object is written under a concrete server-timezone directory such as `images/2026/08/17/`. The server resolves the date for every new object, so no scheduled configuration update is needed at midnight. Existing objects are not moved.

Saving requires step-up 2FA when that gate is enabled, for the same reason the backup S3 form does: changing the target redirects generated content to another account.

Turning the switch off stops new submissions but keeps already-accepted tasks pollable. When the saved storage credentials remain complete, in-flight results are still offloaded and existing completed tasks remain downloadable.

### From the config file

The admin setting takes precedence. When nothing has ever been saved there, the `image_storage` block in `config.yaml` is used instead, so deployments that enabled the feature before the admin UI existed keep working untouched.

Configure an S3-compatible object store (AWS S3, Cloudflare R2, Aliyun OSS, MinIO, …) in `config.yaml` (all keys also accept the `IMAGE_STORAGE_*` environment overrides):

```yaml
image_storage:
  enabled: true
  endpoint: "https://<account_id>.r2.cloudflarestorage.com"  # AWS 官方可留空
  region: "auto"
  bucket: "my-images"
  access_key_id: "..."
  secret_access_key: "..."
  prefix: "images/"
  append_date_path: false          # true → images/yyyy/MM/dd/ in the server timezone
  force_path_style: false          # MinIO/path-style buckets set true
  public_base_url: ""              # set to return public_base_url/key直链; empty → presigned URL
  presign_expiry_hours: 24         # presigned link TTL when public_base_url is empty
  max_download_bytes: 33554432     # cap when re-hosting an upstream image URL (32MB)
```

When a task completes, each generated image is uploaded to the bucket and the result is rewritten to a compact form: `data[].url` points at the stored object (a permanent `public_base_url/key` link, or a time-limited presigned URL) and `b64_json` is removed. Only this small JSON and the private exact object keys needed for ZIP downloads are stored in Redis. The object keys are not exposed in the public task response. If an upload fails, the task is marked `failed` rather than persisting the raw base64.

To support a different vendor beyond the S3-compatible client, implement the `service.ImageStorage` interface (`Save(ctx, key, contentType, data) (url, error)`) and provide it in place of the S3 implementation.

### Troubleshooting: the endpoints return 404 after enabling

`404 async image tasks are not enabled` means `image_storage` did not resolve to a complete configuration, so the feature stayed off. The route exists either way — the 404 comes from the handler, not from an unregistered path, which makes it easy to mistake for a missing build.

Check the startup log for:

```text
WARN image_storage.enabled is true but object storage is not fully configured; async image tasks are disabled  missing_keys=[...]
```

`missing_keys` names exactly which credentials were empty when the config was loaded.

Note that releases **before v0.1.161 silently dropped `IMAGE_STORAGE_ENDPOINT`, `_BUCKET`, `_ACCESS_KEY_ID`, `_SECRET_ACCESS_KEY` and `_PUBLIC_BASE_URL`** when they were supplied only through the environment: those keys had no registered default, and viper cannot see an environment variable for a key it does not already know about. Deployments driven purely by `environment:` — which is what `deploy/docker-compose.yml` does by default — therefore reported `enabled: true` with empty credentials and 404'd on every async call. On an affected release the workaround is to also place the `image_storage` block in `/app/data/config.yaml` (copy it from `deploy/config.example.yaml`); once the keys exist in the file, the environment overrides apply normally.

Two further causes of a 404 that are unrelated to storage: the API key's group must be on the **OpenAI or Grok** platform (any other platform, or a key with no group at all, yields `Images API is not supported for this platform`), and a task may only be polled with the **same API key that submitted it** — polling with a different key of the same user returns `image task not found` by design.

## Submit a task

```bash
curl -i https://api.example.com/v1/images/generations/async \
  -H 'Authorization: Bearer sk-...' \
  -H 'Content-Type: application/json' \
  -d '{
    "model": "gpt-image-1",
    "prompt": "A lighthouse during a winter storm",
    "size": "1536x1024"
  }'
```

The server stores the initial task in Redis and responds with `202 Accepted`:

```json
{
  "id": "imgtask_0123456789abcdef",
  "task_id": "imgtask_0123456789abcdef",
  "object": "image.generation.task",
  "status": "processing",
  "created_at": 1784092800,
  "expires_at": 1784179200,
  "poll_url": "/v1/images/tasks/imgtask_0123456789abcdef"
}
```

`Location` contains the polling path and `Retry-After: 3` provides the recommended polling interval.

## Poll a task

Use the same API key that submitted the task:

```bash
curl https://api.example.com/v1/images/tasks/imgtask_0123456789abcdef \
  -H 'Authorization: Bearer sk-...'
```

While work is in progress:

```json
{
  "id": "imgtask_0123456789abcdef",
  "task_id": "imgtask_0123456789abcdef",
  "object": "image.generation.task",
  "status": "processing",
  "created_at": 1784092800,
  "expires_at": 1784179200
}
```

On success, `result` mirrors the synchronous image API body, except each image has been offloaded to object storage: `data[].url` points at the stored object and `b64_json` is stripped (so both URL and base64 upstream formats end up as compact stored links):

```json
{
  "id": "imgtask_0123456789abcdef",
  "task_id": "imgtask_0123456789abcdef",
  "object": "image.generation.task",
  "status": "completed",
  "http_status": 200,
  "image_url": "https://...",
  "result": {
    "created": 1784092923,
    "data": [{"url": "https://..."}]
  },
  "created_at": 1784092800,
  "completed_at": 1784092923,
  "expires_at": 1784179323
}
```

For URL responses, `image_url` mirrors the first `data[].url` for simple clients. On failure, the task reaches `failed` and exposes the original OpenAI-compatible error object where available:

```json
{
  "id": "imgtask_0123456789abcdef",
  "task_id": "imgtask_0123456789abcdef",
  "object": "image.generation.task",
  "requested_images": 2,
  "actual_images": 0,
  "status": "failed",
  "http_status": 502,
  "error": {
    "type": "api_error",
    "message": "Upstream request failed"
  },
  "created_at": 1784092800,
  "completed_at": 1784092923,
  "expires_at": 1784179323
}
```

All submit and poll responses include `Cache-Control: no-store`, preventing a CDN from caching the `processing` state. `requested_images` records the accepted `n` value and `actual_images` records the number of completed result images. A short upstream result remains completed and retains every successful image; the user console shows the mismatch instead of silently presenting it as a fulfilled count. Tasks and results expire 24 hours after their latest state update. A task executes for at most 30 minutes.

Task ownership is scoped to both user and API key. Unknown task IDs and IDs owned by another key both return `404`, avoiding task-existence disclosure. Polling remains available when the completed generation used the key's remaining balance; normal authentication, disabled-key, user, IP, and group checks still apply.

Completed ZIP downloads first use Redis execution state and fall back to the
owner-scoped PostgreSQL history row when that execution key has expired. The
history stores the exact object keys as private server metadata; public and
administrator task responses never expose those keys. The fallback preserves
the original user plus API-key ownership check and does not reconstruct object
paths from the currently configured prefix.

## Delete a failed task

The user task list shows a delete action only for failed rows. The same action is available through the API with the API key that created the task:

```bash
curl -i -X DELETE \
  https://api.example.com/v1/images/tasks/imgtask_0123456789abcdef \
  -H 'Authorization: Bearer sk-...'
```

A successful deletion returns `204 No Content` and removes both the PostgreSQL `async_image_tasks` history row and the Redis `image_task:{task_id}` execution key. An already expired or absent Redis key still counts as success. The service atomically removes Redis first only while its current status is still `failed`; if Redis is unavailable, it returns `503` and leaves the history row intact so the operation can be retried.

Only `failed` tasks can be deleted. Deleting an owned `processing` or `completed` task returns `409`. A missing task, a task owned by another user, or a task created by another API key of the same user all return the same `404` response. List, detail, download, and deletion skip subscription, quota, and expiration enforcement so users can manage existing task data after billable access ends. Credential parsing, hard-disabled-key, user-state, IP, group, and ownership checks still apply.

The task-page API Key filter continues to show every non-disabled key, including expired and quota-exhausted keys and keys whose group or platform changed after task creation, so their owner-scoped history remains manageable. The new-task form remains stricter and offers only active OpenAI/Grok keys whose current group allows image generation.

Deleting a task record never scans or deletes S3-compatible object storage. In particular, no object key is reconstructed from the active prefix. Object lifecycle and cleanup remain the responsibility of the configured bucket policy.

## Gemini and Vertex batches

Batch generation supports `gemini_api` (Gemini API-key accounts and JSONL file
batches) and `vertex` (Gemini service-account accounts, Vertex
`BatchPredictionJob` and managed GCS JSONL). PostgreSQL is authoritative;
Redis supplies queue wakeups, retries, locks and download limits. Provider
filenames, job names, storage paths, signed URLs and credentials are private.
Image and ZIP downloads are proxied through Sub2API Plus.

### Batch API

```text
POST   /v1/images/batches
GET    /v1/images/batches
GET    /v1/images/batches/models
GET    /v1/images/batches/{id}
GET    /v1/images/batches/{id}/items
GET    /v1/images/batches/{id}/items/{custom_id}/content
GET    /v1/images/batches/{id}/download
POST   /v1/images/batches/{id}/cancel
DELETE /v1/images/batches/{id}
DELETE /v1/images/batches/{id}/outputs
```

Authenticate with a Sub2API Plus API key. Every job read, item, download,
cancellation and deletion is scoped to both its user and submitting API key.
The model list uses eligible accounts in the key's group, available pricing and
the group model allowlist; listing a model does not reserve provider capacity.

Submit JSON, optionally with `Idempotency-Key`:

```json
{
  "model": "gemini-2.5-flash-image",
  "provider": "gemini_api",
  "items": [
    {"custom_id": "cover", "prompt": "A winter lighthouse", "output_count": 2}
  ],
  "image_size": "1K",
  "response_mime_type": "image/png"
}
```

Submission returns the public batch object with HTTP 200. It includes `id`,
`object: image.batch`, `status`, model/provider, item/success/failure counts,
estimated/actual cost and lifecycle timestamps. Reusing an idempotency key with
the same normalized payload returns its job; a changed payload is a conflict.
Optional `task_name` labels the job; `parent_batch_id` links an owner-scoped
retry to its original batch. Lists support `status`, `task_name`, `downloaded`,
`from`, `to`, `limit` and `cursor`; item lists support `status`, `limit` and
`cursor`. Item content accepts a zero-based `image_index`.

`output_count` defaults to 1 and expands each item into separate provider JSONL
requests with IDs such as `cover_01` and `cover_02`. These counts and limits are
applied after expansion:

| Limit | Current default |
| --- | --- |
| Outputs per original item / per batch | 4 / 200 |
| Reference images per Flash / Pro image item | 3 / 14 |
| Reference attachments per batch | 1,000, counting repeats |
| Decoded inline reference bytes per batch | 128 MiB, counting repeats |
| ZIP items / bytes per download | 200 / 512 MiB |
| Download duration / concurrent downloads per user | 600 seconds / 1 |

Split larger workloads before submission. Per-item optional `reference_images`
entries accept `id`, `type`, `mime_type` and exactly one of `data` (base64
without a data-URL prefix) or `file_uri` (an internal `gs://` reference).
Supported MIME types are PNG, JPEG and WebP. Current request validation accepts
only `1K`/default image size; `2K` and `4K` are rejected.

Public states are `queued` (internal created/uploading/submitted), `running`,
`processing_results` (indexing), `settling`, `completed`, `failed`, `cancelled`
and `output_deleted`. Record deletion is allowed only after processing ends;
it hides the record from the user view without replacing provider cleanup or
billing. Output deletion is separate and requires completed results. Manual
or TTL output cleanup changes completed jobs to `output_deleted`; later
downloads return `410 BATCH_IMAGE_OUTPUT_DELETED`.

### Enablement and configuration

Enable Google-side APIs and billing/prepayment first. For Vertex, configure a
fixed managed bucket and grant the runtime and Vertex service agent the required
bucket permissions. Sub2API Plus switches do not grant Google-side access.

Enable `batch_image.enabled` (`BATCH_IMAGE_ENABLED`), then image generation and
`allow_batch_image_generation` on the intended **Gemini** group. Enable
`batch_image.queue_enabled` for Redis workers to poll/index/settle accepted
jobs. Both switches default to false. Workers reserve specific jobs from Redis;
they do not scan PostgreSQL as a polling queue. Turning workers off is separate
from turning admission off.

The `batch_image` configuration also owns these defaults:

| Settings | Defaults |
| --- | --- |
| `max_items_per_job_default` / `max_items_per_job_trial` | 200 / 50 |
| `max_prompt_chars_per_item` | 8,000 |
| `default_response_mime_type` / `default_image_size` | `image/png` / `1K` |
| `input_retention_after_terminal_hours` | 24 |
| `output_retention_after_terminal_hours` / `output_retention_max_days` | 72 hours / 7 days |
| `cleanup_interval_minutes` / `cleanup_batch_size` | 30 / 100 |
| `job_lock_ttl_seconds` / `stale_active_after_seconds` | 300 / 600 |
| `default_requeue_delay_seconds` / `error_retry_delay_seconds` | 30 / 60 |
| `vertex_enabled` / `vertex_location` | false / `global` |
| `vertex_managed_gcs_prefix` | `batch-image/{env}/{batch_id}` |

Set `vertex_project_id` and `vertex_managed_gcs_bucket` when using Vertex.
Complete queue keys, limits, retention and provider defaults are maintained in
[`BatchImageConfig` and its defaults](../backend/internal/config/config.go).
Configure bucket lifecycle/soft delete deliberately to avoid storage retained
after application cleanup. The compatibility `x-goog-api-key` header expects a
Sub2API Plus key, not a plain Google key.

### Billing and security

Admission snapshots the output-image price and applicable multipliers, estimates
cost and reserves a hold. Settlement follows indexing and charges only successful
images against that snapshot. Reference-image input may incur provider costs,
but there is no separate user-facing reference surcharge. Failed items are not
charged. Settlement uses `batch_image_settlement:{batch_id}` and is idempotent;
bounded settlement retries release the remaining hold through the idempotent
release path on final failure. Pricing is operator-configured; example amounts
are not a production price guarantee.

Submission retains the canonical ingress audit before account selection,
holds and provider writes. Provider requests retain the credential-owner
identity and signing rules in [Outbound Identity](OUTBOUND_IDENTITY.md).
Provider cleanup uses server-generated refs and prefix-safe deletion only.
Public responses and logs never contain provider credentials, service-account
JSON or image base64; PostgreSQL stores metadata, not image bytes.

Official setup references: [Gemini API keys](https://ai.google.dev/gemini-api/docs/api-key),
[Gemini Batch API](https://ai.google.dev/gemini-api/docs/batch-api),
[Gemini image generation](https://ai.google.dev/gemini-api/docs/image-generation)
and [Vertex batch inference](https://docs.cloud.google.com/gemini-enterprise-agent-platform/models/capabilities/batch-inference).
