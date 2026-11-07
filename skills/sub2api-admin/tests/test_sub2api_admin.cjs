// Contract: AccountExportRequest and ExportData's JWT/password/TOTP boundary.
// Exercise the actual CLI over loopback HTTP; no deployed instance is contacted.
const assert = require("node:assert/strict");
const { execFile } = require("node:child_process");
const fs = require("node:fs");
const http = require("node:http");
const os = require("node:os");
const path = require("node:path");
const { test } = require("node:test");

const CLI = path.resolve(__dirname, "../scripts/sub2api-admin.js");
const PASSWORD = " fixture administrator password ";
const JWT = "fixture-admin-jwt";
const BACKUP = {
  type: "sub2api-data",
  version: 1,
  exported_at: "2026-11-07T00:00:00Z",
  accounts: [{ name: "selected account", credentials: { access_token: "fixture-exported-secret" } }],
  proxies: [],
  skipped_shadows: 2,
};

async function fixture(t, options = {}) {
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), "sub2api-admin-export-"));
  const confirmationFile = path.join(directory, "confirmation.json");
  const outputFile = path.join(directory, "accounts.json");
  fs.writeFileSync(confirmationFile, JSON.stringify({ password: PASSWORD }), { mode: 0o600 });
  const requests = [];
  const server = http.createServer(async (request, response) => {
    let rawBody = "";
    for await (const chunk of request) rawBody += chunk;
    requests.push({ method: request.method, url: request.url, headers: request.headers, rawBody });
    let status = 200;
    let body;
    if (request.method === "GET" && request.url === "/api/v1/admin/accounts/export/requirements") {
      status = options.requirementsStatus || 200;
      body = options.requirementsResponse || { code: 0, data: { step_up_required: options.stepUp || false } };
    } else if (request.method === "POST" && request.url === "/api/v1/admin/accounts/export") {
      status = options.exportStatus || 200;
      body = options.exportResponse || { code: 0, data: BACKUP };
    } else if (request.method === "GET" && request.url.startsWith("/api/v1/admin/accounts?")) {
      body = { code: 0, data: { items: [] } };
    } else {
      status = 404;
      body = { code: 404, message: "Unexpected or legacy API route" };
    }
    response.writeHead(status, { "Content-Type": "application/json" });
    response.end(JSON.stringify(body));
  });
  await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
  t.after(async () => {
    server.closeAllConnections();
    await new Promise((resolve) => server.close(resolve));
    fs.rmSync(directory, { recursive: true, force: true });
  });

  async function invoke(args, overrides = {}) {
    const env = {
      ...process.env,
      SUB2API_BASE_URL: `http://127.0.0.1:${server.address().port}`,
      SUB2API_ADMIN_API_KEY: "",
      SUB2API_JWT: JWT,
      NODE_USE_ENV_PROXY: "0",
      ...overrides,
    };
    return new Promise((resolve) => {
      execFile(process.execPath, [CLI, ...args], { env, timeout: 10000 }, (error, stdout, stderr) => {
        resolve({ code: error ? error.code : 0, stdout, stderr });
      });
    });
  }
  async function run(args = [], overrides = {}) {
    return invoke([
      "accounts", "export", "--confirmation-file", confirmationFile,
      "--file", outputFile, ...args,
    ], overrides);
  }
  function assertNoExport(result) {
    assert.equal(result.code, 1, result.stderr);
    assert.ok(!requests.some((request) => request.method === "POST"));
    assert.ok(!fs.existsSync(outputFile));
    assert.equal(result.stdout, "");
    assert.ok(!result.stderr.includes(PASSWORD.trim()));
  }
  return { requests, confirmationFile, outputFile, invoke, run, assertNoExport };
}

