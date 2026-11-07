#!/usr/bin/env node

const fs = require("fs");
const path = require("path");

const BASE_URL = (process.env.SUB2API_BASE_URL || "").replace(/\/$/, "");
const ADMIN_API_KEY = process.env.SUB2API_ADMIN_API_KEY || "";
const ADMIN_JWT = process.env.SUB2API_JWT || "";

function usage() {
  console.log(`Usage:
  sub2api-admin.js accounts list [--page-size 200] [--page N] [--search TEXT] [--platform openai] [--type oauth] [--status active] [--group ID|ungrouped] [--privacy-mode MODE] [--sort-by name] [--sort-order asc]
  sub2api-admin.js accounts export --confirmation-file confirmation.json [--ids 1,2] [--file accounts.json] [--include-proxies false] [scope filters...]
  sub2api-admin.js accounts import-data --file accounts.json [--skip-default-group-bind]
  sub2api-admin.js accounts create --json '{...}' | --file account.json
  sub2api-admin.js accounts update <id> --json '{...}' | --file patch.json
  sub2api-admin.js accounts get <id>
  sub2api-admin.js accounts delete <id>
  sub2api-admin.js accounts keep-only --name <account-name>
  sub2api-admin.js accounts usage <id> [--source SOURCE] [--force]
  sub2api-admin.js accounts stats <id> [--days 30]
  sub2api-admin.js accounts today-stats <id>
  sub2api-admin.js accounts batch-today-stats --ids 1,2
  sub2api-admin.js accounts set-status <id> <active|paused|...>
  sub2api-admin.js accounts set-schedulable <id> <true|false>
  sub2api-admin.js accounts clear-error <id>
  sub2api-admin.js accounts clear-rate-limit <id>
  sub2api-admin.js accounts recover-state <id>
  sub2api-admin.js accounts reset-quota <id>
  sub2api-admin.js accounts refresh <id>
  sub2api-admin.js accounts test <id>
  sub2api-admin.js accounts models <id>
  sub2api-admin.js accounts sync-models <id>
  sub2api-admin.js accounts apply-oauth <id> --json '{...}' | --file credentials.json
  sub2api-admin.js accounts batch-create --file accounts.json
  sub2api-admin.js accounts batch-update-credentials --json '{...}' | --file payload.json
  sub2api-admin.js accounts bulk-update --ids 1,2 --json '{...}' | --file patch.json
  sub2api-admin.js accounts batch-refresh --ids 1,2
  sub2api-admin.js accounts batch-clear-error --ids 1,2
  sub2api-admin.js accounts temp-unschedulable <id>
  sub2api-admin.js accounts reset-temp-unschedulable <id>
  sub2api-admin.js accounts crs-preview --json '{...}' | --file payload.json
  sub2api-admin.js accounts crs-sync --json '{...}' | --file payload.json
  sub2api-admin.js accounts import-codex-session --json '{...}' | --file payload.json
  sub2api-admin.js accounts antigravity-default-model-mapping
  sub2api-admin.js accounts import-json --file <path> --template-name <name> [--skip-name <name>] [--dry-run]
  sub2api-admin.js groups all
  sub2api-admin.js proxies all
  sub2api-admin.js redeem-codes list [--page-size 200] [--page N] [--type balance] [--status unused] [--search TEXT] [--sort-by id] [--sort-order desc]
  sub2api-admin.js redeem-codes export [--file redeem-codes.csv] [list filters...]
  sub2api-admin.js redeem-codes get <id>
  sub2api-admin.js redeem-codes generate --json '{...}' | --file payload.json [--idempotency-key KEY]
  sub2api-admin.js redeem-codes create-and-redeem --json '{...}' | --file payload.json [--idempotency-key KEY]
  sub2api-admin.js redeem-codes batch-update --ids 1,2 --json '{...}' | --file fields.json
  sub2api-admin.js redeem-codes delete <id>
  sub2api-admin.js redeem-codes batch-delete --ids 1,2
  sub2api-admin.js redeem-codes expire <id>
  sub2api-admin.js redeem-codes stats
  sub2api-admin.js error-rules list|get|create|update|delete|toggle ...
  sub2api-admin.js tls-profiles list|get|create|update|delete ...
  sub2api-admin.js api <GET|POST|PUT|DELETE> <admin-path> [--json '{...}' | --file payload.json]
`);
}

