// External User OAuth administration + actual Basic AK/SK + separate machine
// JWT/resource realm. No token, SK, password, nonce or raw browser error in logs.
const n = require("../support/native-fixtures.cjs"),
  assert = require("node:assert/strict"),
  { execFileSync } = require("node:child_process");
let phase = "startup",
  checks = 0;
let orphanFixture;
const started = new Date().toISOString();
const sdkProbe = process.env.SSO_MACHINE_VERIFIER_PROBE;
let sdkTenant,
  sdkApplication,
  sampleProcess,
  sampleStopped = true,
  sampleExit = Promise.resolve(),
  sdkChecks = 0;
const sampleLogs = [];
async function startReferenceResource(tenant, application) {
  sdkTenant = tenant;
  sdkApplication = application;
  if (!process.env.SSO_MACHINE_RESOURCE_EXAMPLE) return;
  const { spawn } = require("node:child_process");
  sampleProcess = spawn(process.env.SSO_MACHINE_RESOURCE_EXAMPLE, [], {
    cwd: n.root,
    env: {
      ...process.env,
      SSO_API_ORIGIN: "http://api.localhost:8083",
      SSO_ALLOW_LOCAL_HTTP: "true",
      SSO_EXPECTED_TENANT_ID: tenant,
      SSO_EXPECTED_APPLICATION_ID: application,
    },
    stdio: ["ignore", "pipe", "pipe"],
  });
  sampleStopped = false;
  sampleExit = new Promise((resolve) => {
    sampleProcess.once("exit", () => {
      sampleStopped = true;
      resolve();
    });
    sampleProcess.once("error", () => {
      sampleStopped = true;
      resolve();
    });
  });
  await new Promise((resolve, reject) => {
    const timer = setTimeout(
      () => reject(new Error("reference resource startup timed out")),
      4000,
    );
    let ready = false;
    sampleProcess.on("error", () => {
      clearTimeout(timer);
      reject(new Error("reference resource startup failed"));
    });
    sampleProcess.on("exit", () => {
      if (!ready) {
        clearTimeout(timer);
        reject(
          new Error("reference resource exited before listener ownership"),
        );
      }
    });
    const collect = (data) => {
      sampleLogs.push(Buffer.from(data));
      if (
        Buffer.concat(sampleLogs).includes(
          Buffer.from("reference machine resource ready on 127.0.0.1:8092"),
        )
      ) {
        ready = true;
        clearTimeout(timer);
        resolve();
      }
    };
    sampleProcess.stdout.on("data", collect);
    sampleProcess.stderr.on("data", collect);
  });
  for (const [method, path, status] of [
    ["GET", "/whoami", 401],
    ["GET", "/whoami?tenant=untrusted", 400],
    ["POST", "/whoami", 405],
    ["GET", "/unknown", 404],
  ]) {
    const r = await n.request(8092, "127.0.0.1:8092", method, path, undefined, {
      Cookie: "user-session=does-not-authorize",
    });
    assert.equal(r.status, status);
    assert.equal(r.headers["cache-control"], "no-store");
    if (method === "GET" && path.split("?")[0] === "/whoami")
      validateReferenceResponse("/whoami", "GET", r);
    sdkChecks++;
  }
  for (const suffix of ["json", "yaml"]) {
    const path = "/openapi." + suffix;
    const r = await n.request(8092, "127.0.0.1:8092", "GET", path);
    assert.equal(r.status, 200);
    assert.equal(r.headers["cache-control"], "no-store");
    validateReferenceResponse(path, "GET", r);
    sdkChecks++;
  }
}
function validateReferenceResponse(path, method, r) {
  execFileSync("python3", ["tests/api/validate_contract.py"], {
    cwd: n.root,
    input: JSON.stringify({
      contract: "machine-example",
      path,
      method,
      status: r.status,
      body: r.body ?? r.text,
      content_type: r.headers["content-type"],
    }),
    stdio: ["pipe", "pipe", "pipe"],
  });
}
function checkSDK(application, token, tenant = sdkTenant) {
  const result = JSON.parse(
    execFileSync(sdkProbe, [], {
      cwd: n.root,
      input: JSON.stringify({ tenant, application, token }),
      stdio: ["pipe", "pipe", "pipe"],
      timeout: 8000,
    }),
  );
  sdkChecks++;
  return result;
}
async function checkExternalResource(url, token, response) {
  const match = /^\/v1\/machine\/applications\/([a-f0-9]{32})\/identity$/.exec(
    url,
  );
  if (!sdkProbe || !match) return;
  const verified = checkSDK(match[1], token);
  assert.equal(verified.ok, response.status === 200);
  if (response.status === 200) {
    assert.deepEqual(verified.identity, response.body);
    const other = checkSDK(match[1], token, "e".repeat(32));
    assert.equal(other.ok, false);
    assert.equal(other.category, "authority_response_invalid");
  } else assert.equal(verified.category, "unauthenticated");
  if (sampleProcess && match[1] === sdkApplication) {
    assert.equal(sampleStopped, false);
    assert.equal(sampleProcess.exitCode, null);
    const mirrored = await n.request(
      8092,
      "127.0.0.1:8092",
      "GET",
      "/whoami",
      undefined,
      { Authorization: "Bearer " + token },
    );
    assert.equal(mirrored.status, response.status);
    validateReferenceResponse("/whoami", "GET", mirrored);
    assert.equal(mirrored.headers["cache-control"], "no-store");
    if (response.status === 200) assert.deepEqual(mirrored.body, response.body);
    sdkChecks++;
  }
}
function checkReferenceClient(credential, secret, success) {
  if (!sdkProbe) return;
  const { spawnSync } = require("node:child_process");
  const r = spawnSync(
    process.execPath,
    [n.root + "/examples/machine-client.cjs"],
    {
      cwd: n.root,
      input: JSON.stringify({
        access_key: credential.access_key,
        secret_key: secret,
        application_id: credential.application_id,
      }),
      stdio: ["pipe", "pipe", "pipe"],
      timeout: 10000,
    },
  );
  const output = Buffer.concat([
    r.stdout || Buffer.alloc(0),
    r.stderr || Buffer.alloc(0),
  ]);
  for (const value of secrets) assert(!output.includes(Buffer.from(value)));
  if (success) {
    assert.equal(r.status, 0);
    const identity = JSON.parse(r.stdout);
    assert.equal(identity.credential_id, credential.id);
    assert.equal(identity.tenant_id, sdkTenant);
    assert.equal(identity.principal_type, "service_principal");
  } else {
    assert.notEqual(r.status, 0);
    assert.equal(r.stdout.length, 0);
    assert.match(
      r.stderr.toString(),
      /^Machine client reference failed: token_exchange HTTP 403 \(private details withheld\)\s*$/,
    );
  }
  sdkChecks++;
}
async function stopReferenceResource() {
  if (sampleProcess && !sampleStopped) {
    sampleProcess.kill("SIGTERM");
    let timer;
    await Promise.race([
      sampleExit,
      new Promise((resolve) => {
        timer = setTimeout(() => {
          if (!sampleStopped) sampleProcess.kill("SIGKILL");
          resolve();
        }, 2000);
      }),
    ]);
    clearTimeout(timer);
    await sampleExit;
  }
}

