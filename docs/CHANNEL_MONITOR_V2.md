# Passive channel monitor V2

V2 derives metrics from recorded requests. Model visibility settings do not send
provider requests, start probes, delete logs or modify collected per-model facts.

## Model visibility

Administrator `GET/PUT /api/v1/admin/channel-monitor-v2/config` stores each
`platforms[]` entry with:

- `models: string[]`: names to show individually in selected mode; these also
  seed zero-traffic rows.
- `model_display_mode: "all" | "selected"`: all shows real model names; selected
  shows `models` individually and folds remaining visible models into `__other__`.
  An explicitly selected empty list sends every visible model to Other.
- `excluded_models: string[]`: complete requested-model names removed from every
  V2 view and calculation for that platform. The default is `[]`.

Exclusion wins even if a name also occurs in `models`. Names are trimmed,
deduplicated and compared exactly, preserving case. A policy for one platform
does not affect another platform's identical model name. With all mode, the
visible set is all real models minus exclusions. With selected mode, excluded
models contribute neither to named rows nor to Other.

Older configurations without a mode retain their semantics: a nonempty `models`
list means selected mode and an empty list means all mode. Migration 295 fills
these defaults without changing existing named/excluded lists or custom fields;
its configuration version advances once. New and saved entries return explicit
mode and non-null arrays.

Exclusions apply before aggregation on every read of minute facts and fixed
5-minute, 1-hour, 12-hour and 1-day rollups. They affect dimensions, model rows,
platform/group/global totals, trend points, tokens, latency histograms, health,
ignored-error adjustments, errors and administrator error samples, and user
rankings. Excluded named models do not return as zero-traffic placeholders.
Watermarks continue to describe source coverage, independently of visibility.
No aggregate rebuild is required. Every read loads current configuration, so
saving or restoring an exclusion also changes historical windows immediately.
Raw logs and per-model facts remain intact for restoration and retention jobs.

In the administrator's platform/model editor, hovering or focusing a model's
action button shows a themed card with the action and its effect after saving.
The explanations follow the current Chinese/English interface language. Moving
to Other retains statistics; exclusion removes the model from every aggregate;
restoration follows the current display mode. Viewing a tooltip changes no draft
or saved configuration.

Configuration writes use the existing complete-document optimistic lock:
`version` must equal the current version, successful writes increment it, and a
stale write returns HTTP 409. Invalid display modes return HTTP 400. Public
snapshot configuration omits named/excluded inventories and display policy.
Configuration and read filters accept at most 128 platforms (64 bytes per name)
and 4096 group IDs. Each named/excluded list accepts at most 4096 entries (1024
bytes per model), with at most 32768 model declarations across a configuration.
Invalid names or counts return HTTP 400; they are never silently truncated.

## Administrator model catalog

`GET /api/v1/admin/channel-monitor-v2/model-catalog` returns complete real names
for the settings editor without folding Other or applying saved exclusions,
platform enablement, or saved group restrictions. It is an administrator route
under the monitor feature guard, available when the runtime uses V1 or the V2
configuration is disabled. It is absent from user and support route groups.

Query parameters:

- `range`: `90m`, `24h`, `7d` or `30d` (default `30d`), with the same aligned
  window boundaries as V2 reads.
- `platform`: optional repeated/comma-separated platform names.
- `group_id`: optional repeated/comma-separated positive group IDs.

The response contains `config_version`, `range`, `start`, `end`, and `items`.
Each item contains `platform`, `model`, `success_requests`, `error_requests`, and
`sources`. Sources are `success`, `error`, `configured`, and/or `excluded` in
that order. Items are sorted by platform then exact model name, without
pagination or truncation. The database query has a 15-second deadline and accepts
at most 10000 distinct candidates (including saved seeds). Larger catalogs fail
with HTTP 422 and timeouts with HTTP 504; narrow the platform/group/range and retry.
An error never presents an incomplete candidate list as a complete catalog.

Observation names come from `requested_model`, falling back to `model` and then
`unknown` only for records without a requested name. Successful usage and failed
requests are both queried, so failed-only models are discoverable. Usage counts
follow V2's billable-success rule and exclude count-token and async-submission
request types. Catalog usage deduplicates request IDs per platform/model/group
across the requested interval; errors keep the latest eligible record per request
in that interval, even when a retry changes platform/group. Missing IDs count as
separate records. Catalog counts describe discovery observations, rather than
replacing V2's bucketed metric counters (which may count the same usage request ID
again in a different minute). Both sources apply platform/group/time conditions
before aggregation; error deduplication checks later records across scopes through
the existing request-ID index, without sorting all platforms' error history.
Composite groups resolve their concrete account platform. Counts describe raw
records still retained within the requested interval; they do not require the
passive materializer to have backfilled that interval.

Saved named and excluded entries are included as zero-observation candidates
when not observed, scoped by requested platform, even for disabled platforms.
Group filtering restricts observations; configuration candidates have no group
ownership. The editor merges its factory candidates and unsaved entries, then
classifies candidates from the current draft. Catalog loading neither saves
configuration nor changes monitoring/traffic collection. Provider discovery,
credentials, request bodies and error messages are never returned by this API.