function parseArgs(argv) {
  const positional = [];
  const flags = {};
  for (let i = 0; i < argv.length; i += 1) {
    const token = argv[i];
    if (token.startsWith("--")) {
      const key = token.slice(2);
      const next = argv[i + 1];
      if (!next || next.startsWith("--")) {
        flags[key] = true;
      } else {
        if (flags[key] === undefined) {
          flags[key] = next;
        } else if (Array.isArray(flags[key])) {
          flags[key].push(next);
        } else {
          flags[key] = [flags[key], next];
        }
        i += 1;
      }
    } else {
      positional.push(token);
    }
  }
  return { positional, flags };
}

function authHeaders() {
  if (!BASE_URL) throw new Error("Missing SUB2API_BASE_URL");
  if (ADMIN_API_KEY) return { "x-api-key": ADMIN_API_KEY };
  if (ADMIN_JWT) return { Authorization: `Bearer ${ADMIN_JWT}` };
  throw new Error("Missing SUB2API_ADMIN_API_KEY or SUB2API_JWT");
}

async function apiRequest(method, pathname, body, extraHeaders = {}) {
  const headers = {
    ...authHeaders(),
    Accept: "application/json",
    ...extraHeaders,
  };
  const options = { method, headers };
  if (body !== undefined) {
    headers["Content-Type"] = "application/json";
    options.body = JSON.stringify(body);
  }
  const res = await fetch(`${BASE_URL}${pathname}`, options);
  const text = await res.text();
  let data;
  try {
    data = JSON.parse(text);
  } catch {
    data = { raw: text };
  }
  if (!res.ok || (data && data.code !== undefined && data.code !== 0 && data.code !== "0")) {
    const detail = data.message || data.code || res.statusText;
    throw new Error(`${method} ${pathname} failed: ${detail}`);
  }
  return data.data;
}

async function apiRawRequest(method, pathname, body, extraHeaders = {}) {
  const headers = {
    ...authHeaders(),
    Accept: "*/*",
    ...extraHeaders,
  };
  const options = { method, headers };
  if (body !== undefined) {
    headers["Content-Type"] = "application/json";
    options.body = JSON.stringify(body);
  }
  const res = await fetch(`${BASE_URL}${pathname}`, options);
  const text = await res.text();
  if (!res.ok) {
    let detail = res.statusText;
    try {
      const data = JSON.parse(text);
      detail = data.message || data.code || detail;
    } catch {
      if (text) detail = text;
    }
    throw new Error(`${method} ${pathname} failed: ${detail}`);
  }
  return text;
}

function encodeQuery(params) {
  const pairs = [];
  for (const [key, value] of Object.entries(params)) {
    if (value === undefined || value === null || value === false || value === "") continue;
    pairs.push(`${encodeURIComponent(key)}=${encodeURIComponent(String(value))}`);
  }
  return pairs.length ? `?${pairs.join("&")}` : "";
}

function normalizeAdminPath(pathname) {
  if (!pathname.startsWith("/")) pathname = `/${pathname}`;
  if (pathname.startsWith("/api/v1/admin/")) return pathname;
  if (pathname.startsWith("/admin/")) return `/api/v1${pathname}`;
  return pathname;
}

async function adminRequest(method, adminPath, body) {
  return apiRequest(method, normalizeAdminPath(adminPath), body);
}

async function adminRequestWithHeaders(method, adminPath, body, headers = {}) {
  return apiRequest(method, normalizeAdminPath(adminPath), body, headers);
}