test("selected IDs, password and leading-zero TOTP travel only in the POST body", async (t) => {
  const f = await fixture(t, { stepUp: true });
  fs.writeFileSync(f.confirmationFile, JSON.stringify({ password: PASSWORD, totp_code: "012345" }));
  const result = await f.run(["--ids", "40,39", "--include-proxies", "false"]);
  assert.equal(result.code, 0, result.stderr);
  assert.deepEqual(f.requests.map(({ method, url }) => ({ method, url })), [
    { method: "GET", url: "/api/v1/admin/accounts/export/requirements" },
    { method: "POST", url: "/api/v1/admin/accounts/export" },
  ]);
  assert.equal(f.requests[0].rawBody, "");
  assert.deepEqual(JSON.parse(f.requests[1].rawBody), {
    ids: [40, 39], include_proxies: false, password: PASSWORD, totp_code: "012345",
  });
  for (const request of f.requests) {
    assert.equal(request.headers.authorization, `Bearer ${JWT}`);
    assert.equal(request.headers["x-api-key"], undefined);
  }
  assert.equal(f.requests[1].headers["content-type"], "application/json");
  assert.deepEqual(JSON.parse(fs.readFileSync(f.outputFile, "utf8")), BACKUP);
  assert.equal(fs.statSync(f.outputFile).mode & 0o777, 0o600);
  assert.deepEqual(JSON.parse(result.stdout), { file: f.outputFile, count: 1, skipped_shadows: 2 });
  assert.ok(!result.stdout.includes("fixture-exported-secret"));
  assert.ok(!result.stdout.includes(PASSWORD.trim()));
  assert.ok(!result.stdout.includes("012345"));
});

test("scope filters map to the POST body and omitted proxy flag preserves server default", async (t) => {
  const f = await fixture(t);
  const result = await f.run([
    "--platform", "openai", "--type", "oauth", "--status", "active",
    "--group", "12", "--privacy-mode", "training_disabled",
    "--search", "named & selected", "--sort-by", "name", "--sort-order", "desc",
  ]);
  assert.equal(result.code, 0, result.stderr);
  assert.deepEqual(JSON.parse(f.requests[1].rawBody), {
    password: PASSWORD,
    filters: {
      platform: "openai", type: "oauth", status: "active", group: "12",
      privacy_mode: "training_disabled", search: "named & selected", sort_by: "name", sort_order: "desc",
    },
  });
});

test("empty scope exports all, includes explicit proxies and reports an empty backup correctly", async (t) => {
  const f = await fixture(t, { exportResponse: { code: 0, data: { accounts: [], proxies: [] } } });
  const result = await f.run(["--include-proxies", "true"]);
  assert.equal(result.code, 0, result.stderr);
  assert.deepEqual(JSON.parse(f.requests[1].rawBody), { password: PASSWORD, filters: {}, include_proxies: true });
  assert.deepEqual(JSON.parse(result.stdout), { file: f.outputFile, count: 0, skipped_shadows: 0 });
});

test("overwriting an existing export restricts its permissions", async (t) => {
  const f = await fixture(t);
  fs.writeFileSync(f.outputFile, "older backup");
  fs.chmodSync(f.outputFile, 0o644);
  assert.equal((await f.run()).code, 0);
  assert.equal(fs.statSync(f.outputFile).mode & 0o777, 0o600);
  assert.deepEqual(JSON.parse(fs.readFileSync(f.outputFile, "utf8")), BACKUP);
});

test("the ungrouped scope is passed as the server's literal sentinel", async (t) => {
  const f = await fixture(t);
  assert.equal((await f.run(["--group", "ungrouped"])).code, 0);
  assert.deepEqual(JSON.parse(f.requests[1].rawBody), {
    password: PASSWORD, filters: { group: "ungrouped" },
  });
});