const covered = new Set(),
  secrets = [];
const progress = setInterval(
  () => console.log("Machine native acceptance phase: " + phase),
  20000,
);
function normalized(url) {
  return url
    .split("?")[0]
    .replace(/\/tenants\/[a-f0-9]{32}/, "/tenants/{tenant}")
    .replace(/\/applications\/[a-f0-9]{32}/, "/applications/{application}")
    .replace(
      /\/service-principals\/[a-f0-9]{32}/,
      "/service-principals/{principal}",
    )
    .replace(/\/credentials\/[a-f0-9]{32}/, "/credentials/{credential}");
}
async function call(method, url, token, body, status = 200, etag, contract) {
  const h = {};
  if (token) h.Authorization = "Bearer " + token;
  if (body !== undefined) h["Content-Type"] = "application/json";
  if (etag) h["If-Match"] = etag;
  const r = await n.request(
    8083,
    "api.localhost:8083",
    method,
    url,
    body === undefined ? undefined : JSON.stringify(body),
    h,
  );
  if (
    !(Array.isArray(status) ? status.includes(r.status) : r.status === status)
  )
    console.error(
      "Unexpected status: " +
        method +
        " " +
        normalized(url) +
        " expected=" +
        JSON.stringify(status) +
        " received=" +
        r.status,
    );
  assert(
    Array.isArray(status) ? status.includes(r.status) : r.status === status,
  );
  execFileSync("python3", ["tests/api/validate_contract.py"], {
    cwd: n.root,
    input: JSON.stringify({
      contract,
      path: normalized(url),
      method,
      status: r.status,
      body: r.body,
      request_body: body,
      content_type: r.headers["content-type"],
      request_id: r.headers["x-request-id"],
    }),
    stdio: ["pipe", "pipe", "pipe"],
  });
  await checkExternalResource(url, token, r);
  checks++;
  covered.add(method.toLowerCase() + " " + normalized(url));
  return r;
}
async function token(key, secret, audience, status = 200, extra = {}) {
  const body = {
    grant_type: "client_credentials",
    scope: "machine:invoke",
    audience,
    ...extra,
  };
  const r = await n.request(
    4464,
    "auth.localhost:4464",
    "POST",
    "/oauth2/token",
    new URLSearchParams(body).toString(),
    {
      "Content-Type": "application/x-www-form-urlencoded",
      Authorization:
        "Basic " + Buffer.from(key + ":" + secret).toString("base64"),
    },
  );
  if (
    !(Array.isArray(status) ? status.includes(r.status) : r.status === status)
  )
    console.error(
      "Unexpected machine token status expected=" +
        JSON.stringify(status) +
        " received=" +
        r.status,
    );
  assert(
    Array.isArray(status) ? status.includes(r.status) : r.status === status,
  );
  execFileSync("python3", ["tests/api/validate_contract.py"], {
    cwd: n.root,
    input: JSON.stringify({
      contract: "machine",
      path: "/oauth2/token",
      method: "POST",
      status: r.status,
      body: r.body,
      request_body: body,
      content_type: r.headers["content-type"],
    }),
    stdio: ["pipe", "pipe", "pipe"],
  });
  checks++;
  if (r.status === 200) {
    assert(r.body.access_token);
    secrets.push(r.body.access_token);
    assert(!r.body.refresh_token);
    assert(!r.body.id_token);
  } else assert(!r.body?.access_token);
  return r;
}
function fixture(action, app, principal, credential, secret) {
  execFileSync(
    "python3",
    ["tests/machine/fixture.py", action, app, principal, credential],
    {
      cwd: n.root,
      input: secret ? JSON.stringify({ secret }) : undefined,
      stdio: ["pipe", "pipe", "pipe"],
    },
  );
}
function privateAbsent(value) {
  const text = JSON.stringify(value);
  for (const sk of secrets) assert(!text.includes(sk));
  assert(!Object.hasOwn(value, "secret_key"));
}
(async () => {
  try {
    await n.start();
    const owner = await n.register();
    let ownerToken = (await n.authorize(owner)).access_token;
    secrets.push(ownerToken);
    const me = (await call("GET", "/v1/me", ownerToken)).body;
    const tenant = me.tenant_id,
      base = "/v1/tenants/" + tenant;
    phase = "owned-application-principal-and-first-one-time-key";
    const app = (
      await call(
        "POST",
        base + "/applications",
        ownerToken,
        { name: "Machine native owned app" },
        201,
      )
    ).body;
    const appPath = base + "/applications/" + app.id,
      spBase = appPath + "/service-principals";
    if (sdkProbe) await startReferenceResource(tenant, app.id);
    let principal = await call(
      "POST",
      spBase,
      ownerToken,
      { name: "Automation worker" },
      201,
    );
    const spPath = spBase + "/" + principal.body.id;
    await call("GET", spBase + "?limit=1", ownerToken);
    await call("GET", spPath, ownerToken);
    const keys = spPath + "/credentials";
    let first = await call(
      "POST",
      keys,
      ownerToken,
      { name: "Primary key", expires_in_days: 90 },
      201,
    );
    const firstKey = first.body.credential,
      firstPath = keys + "/" + firstKey.id;
    secrets.push(first.body.secret_key);
    assert.equal(Buffer.from(first.body.secret_key, "base64url").length, 32);
    assert.equal(firstKey.state, "active");
    let firstMeta = await call("GET", firstPath, ownerToken);
    privateAbsent(firstMeta.body);
    privateAbsent((await call("GET", keys, ownerToken)).body);
    checkReferenceClient(firstKey, first.body.secret_key, true);
    const resource = "/v1/machine/applications/" + app.id + "/identity";
    const audience = firstKey.audience;
    phase = "real-m2m-token-separate-audience-resource-and-hints";
    phase = "real-orphan-client-fails-primary-hook";
    const crypto = require("node:crypto"),
      orphan = crypto.randomBytes(16).toString("hex"),
      orphanSecret = crypto.randomBytes(32).toString("base64url");
    secrets.push(orphanSecret);
    orphanFixture = { app: app.id, principal: principal.body.id, id: orphan };
    fixture("orphan-create", app.id, principal.body.id, orphan, orphanSecret);
    await token("ak_" + orphan, orphanSecret, audience, 403);
    fixture("orphan-delete", app.id, principal.body.id, orphan);
    orphanFixture = null;
    await token(
      "ak_" + crypto.randomBytes(16).toString("hex"),
      orphanSecret,
      audience,
      401,
    );
    phase = "real-m2m-token-separate-audience-resource-and-hints";
    const firstToken = await token(
      firstKey.access_key,
      first.body.secret_key,
      audience,
    );
    const signed = JSON.parse(
      Buffer.from(
        firstToken.body.access_token.split(".")[1],
        "base64url",
      ).toString(),
    );
    for (const field of [
      "tenant_authz_version",
      "application_version",
      "enabled_version",
      "principal_version",
      "credential_version",
      "credential_expires_at",
    ])
      assert.match(signed.ext[field], /^[1-9][0-9]*$/);
    await call(
      "GET",
      resource,
      firstToken.body.access_token,
      undefined,
      200,
      undefined,
      "machine",
    );
    await call("GET", "/v1/me", firstToken.body.access_token, undefined, 401);
    await call(
      "GET",
      resource,
      ownerToken,
      undefined,
      401,
      undefined,
      "machine",
    );
    await token(firstKey.access_key, "incorrect-secret", audience, 401);
    await token(
      firstKey.access_key,
      first.body.secret_key,
      "https://api.sso.local",
      [400, 403],
    );
    await token(
      firstKey.access_key,
      first.body.secret_key,
      audience,
      [400, 403],
      { scope: "platform:write" },
    );
    const hinted = await token(
      firstKey.access_key,
      first.body.secret_key,
      audience,
      200,
      { tenant_id: "f".repeat(32), principal_id: "e".repeat(32) },
    );
    const identity = await call(
      "GET",
      resource,
      hinted.body.access_token,
      undefined,
      200,
      undefined,
      "machine",
    );
    assert.equal(identity.body.tenant_id, tenant);
    assert.equal(identity.body.service_principal_id, principal.body.id);
    phase = "second-key-bounded-overlap-and-old-jwt-retention";
    let second = await call(
      "POST",
      keys,
      ownerToken,
      { name: "Rotated key", expires_in_days: 90 },
      201,
    );
    secrets.push(second.body.secret_key);
    const secondKey = second.body.credential,
      secondPath = keys + "/" + secondKey.id;
    await call(
      "POST",
      keys,
      ownerToken,
      { name: "Over quota", expires_in_days: 90 },
      409,
    );
    firstMeta = await call("GET", firstPath, ownerToken);
    assert.equal(firstMeta.body.version, 3);
    assert(firstMeta.body.expires_at <= Math.floor(Date.now() / 1000) + 86401);
    await call(
      "GET",
      resource,
      firstToken.body.access_token,
      undefined,
      200,
      undefined,
      "machine",
    );
    const secondToken = await token(
      secondKey.access_key,
      second.body.secret_key,
      audience,
    );
    await call(
      "POST",
      firstPath + "/revoke",
      ownerToken,
      {},
      412,
      first.headers.etag,
    );
    const revokedFirst = await call(
      "POST",
      firstPath + "/revoke",
      ownerToken,
      {},
      200,
      firstMeta.headers.etag,
    );
    checkReferenceClient(firstKey, first.body.secret_key, false);
    await call(
      "GET",
      resource,
      firstToken.body.access_token,
      undefined,
      401,
      undefined,
      "machine",
    );
    await token(firstKey.access_key, first.body.secret_key, audience, 403);
    await call(
      "GET",
      resource,
      secondToken.body.access_token,
      undefined,
      200,
      undefined,
      "machine",
    );
    await call(
      "POST",
      firstPath + "/cleanup",
      ownerToken,
      {},
      200,
      revokedFirst.headers.etag,
    );
    phase = "sp-pause-restore-new-token-and-no-resurrection";
    principal = await call("GET", spPath, ownerToken);
    const disabled = await call(
      "POST",
      spPath + "/disable",
      ownerToken,
      {},
      200,
      principal.headers.etag,
    );
    await call(
      "GET",
      resource,
      secondToken.body.access_token,
      undefined,
      401,
      undefined,
      "machine",
    );
    await token(secondKey.access_key, second.body.secret_key, audience, 403);
    const restored = await call(
      "POST",
      spPath + "/enable",
      ownerToken,
      {},
      200,
      disabled.headers.etag,
    );
    assert(restored.body.enabled);
    await call(
      "GET",
      resource,
      secondToken.body.access_token,
      undefined,
      401,
      undefined,
      "machine",
    );
    let current = await token(
      secondKey.access_key,
      second.body.secret_key,
      audience,
    );
    await call(
      "GET",
      resource,
      current.body.access_token,
      undefined,
      200,
      undefined,
      "machine",
    );
    phase = "application-pause-restore-and-namespace-isolation";
    const appDetail = await call("GET", appPath, ownerToken);
    const paused = await call(
      "POST",
      appPath + "/disable",
      ownerToken,
      {},
      200,
      appDetail.headers.etag,
    );
    await token(secondKey.access_key, second.body.secret_key, audience, 403);
    await call(
      "GET",
      resource,
      current.body.access_token,
      undefined,
      401,
      undefined,
      "machine",
    );
    await call(
      "POST",
      appPath + "/enable",
      ownerToken,
      {},
      200,
      paused.headers.etag,
    );
    await call(
      "GET",
      resource,
      current.body.access_token,
      undefined,
      401,
      undefined,
      "machine",
    );
    current = await token(
      secondKey.access_key,
      second.body.secret_key,
      audience,
    );
    await call(
      "GET",
      resource,
      current.body.access_token,
      undefined,
      200,
      undefined,
      "machine",
    );
    const foreignApp = (
      await call(
        "POST",
        base + "/applications",
        ownerToken,
        { name: "Other machine resource" },
        201,
      )
    ).body;
    await call(
      "GET",
      "/v1/machine/applications/" + foreignApp.id + "/identity",
      current.body.access_token,
      undefined,
      401,
      undefined,
      "machine",
    );
    phase = "machine-audit-not-human-actor-and-metadata-secrets";
    const events = await call(
      "GET",
      base + "/machine-events?limit=1",
      ownerToken,
    );
    assert.equal(events.body.items.length, 1);
    assert(events.body.next_cursor);
    assert.equal(events.body.items[0].service_principal_id, principal.body.id);
    assert(!Object.hasOwn(events.body.items[0], "actor_membership_id"));
    privateAbsent(events.body);
    await call(
      "GET",
      base + "/machine-events?after=" + events.body.next_cursor,
      ownerToken,
    );
    privateAbsent((await call("GET", keys, ownerToken)).body);
    phase = "final-revocation-cleanup-retains-user-login";
    phase = "pending-primary-state-blocks-existing-engine-key";
    const pendingSP = await call(
        "POST",
        spBase,
        ownerToken,
        { name: "Pending failure probe" },
        201,
      ),
      pendingSPPath = spBase + "/" + pendingSP.body.id;
    const pendingCreated = await call(
        "POST",
        pendingSPPath + "/credentials",
        ownerToken,
        { name: "Uncertain activation", expires_in_days: 1 },
        201,
      ),
      pendingKey = pendingCreated.body.credential,
      pendingPath = pendingSPPath + "/credentials/" + pendingKey.id;
    secrets.push(pendingCreated.body.secret_key);
    const beforePending = await token(
      pendingKey.access_key,
      pendingCreated.body.secret_key,
      pendingKey.audience,
    );
    fixture("pending", app.id, pendingSP.body.id, pendingKey.id);
    const pendingMeta = await call("GET", pendingPath, ownerToken);
    assert.equal(pendingMeta.body.state, "pending");
    assert.equal(pendingMeta.body.active, false);
    await token(
      pendingKey.access_key,
      pendingCreated.body.secret_key,
      pendingKey.audience,
      403,
    );
    await call(
      "GET",
      resource,
      beforePending.body.access_token,
      undefined,
      401,
      undefined,
      "machine",
    );
    await call(
      "POST",
      pendingPath + "/cleanup",
      ownerToken,
      {},
      409,
      pendingMeta.headers.etag,
    );
    const pendingRevoked = await call(
      "POST",
      pendingPath + "/revoke",
      ownerToken,
      {},
      200,
      pendingMeta.headers.etag,
    );
    await call(
      "POST",
      pendingPath + "/cleanup",
      ownerToken,
      {},
      200,
      pendingRevoked.headers.etag,
    );
    phase = "credential-expiry-blocks-cached-jwt-and-future-exchange";
    fixture("expire", app.id, principal.body.id, secondKey.id);
    const expired = await call("GET", secondPath, ownerToken);
    assert.equal(expired.body.state, "expired");
    assert.equal(expired.body.active, false);
    await token(secondKey.access_key, second.body.secret_key, audience, 403);
    await call(
      "GET",
      resource,
      current.body.access_token,
      undefined,
      401,
      undefined,
      "machine",
    );
    const secondMeta = await call("GET", secondPath, ownerToken);
    const revoked = await call(
      "POST",
      secondPath + "/revoke",
      ownerToken,
      {},
      200,
      secondMeta.headers.etag,
    );
    await call(
      "GET",
      resource,
      current.body.access_token,
      undefined,
      401,
      undefined,
      "machine",
    );
    await token(secondKey.access_key, second.body.secret_key, audience, 403);
    await call(
      "POST",
      secondPath + "/cleanup",
      ownerToken,
      {},
      200,
      revoked.headers.etag,
    );
    await call("GET", "/v1/me", ownerToken);
    const spec = require("../../api/openapi.json");
    for (const [route, operations] of Object.entries(spec.paths))
      if (
        route.includes("/service-principals") ||
        route.endsWith("/machine-events")
      )
        for (const method of Object.keys(operations))
          assert(covered.has(method + " " + route));
    phase = "private-material-public-file-and-runtime-log-exclusion";
    for (const secret of secrets)
      assert(!Buffer.concat(sampleLogs).includes(Buffer.from(secret)));
    assert(
      !/eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}/.test(
        Buffer.concat(sampleLogs).toString("utf8"),
      ),
    );
    if (sdkProbe)
      console.log(
        "Machine external Go SDK acceptance passed: " +
          sdkChecks +
          " online verifier/reference resource checks; no cache, wrong tenant, User-cookie/realm refusal and live revocation/restoration/expiry",
      );
    const privacy = require("../support/private-material.cjs")(
      n.root,
      secrets,
      started,
    );
    console.log(
      "Machine native private-material exclusion passed: " +
        privacy.files +
        " candidate files and runtime logs",
    );
    console.log(
      "Machine native acceptance passed: " +
        checks +
        " real contract responses; external User administration, one-time SK metadata exclusion, actual Basic M2M/JWT/resource, scope/audience/realm/hint bounds, dual-key overlap, revocation, SP/app restoration, audit and cleanup",
    );
  } finally {
    clearInterval(progress);
    try {
      if (orphanFixture)
        fixture(
          "orphan-delete",
          orphanFixture.app,
          orphanFixture.principal,
          orphanFixture.id,
        );
    } finally {
      try {
        await stopReferenceResource();
      } finally {
        await n.close();
      }
    }
  }
})().catch((e) => {
  console.error(
    "Machine native failed: phase=" +
      phase +
      ", error=" +
      e.name +
      " (private details withheld)",
  );
  console.error(JSON.stringify(n.diagnostics()));
  process.exitCode = 1;
});