async function adminRawRequest(method, adminPath, body, headers = {}) {
  return apiRawRequest(method, normalizeAdminPath(adminPath), body, headers);
}

async function listAccounts(options = {}) {
  const params = {
    page: options.page || 1,
    page_size: options.pageSize || 200,
    sort_by: options.sortBy || "name",
    sort_order: options.sortOrder || "asc",
    lite: options.lite === false ? undefined : 1,
    search: options.search,
    platform: options.platform,
    type: options.type,
    status: options.status,
    group: options.group,
    privacy_mode: options.privacyMode,
  };
  return apiRequest("GET", `/api/v1/admin/accounts${encodeQuery(params)}`);
}

async function getAccount(id) {
  return apiRequest("GET", `/api/v1/admin/accounts/${id}`);
}

async function deleteAccount(id) {
  return apiRequest("DELETE", `/api/v1/admin/accounts/${id}`);
}

function asArray(value) {
  if (value === undefined) return [];
  return Array.isArray(value) ? value : [value];
}

function parseBool(value, name = "value") {
  if (value === true || value === "true" || value === "1" || value === "yes") return true;
  if (value === false || value === "false" || value === "0" || value === "no") return false;
  throw new Error(`${name} must be true or false`);
}

function parseIds(value) {
  if (!value) throw new Error("requires --ids");
  return String(value)
    .split(",")
    .map((id) => id.trim())
    .filter(Boolean)
    .map((id) => {
      const n = Number(id);
      if (!Number.isFinite(n)) throw new Error(`invalid id: ${id}`);
      return n;
    });
}

function readJsonPayload(flags, { required = true } = {}) {
  if (flags.json !== undefined) return JSON.parse(flags.json);
  if (flags.file !== undefined) return JSON.parse(fs.readFileSync(path.resolve(flags.file), "utf8"));
  if (required) throw new Error("requires --json or --file");
  return undefined;
}

function accountListOptions(flags) {
  return {
    page: Number(flags.page || 1),
    pageSize: Number(flags["page-size"] || 200),
    search: flags.search,
    platform: flags.platform,
    type: flags.type,
    status: flags.status,
    group: flags.group,
    privacyMode: flags["privacy-mode"],
    sortBy: flags["sort-by"] || "name",
    sortOrder: flags["sort-order"] || "asc",
    lite: flags.lite === undefined ? true : parseBool(flags.lite, "--lite"),
  };
}

function redeemCodesListOptions(flags) {
  return {
    page: Number(flags.page || 1),
    pageSize: Number(flags["page-size"] || 200),
    type: flags.type,
    status: flags.status,
    search: flags.search,
    sortBy: flags["sort-by"] || "id",
    sortOrder: flags["sort-order"] || "desc",
  };
}

function redeemCodesQuery(flags) {
  const options = redeemCodesListOptions(flags);
  return encodeQuery({
    page: options.page,
    page_size: options.pageSize,
    type: options.type,
    status: options.status,
    search: options.search,
    sort_by: options.sortBy,
    sort_order: options.sortOrder,
  });
}

function idempotencyHeaders(flags) {
  if (!flags["idempotency-key"]) return {};
  return { "Idempotency-Key": flags["idempotency-key"] };
}

function printJson(data) {
  console.log(JSON.stringify(data, null, 2));
}