// Private backup contract: owning skill/reference requires private replacement,
// unchanged linked files and preservation of an existing backup on disk failure.
for (const kind of ["symbolic", "hard"]) {
  test(`${kind} output links cannot overwrite or expose another file`, async (t) => {
    const f = await fixture(t);
    const target = path.join(path.dirname(f.outputFile), "unrelated.json");
    fs.writeFileSync(target, "unrelated content", { mode: 0o644 });
    if (kind === "symbolic") fs.symlinkSync(target, f.outputFile);
    else fs.linkSync(target, f.outputFile);
    const result = await f.run();
    assert.equal(result.code, 0, result.stderr);
    assert.equal(fs.readFileSync(target, "utf8"), "unrelated content");
    assert.equal(fs.statSync(target).mode & 0o777, 0o644);
    assert.equal(fs.lstatSync(f.outputFile).isSymbolicLink(), false);
    assert.equal(fs.statSync(f.outputFile).mode & 0o777, 0o600);
    assert.deepEqual(JSON.parse(fs.readFileSync(f.outputFile, "utf8")), BACKUP);
  });
}

test("a partial disk write preserves the previous backup and removes staging files", async (t) => {
  const f = await fixture(t);
  const directory = path.dirname(f.outputFile);
  const preload = path.join(directory, "fail-write.cjs");
  fs.writeFileSync(f.outputFile, "previous backup", { mode: 0o644 });
  fs.writeFileSync(preload, `
    const fs = require('node:fs');
    const write = fs.writeFileSync;
    fs.writeFileSync = function(file, data, options) {
      if (String(data).includes('fixture-exported-secret')) {
        write(file, String(data).slice(0, 20), options);
        throw new Error('fixture storage failure');
      }
      return write(file, data, options);
    };
  `);
  const before = fs.readdirSync(directory).sort();
  const result = await f.run([], { NODE_OPTIONS: `--require=${preload}` });
  assert.equal(result.code, 1);
  assert.match(result.stderr, /fixture storage failure/);
  assert.equal(result.stdout, "");
  assert.equal(fs.readFileSync(f.outputFile, "utf8"), "previous backup");
  assert.equal(fs.statSync(f.outputFile).mode & 0o777, 0o644);
  assert.deepEqual(fs.readdirSync(directory).sort(), before);
});

test("stdout export remains available when no output file is requested", async (t) => {
  const f = await fixture(t);
  const result = await f.invoke(["accounts", "export", "--confirmation-file", f.confirmationFile]);
  assert.equal(result.code, 0, result.stderr);
  assert.deepEqual(JSON.parse(result.stdout), BACKUP);
  assert.ok(!fs.existsSync(f.outputFile));
});

for (const [name, env] of [
  ["API key only", { SUB2API_JWT: "", SUB2API_ADMIN_API_KEY: "fixture-key" }],
  ["API key and JWT together", { SUB2API_ADMIN_API_KEY: "fixture-key" }],
  ["no JWT", { SUB2API_JWT: "" }],
]) {
  test(`${name} cannot authorize export or trigger a request`, async (t) => {
    const f = await fixture(t);
    const result = await f.run([], env);
    f.assertNoExport(result);
    assert.match(result.stderr, /requires SUB2API_JWT/);
    assert.equal(f.requests.length, 0);
  });
}

test("other commands retain API-key-first authentication", async (t) => {
  const f = await fixture(t);
  const result = await f.invoke(["accounts", "list"], { SUB2API_ADMIN_API_KEY: "fixture-key" });
  assert.equal(result.code, 0, result.stderr);
  assert.equal(f.requests[0].headers["x-api-key"], "fixture-key");
  assert.equal(f.requests[0].headers.authorization, undefined);
});

for (const [name, args] of [
  ["mixed IDs and filters", ["--ids", "40", "--platform", "openai"]],
  ["empty IDs", ["--ids", ","]],
  ["zero ID", ["--ids", "0"]],
  ["negative ID", ["--ids", "-1"]],
  ["fractional ID", ["--ids", "1.5"]],
  ["unsafe integer ID", ["--ids", "9007199254740992"]],
  ["nonnumeric ID", ["--ids", "abc"]],
  ["missing list entry", ["--ids", "40,,39"]],
  ["missing IDs value", ["--ids"]],
  ["repeated IDs", ["--ids", "40", "--ids", "39"]],
  ["unknown scope option", ["--platforms", "openai"]],
  ["missing filter value", ["--platform"]],
  ["invalid proxy boolean", ["--include-proxies", "sometimes"]],
  ["missing output path", ["--file"]],
  ["inline confirmation", ["--password", "fixture-inline-secret"]],
  ["positional account ID", ["40"]],
]) {
  test(`${name} fails before any export request`, async (t) => {
    const f = await fixture(t);
    f.assertNoExport(await f.run(args));
    assert.equal(f.requests.length, 0);
  });
}

test("a confirmation file is mandatory", async (t) => {
  const f = await fixture(t);
  f.assertNoExport(await f.invoke(["accounts", "export", "--file", f.outputFile]));
  assert.equal(f.requests.length, 0);
});

for (const [name, content] of [
  ["malformed JSON containing a password", '{"password":"fixture-parser-secret", invalid}'],
  ["null", "null"],
  ["array", "[]"],
  ["missing password", "{}"],
  ["numeric password", '{"password":123456}'],
  ["blank password", '{"password":"   "}'],
  ["numeric TOTP", JSON.stringify({ password: PASSWORD, totp_code: 123456 })],
]) {
  test(`${name} cannot expose confirmation or contact the server`, async (t) => {
    const f = await fixture(t);
    fs.writeFileSync(f.confirmationFile, content);
    const result = await f.run();
    f.assertNoExport(result);
    assert.equal(f.requests.length, 0);
    assert.ok(!result.stderr.includes("fixture-parser-secret"));
  });
}

test("an unreadable confirmation file fails without a request", async (t) => {
  const f = await fixture(t);
  fs.unlinkSync(f.confirmationFile);
  f.assertNoExport(await f.run());
  assert.equal(f.requests.length, 0);
});

for (const code of [undefined, "", "   "]) {
  test(`required TOTP ${JSON.stringify(code)} stops before POST`, async (t) => {
    const f = await fixture(t, { stepUp: true });
    fs.writeFileSync(f.confirmationFile, JSON.stringify({ password: PASSWORD, totp_code: code }));
    f.assertNoExport(await f.run());
    assert.equal(f.requests.length, 1);
    assert.equal(f.requests[0].method, "GET");
  });
}

for (const data of [{}, { step_up_required: "false" }, null]) {
  test(`invalid requirements ${JSON.stringify(data)} fail closed`, async (t) => {
    const f = await fixture(t, { requirementsResponse: { code: 0, data } });
    f.assertNoExport(await f.run());
    assert.equal(f.requests.length, 1);
  });
}

for (const status of [401, 403, 429, 503]) {
  test(`requirements HTTP ${status} prevents POST and does not retry`, async (t) => {
    const f = await fixture(t, {
      requirementsStatus: status,
      requirementsResponse: { code: status, message: "Confirmation unavailable" },
    });
    f.assertNoExport(await f.run());
    assert.equal(f.requests.length, 1);
  });
  test(`export HTTP ${status} preserves the previous backup and does not retry`, async (t) => {
    const f = await fixture(t, {
      exportStatus: status,
      exportResponse: { code: status, message: "Confirmation denied" },
    });
    fs.writeFileSync(f.outputFile, "previous backup");
    const result = await f.run();
    assert.equal(result.code, 1);
    assert.equal(result.stdout, "");
    assert.equal(fs.readFileSync(f.outputFile, "utf8"), "previous backup");
    assert.equal(f.requests.length, 2);
  });
}

test("server business errors remain authoritative after a successful requirements response", async (t) => {
  const f = await fixture(t, { exportResponse: { code: "EXPORT_CONFIRMATION_FAILED", message: "Confirmation failed" } });
  const result = await f.run();
  assert.equal(result.code, 1);
  assert.equal(result.stdout, "");
  assert.ok(!fs.existsSync(f.outputFile));
  assert.equal(f.requests.length, 2);
});

test("an invalid backup response is not written as a successful export", async (t) => {
  const f = await fixture(t, { exportResponse: { code: 0, data: {} } });
  assert.equal((await f.run()).code, 1);
  assert.ok(!fs.existsSync(f.outputFile));
  assert.equal(f.requests.length, 2);
});