async function exportAccountData(flags) {
  // Keep the CLI's API-key-first authentication convention. Export is a
  // JWT-only endpoint, so reject conflicting credentials before any request.
  if (!ADMIN_JWT || ADMIN_API_KEY) {
    throw new Error("accounts export requires SUB2API_JWT; unset SUB2API_ADMIN_API_KEY for this command");
  }
  const filterFlags = {
    platform: "platform",
    type: "type",
    status: "status",
    group: "group",
    search: "search",
    "privacy-mode": "privacy_mode",
    "sort-by": "sort_by",
    "sort-order": "sort_order",
  };
  const allowed = new Set(["confirmation-file", "ids", "file", "include-proxies", ...Object.keys(filterFlags)]);
  for (const flag of Object.keys(flags)) {
    if (!allowed.has(flag)) throw new Error(`unsupported accounts export option: --${flag}`);
  }
  const filters = {};
  for (const [flag, field] of Object.entries(filterFlags)) {
    if (flags[flag] !== undefined) {
      if (typeof flags[flag] !== "string") throw new Error(`--${flag} requires one value`);
      filters[field] = flags[flag];
    }
  }
  const body = {};
  if (flags.ids !== undefined) {
    if (typeof flags.ids !== "string") throw new Error("--ids requires comma-separated positive integer account IDs");
    const ids = flags.ids.split(",").map((id) => Number(id.trim()));
    if (ids.some((id) => !Number.isSafeInteger(id) || id <= 0)) {
      throw new Error("--ids requires comma-separated positive integer account IDs");
    }
    if (Object.keys(filters).length) throw new Error("accounts export cannot combine --ids with scope filters");
    body.ids = ids;
  } else {
    body.filters = filters;
  }
  if (flags["include-proxies"] !== undefined) {
    body.include_proxies = parseBool(flags["include-proxies"], "--include-proxies");
  }
  if (flags.file !== undefined && (typeof flags.file !== "string" || !flags.file.trim())) {
    throw new Error("--file requires one output path");
  }
  if (typeof flags["confirmation-file"] !== "string" || !flags["confirmation-file"].trim()) {
    throw new Error("accounts export requires --confirmation-file containing password and optional totp_code");
  }
  let confirmation;
  try {
    confirmation = JSON.parse(fs.readFileSync(path.resolve(flags["confirmation-file"]), "utf8"));
  } catch {
    // JSON parser errors may quote the input, including the administrator password.
    throw new Error("Unable to read export confirmation JSON");
  }
  if (!confirmation || Array.isArray(confirmation) || typeof confirmation.password !== "string" || !confirmation.password.trim()) {
    throw new Error("Export confirmation requires a nonempty password string");
  }
  if (confirmation.totp_code !== undefined && typeof confirmation.totp_code !== "string") {
    throw new Error("Export confirmation totp_code must be a string");
  }
  body.password = confirmation.password;
  if (confirmation.totp_code !== undefined) body.totp_code = confirmation.totp_code.trim();

  const requirements = await adminRequest("GET", "/admin/accounts/export/requirements");
  if (!requirements || typeof requirements.step_up_required !== "boolean") {
    throw new Error("Invalid export confirmation requirements response");
  }
  if (requirements.step_up_required && !body.totp_code) {
    throw new Error("Account export requires totp_code in the confirmation file");
  }
  // The server rechecks password, TOTP policy, scope and critical audit at export time.
  const data = await adminRequest("POST", "/admin/accounts/export", body);
  if (!data || !Array.isArray(data.accounts) || !Array.isArray(data.proxies)) {
    throw new Error("Invalid account export response");
  }
  return data;
}

async function commandAccounts(args) {
  const sub = args.positional[1];
  if (sub === "list") {
    const data = await listAccounts(accountListOptions(args.flags));
    printJson(data);
    return;
  }

  if (sub === "export") {
    if (args.positional.length !== 2) {
      throw new Error("accounts export accepts account IDs only through --ids");
    }
    const data = await exportAccountData(args.flags);
    if (args.flags.file) {
      const outputPath = path.resolve(args.flags.file);
      // Stage beside the destination so replacement is atomic. A fresh private
      // inode also avoids following output symlinks or modifying hard links.
      const stagingDir = fs.mkdtempSync(path.join(path.dirname(outputPath), ".sub2api-export-"));
      try {
        const stagingPath = path.join(stagingDir, "accounts.json");
        fs.writeFileSync(stagingPath, JSON.stringify(data, null, 2), { flag: "wx", mode: 0o600 });
        fs.renameSync(stagingPath, outputPath);
      } finally {
        fs.rmSync(stagingDir, { recursive: true, force: true });
      }
      printJson({ file: outputPath, count: data.accounts.length, skipped_shadows: data.skipped_shadows || 0 });
    } else {
      printJson(data);
    }
    return;
  }

  if (sub === "import-data") {
    const file = args.flags.file;
    if (!file) throw new Error("accounts import-data requires --file");
    const payload = {
      data: JSON.parse(fs.readFileSync(path.resolve(file), "utf8")),
      skip_default_group_bind: Boolean(args.flags["skip-default-group-bind"]),
    };
    printJson(await adminRequest("POST", "/admin/accounts/data", payload));
    return;
  }

  if (sub === "create") {
    printJson(await adminRequest("POST", "/admin/accounts", readJsonPayload(args.flags)));
    return;
  }

  if (sub === "update") {
    const id = args.positional[2];
    if (!id) throw new Error("accounts update requires <id>");
    printJson(await adminRequest("PUT", `/admin/accounts/${id}`, readJsonPayload(args.flags)));
    return;
  }

  if (sub === "get") {
    const id = args.positional[2];
    if (!id) throw new Error("accounts get requires <id>");
    const data = await getAccount(id);
    printJson(data);
    return;
  }

  if (sub === "delete") {
    const id = args.positional[2];
    if (!id) throw new Error("accounts delete requires <id>");
    const data = await deleteAccount(id);
    printJson(data);
    return;
  }

  if (sub === "keep-only") {
    const name = args.flags.name;
    if (!name) throw new Error("accounts keep-only requires --name");
    const data = await listAccounts({ pageSize: Number(args.flags["page-size"] || 500) });
    const items = data.items || [];
    const keep = items.find((item) => item.name === name);
    if (!keep) throw new Error(`account not found: ${name}`);
    const targets = items.filter((item) => item.name !== name);
    const results = [];
    for (const item of targets) {
      const out = await deleteAccount(item.id);
      results.push({ id: item.id, name: item.name, result: out });
    }
    printJson({ kept: { id: keep.id, name: keep.name }, deleted: results });
    return;
  }

  if (sub === "usage") {
    const id = args.positional[2];
    if (!id) throw new Error("accounts usage requires <id>");
    const params = encodeQuery({ source: args.flags.source, force: args.flags.force ? "true" : undefined });
    printJson(await adminRequest("GET", `/admin/accounts/${id}/usage${params}`));
    return;
  }

  if (sub === "stats") {
    const id = args.positional[2];
    if (!id) throw new Error("accounts stats requires <id>");
    printJson(await adminRequest("GET", `/admin/accounts/${id}/stats${encodeQuery({ days: args.flags.days || 30 })}`));
    return;
  }

  if (sub === "today-stats") {
    const id = args.positional[2];
    if (!id) throw new Error("accounts today-stats requires <id>");
    printJson(await adminRequest("GET", `/admin/accounts/${id}/today-stats`));
    return;
  }

  if (sub === "batch-today-stats") {
    printJson(await adminRequest("POST", "/admin/accounts/today-stats/batch", { account_ids: parseIds(args.flags.ids) }));
    return;
  }

  if (sub === "set-status") {
    const id = args.positional[2];
    const status = args.positional[3];
    if (!id || !status) throw new Error("accounts set-status requires <id> <status>");
    printJson(await adminRequest("PUT", `/admin/accounts/${id}`, { status }));
    return;
  }

  if (sub === "set-schedulable") {
    const id = args.positional[2];
    const schedulable = args.positional[3];
    if (!id || schedulable === undefined) throw new Error("accounts set-schedulable requires <id> <true|false>");
    printJson(await adminRequest("POST", `/admin/accounts/${id}/schedulable`, { schedulable: parseBool(schedulable, "schedulable") }));
    return;
  }

  const noBodyPostById = {
    "clear-error": "clear-error",
    "clear-rate-limit": "clear-rate-limit",
    "recover-state": "recover-state",
    "reset-quota": "reset-quota",
    refresh: "refresh",
    test: "test",
    "sync-models": "models/sync-upstream",
    "set-privacy": "set-privacy",
  };
  if (noBodyPostById[sub]) {
    const id = args.positional[2];
    if (!id) throw new Error(`accounts ${sub} requires <id>`);
    printJson(await adminRequest("POST", `/admin/accounts/${id}/${noBodyPostById[sub]}`));
    return;
  }

  if (sub === "models") {
    const id = args.positional[2];
    if (!id) throw new Error("accounts models requires <id>");
    printJson(await adminRequest("GET", `/admin/accounts/${id}/models`));
    return;
  }

  if (sub === "apply-oauth") {
    const id = args.positional[2];
    if (!id) throw new Error("accounts apply-oauth requires <id>");
    printJson(await adminRequest("POST", `/admin/accounts/${id}/apply-oauth-credentials`, readJsonPayload(args.flags)));
    return;
  }

  if (sub === "batch-create") {
    const payload = readJsonPayload(args.flags);
    const accounts = Array.isArray(payload) ? payload : payload.accounts;
    if (!Array.isArray(accounts)) throw new Error("batch-create payload must be an array or {accounts:[...]}");
    printJson(await adminRequest("POST", "/admin/accounts/batch", { accounts }));
    return;
  }

  if (sub === "batch-update-credentials") {
    printJson(await adminRequest("POST", "/admin/accounts/batch-update-credentials", readJsonPayload(args.flags)));
    return;
  }

  if (sub === "bulk-update") {
    const patch = readJsonPayload(args.flags, { required: false }) || {};
    const payload = args.flags.ids ? { account_ids: parseIds(args.flags.ids), ...patch } : patch;
    if (!Array.isArray(payload.account_ids) || payload.account_ids.length === 0) {
      throw new Error("accounts bulk-update requires --ids or payload.account_ids");
    }
    printJson(await adminRequest("POST", "/admin/accounts/bulk-update", payload));
    return;
  }

  if (sub === "batch-refresh") {
    printJson(await adminRequest("POST", "/admin/accounts/batch-refresh", { account_ids: parseIds(args.flags.ids) }));
    return;
  }

  if (sub === "batch-clear-error") {
    printJson(await adminRequest("POST", "/admin/accounts/batch-clear-error", { account_ids: parseIds(args.flags.ids) }));
    return;
  }

  if (sub === "temp-unschedulable") {
    const id = args.positional[2];
    if (!id) throw new Error("accounts temp-unschedulable requires <id>");
    printJson(await adminRequest("GET", `/admin/accounts/${id}/temp-unschedulable`));
    return;
  }

  if (sub === "reset-temp-unschedulable") {
    const id = args.positional[2];
    if (!id) throw new Error("accounts reset-temp-unschedulable requires <id>");
    printJson(await adminRequest("DELETE", `/admin/accounts/${id}/temp-unschedulable`));
    return;
  }

  if (sub === "crs-preview") {
    printJson(await adminRequest("POST", "/admin/accounts/sync/crs/preview", readJsonPayload(args.flags)));
    return;
  }

  if (sub === "crs-sync") {
    printJson(await adminRequest("POST", "/admin/accounts/sync/crs", readJsonPayload(args.flags)));
    return;
  }

  if (sub === "import-codex-session") {
    printJson(await adminRequest("POST", "/admin/accounts/import/codex-session", readJsonPayload(args.flags)));
    return;
  }

  if (sub === "antigravity-default-model-mapping") {
    printJson(await adminRequest("GET", "/admin/accounts/antigravity/default-model-mapping"));
    return;
  }

  if (sub === "import-json") {
    const file = args.flags.file;
    const templateName = args.flags["template-name"];
    const dryRun = Boolean(args.flags["dry-run"]);
    const skipNames = new Set(asArray(args.flags["skip-name"]));
    if (!file) throw new Error("accounts import-json requires --file");
    if (!templateName) throw new Error("accounts import-json requires --template-name");

    const raw = JSON.parse(fs.readFileSync(path.resolve(file), "utf8"));
    const accounts = raw.accounts || [];
    const live = await listAccounts({ pageSize: 500 });
    const liveItems = live.items || [];
    const template = liveItems.find((item) => item.name === templateName);
    if (!template) throw new Error(`template account not found in backend: ${templateName}`);

    const templateGroupIds = template.group_ids || [];
    const templateConcurrency = template.concurrency;
    const templatePriority = template.priority;
    const templateModelMapping = (template.credentials && template.credentials.model_mapping) || {};

    const existingNames = new Set(liveItems.map((item) => item.name));
    const planned = accounts.filter((acc) => acc.name !== templateName && !skipNames.has(acc.name));

    if (dryRun) {
      printJson({
        template: {
          name: template.name,
          concurrency: templateConcurrency,
          priority: templatePriority,
          group_ids: templateGroupIds,
          model_mapping: templateModelMapping,
        },
        to_import: planned.map((acc) => ({
          name: acc.name,
          exists: existingNames.has(acc.name),
        })),
      });
      return;
    }

    const results = [];
    for (const acc of planned) {
      if (existingNames.has(acc.name)) {
        results.push({ name: acc.name, skipped: true, reason: "already exists" });
        continue;
      }
      const payload = {
        ...acc,
        group_ids: templateGroupIds,
        concurrency: templateConcurrency,
        priority: templatePriority,
        credentials: {
          ...acc.credentials,
          model_mapping: templateModelMapping,
        },
      };
      const created = await apiRequest("POST", "/api/v1/admin/accounts", payload);
      results.push({ name: acc.name, id: created.id, skipped: false });
    }
    printJson({ imported: results });
    return;
  }

  throw new Error(`unknown accounts subcommand: ${sub || "(missing)"}`);
}

async function commandGroups(args) {
  const sub = args.positional[1];
  if (sub === "all") {
    printJson(await adminRequest("GET", "/admin/groups/all"));
    return;
  }
  throw new Error(`unknown groups subcommand: ${sub || "(missing)"}`);
}

async function commandProxies(args) {
  const sub = args.positional[1];
  if (sub === "all") {
    printJson(await adminRequest("GET", "/admin/proxies/all"));
    return;
  }
  throw new Error(`unknown proxies subcommand: ${sub || "(missing)"}`);
}

async function commandRedeemCodes(args) {
  const sub = args.positional[1];
  if (sub === "list") {
    printJson(await adminRequest("GET", `/admin/redeem-codes${redeemCodesQuery(args.flags)}`));
    return;
  }
  if (sub === "export") {
    const csv = await adminRawRequest("GET", `/admin/redeem-codes/export${redeemCodesQuery(args.flags)}`);
    if (args.flags.file) {
      fs.writeFileSync(path.resolve(args.flags.file), csv);
      printJson({ file: path.resolve(args.flags.file) });
    } else {
      process.stdout.write(csv);
    }
    return;
  }
  if (sub === "get") {
    const id = args.positional[2];
    if (!id) throw new Error("redeem-codes get requires <id>");
    printJson(await adminRequest("GET", `/admin/redeem-codes/${id}`));
    return;
  }
  if (sub === "generate") {
    printJson(await adminRequestWithHeaders("POST", "/admin/redeem-codes/generate", readJsonPayload(args.flags), idempotencyHeaders(args.flags)));
    return;
  }
  if (sub === "create-and-redeem") {
    printJson(await adminRequestWithHeaders("POST", "/admin/redeem-codes/create-and-redeem", readJsonPayload(args.flags), idempotencyHeaders(args.flags)));
    return;
  }
  if (sub === "batch-update") {
    const fields = readJsonPayload(args.flags, { required: false }) || {};
    const payload = args.flags.ids ? { ids: parseIds(args.flags.ids), fields } : fields;
    if (!Array.isArray(payload.ids) || payload.ids.length === 0) {
      throw new Error("redeem-codes batch-update requires --ids or payload.ids");
    }
    if (!payload.fields || typeof payload.fields !== "object") {
      throw new Error("redeem-codes batch-update requires fields");
    }
    printJson(await adminRequest("POST", "/admin/redeem-codes/batch-update", payload));
    return;
  }
  if (sub === "delete") {
    const id = args.positional[2];
    if (!id) throw new Error("redeem-codes delete requires <id>");
    printJson(await adminRequest("DELETE", `/admin/redeem-codes/${id}`));
    return;
  }
  if (sub === "batch-delete") {
    printJson(await adminRequest("POST", "/admin/redeem-codes/batch-delete", { ids: parseIds(args.flags.ids) }));
    return;
  }
  if (sub === "expire") {
    const id = args.positional[2];
    if (!id) throw new Error("redeem-codes expire requires <id>");
    printJson(await adminRequest("POST", `/admin/redeem-codes/${id}/expire`));
    return;
  }
  if (sub === "stats") {
    printJson(await adminRequest("GET", "/admin/redeem-codes/stats"));
    return;
  }
  throw new Error(`unknown redeem-codes subcommand: ${sub || "(missing)"}`);
}

async function commandCrudResource(args, name, basePath, options = {}) {
  const sub = args.positional[1];
  const id = args.positional[2];
  if (sub === "list") {
    printJson(await adminRequest("GET", basePath));
    return;
  }
  if (sub === "get") {
    if (!id) throw new Error(`${name} get requires <id>`);
    printJson(await adminRequest("GET", `${basePath}/${id}`));
    return;
  }
  if (sub === "create") {
    printJson(await adminRequest("POST", basePath, readJsonPayload(args.flags)));
    return;
  }
  if (sub === "update") {
    if (!id) throw new Error(`${name} update requires <id>`);
    printJson(await adminRequest("PUT", `${basePath}/${id}`, readJsonPayload(args.flags)));
    return;
  }
  if (sub === "delete") {
    if (!id) throw new Error(`${name} delete requires <id>`);
    printJson(await adminRequest("DELETE", `${basePath}/${id}`));
    return;
  }
  if (options.toggle && sub === "toggle") {
    const enabled = args.positional[3];
    if (!id || enabled === undefined) throw new Error(`${name} toggle requires <id> <true|false>`);
    printJson(await adminRequest("PUT", `${basePath}/${id}`, { enabled: parseBool(enabled, "enabled") }));
    return;
  }
  throw new Error(`unknown ${name} subcommand: ${sub || "(missing)"}`);
}

async function commandApi(args) {
  const method = args.positional[1];
  const pathname = args.positional[2];
  if (!method || !pathname) throw new Error("api requires <GET|POST|PUT|DELETE> <admin-path>");
  const body = readJsonPayload(args.flags, { required: false });
  printJson(await adminRequest(method.toUpperCase(), pathname, body));
}

async function main() {
  const args = parseArgs(process.argv.slice(2));
  const root = args.positional[0];
  if (!root) {
    usage();
    process.exit(1);
  }
  if (root === "accounts") {
    await commandAccounts(args);
    return;
  }
  if (root === "groups") {
    await commandGroups(args);
    return;
  }
  if (root === "proxies") {
    await commandProxies(args);
    return;
  }
  if (root === "redeem-codes") {
    await commandRedeemCodes(args);
    return;
  }
  if (root === "error-rules") {
    await commandCrudResource(args, "error-rules", "/admin/error-passthrough-rules", { toggle: true });
    return;
  }
  if (root === "tls-profiles") {
    await commandCrudResource(args, "tls-profiles", "/admin/tls-fingerprint-profiles");
    return;
  }
  if (root === "api") {
    await commandApi(args);
    return;
  }
  throw new Error(`unknown command: ${root}`);
}

main().catch((error) => {
  console.error(error.message);
  process.exit(1);
});
