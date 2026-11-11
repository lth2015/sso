// Actual User PKCE, primary audit projections and independent machine issuance.
const n = require("../support/native-fixtures.cjs"),
  assert = require("node:assert/strict"),
  crypto = require("node:crypto"),
  { execFileSync } = require("node:child_process");
let phase = "startup",
  checks = 0;
const privateValues = [],
  started = new Date().toISOString();
const progress = setInterval(
  () => console.log("Audit export native phase: " + phase),
  20000,
);
async function call(method, path, token, body, status = 200, etag) {
  const r = await n.api(
    method,
    path,
    token,
    body,
    etag ? { "If-Match": etag } : {},
    status,
  );
  checks++;
  return r;
}
async function authorize(f, tenant, scopes, force = false) {
  const tokens = await n.authorize(f, tenant, scopes, force);
  privateValues.push(
    ...[tokens.access_token, tokens.refresh_token, tokens.id_token].filter(
      (v) => typeof v === "string" && v.length >= 32,
    ),
  );
  return tokens.access_token;
}
function privateAbsent(value) {
  const text = JSON.stringify(value);
  for (const v of privateValues) assert(!text.includes(v));
}
(async () => {
  try {
    await n.start();
    const owner = await n.register();
    let token = await authorize(owner);
    let me = (await call("GET", "/v1/me", token)).body;
    const tenant = me.tenant_id,
      base = "/v1/tenants/" + tenant,
      path = base + "/audit-exports";
    phase = "default-envelope-bounds-and-empty-machine-projection";
    const initial = await call("GET", path, token);
    assert.equal(initial.body.kind, "management");
    assert.equal(initial.body.format, "sso.audit.batch.v1");
    assert.equal(initial.body.tenant_id, tenant);
    assert.equal(initial.headers["cache-control"], "no-store");
    assert(initial.body.authz_version > 0);
    const empty = await call("GET", path + "?kind=machine", token);
    assert.deepEqual(empty.body.items, []);
    assert.equal(empty.body.next_cursor, null);
    for (const query of [
      "?kind=users",
      "?kind=",
      "?kind=machine&kind=management",
      "?limit=0",
      "?limit=101",
      "?limit=1&limit=2",
      "?after=invalid",
      "?tenant_id=" + tenant,
      "?kind=management&format=csv",
      "?after=",
    ])
      await call("GET", path + query, token, undefined, 400);
    await call("GET", path + "?after=" + "0".repeat(32), token, undefined, 404);
    await call(
      "GET",
      path + "?after=" + crypto.randomBytes(16).toString("hex"),
      token,
      undefined,
      404,
    );
    await call("GET", path, token, { tenant_id: tenant }, 400);
    await call("GET", path, undefined, undefined, 401);
    phase = "bounded-pagination-and-private-field-exclusion";
    for (let i = 0; i < 6; i++) {
      const r = await call(
        "POST",
        base + "/invitations",
        token,
        {
          email:
            "export-" +
            crypto.randomBytes(8).toString("hex") +
            "@example.invalid",
        },
        201,
      );
      privateValues.push(r.body.token);
    }
    const ids = new Set();
    let cursor, first;
    for (let page = 0; page < 30; page++) {
      const r = await call(
        "GET",
        path + "?limit=2" + (cursor ? "&after=" + cursor : ""),
        token,
      );
      assert(r.body.items.length <= 2);
      privateAbsent(r.body);
      for (const event of r.body.items) {
        assert(!ids.has(event.id));
        ids.add(event.id);
        assert.deepEqual(
          Object.keys(event).sort(),
          [
            "action",
            "actor_membership_id",
            "id",
            "membership_id",
            "occurred_at",
            "role_code",
            "role_id",
            "target_id",
          ].sort(),
        );
        first ??= event.id;
      }
      if (!r.body.next_cursor) break;
      assert.equal(r.body.next_cursor, r.body.items.at(-1).id);
      cursor = r.body.next_cursor;
    }
    assert(ids.size >= 7);
    phase = "real-machine-event-no-human-actor-and-source-cursor-isolation";
    const app = (
      await call(
        "POST",
        base + "/applications",
        token,
        { name: "Audit export native app" },
        201,
      )
    ).body;
    const spBase = base + "/applications/" + app.id + "/service-principals";
    const sp = (
      await call("POST", spBase, token, { name: "Export event worker" }, 201)
    ).body;
    const credential = (
      await call(
        "POST",
        spBase + "/" + sp.id + "/credentials",
        token,
        { name: "Export test key", expires_in_days: 1 },
        201,
      )
    ).body;
    privateValues.push(credential.secret_key);
    const exchanged = await n.request(
      4464,
      "auth.localhost:4464",
      "POST",
      "/oauth2/token",
      new URLSearchParams({
        grant_type: "client_credentials",
        scope: "machine:invoke",
        audience: credential.credential.audience,
      }).toString(),
      {
        "Content-Type": "application/x-www-form-urlencoded",
        Authorization:
          "Basic " +
          Buffer.from(
            credential.credential.access_key + ":" + credential.secret_key,
          ).toString("base64"),
      },
    );
    assert.equal(exchanged.status, 200);
    privateValues.push(exchanged.body.access_token);
    const machine = (await call("GET", path + "?kind=machine&limit=1", token))
      .body;
    assert.equal(machine.items.length, 1);
    assert.equal(machine.items[0].service_principal_id, sp.id);
    assert(!Object.hasOwn(machine.items[0], "actor_membership_id"));
    privateAbsent(machine);
    await call(
      "GET",
      path + "?kind=machine&after=" + first,
      token,
      undefined,
      404,
    );
    await call(
      "GET",
      path + "?after=" + machine.items[0].id,
      token,
      undefined,
      404,
    );
    await call("GET", path, exchanged.body.access_token, undefined, 401);
    phase = "foreign-cursor-and-path-context";
    const foreign = await n.register(),
      foreignToken = await authorize(foreign);
    const other = (await call("GET", "/v1/me", foreignToken)).body;
    const otherBatch = (
      await call(
        "GET",
        "/v1/tenants/" + other.tenant_id + "/audit-exports",
        foreignToken,
      )
    ).body;
    await call(
      "GET",
      path + "?after=" + otherBatch.items[0].id,
      token,
      undefined,
      404,
    );
    await call(
      "GET",
      "/v1/tenants/" + other.tenant_id + "/audit-exports",
      token,
      undefined,
      404,
    );
    phase = "delegated-auditor-and-live-explicit-deny";
    const guest = await n.register();
    let guestToken = await authorize(guest);
    const invitation = (
      await call(
        "POST",
        base + "/invitations",
        token,
        { email: guest.email },
        201,
      )
    ).body;
    privateValues.push(invitation.token);
    await call(
      "POST",
      "/v1/invitations/accept",
      guestToken,
      { token: invitation.token },
      201,
    );
    await call("GET", path, token, undefined, 401);
    token = await authorize(owner, tenant);
    guestToken = await authorize(guest, tenant);
    await call("GET", path, guestToken, undefined, 403);
    let members = (await call("GET", base + "/members", token)).body.items;
    const member = members.find((m) => m.email === guest.email).id;
    async function bindPolicy(code, effect) {
      const detail = await call("GET", base, token);
      const role = (
        await call(
          "POST",
          base + "/roles",
          token,
          { code, policies: [{ permission: "audit.read", effect }] },
          201,
          detail.headers.etag,
        )
      ).body;
      token = await authorize(owner, tenant);
      const latest = await call("GET", base, token);
      await call(
        "PUT",
        base + "/members/" + member + "/roles/" + role.id,
        token,
        {},
        204,
        latest.headers.etag,
      );
      token = await authorize(owner, tenant);
    }
    await bindPolicy("export_reader", "allow");
    await call("GET", path, guestToken, undefined, 401);
    guestToken = await authorize(guest, tenant);
    await call("GET", path, guestToken);
    await call("GET", path + "?kind=machine", guestToken);
    await call(
      "GET",
      spBase + "/" + sp.id + "/credentials",
      guestToken,
      undefined,
      403,
    );
    await bindPolicy("export_blocked", "deny");
    await call("GET", path, guestToken, undefined, 401);
    guestToken = await authorize(guest, tenant);
    await call("GET", path, guestToken, undefined, 403);
    await call("GET", path + "?kind=machine", guestToken, undefined, 403);
    phase = "read-scope-and-aged-mfa-no-authority-mutation";
    const readOnly = await authorize(owner, tenant, "openid platform:read");
    await call("GET", path, readOnly);
    const writeOnly = await authorize(owner, tenant, "openid platform:write");
    await call("GET", path, writeOnly, undefined, 403);
    me = (await call("GET", "/v1/me", token)).body;
    execFileSync(
      "python3",
      ["tests/api/fixtures.py", "age-session", me.user_id, me.session_id],
      { cwd: n.root, stdio: "pipe" },
    );
    const before = (await call("GET", base, token)).body.authz_version;
    const aged = await call("GET", path, token);
    assert.equal(aged.body.authz_version, before);
    await call("GET", path + "?kind=machine", token);
    const refused = await call(
      "POST",
      base + "/invitations",
      token,
      { email: "aged-export@example.invalid" },
      403,
    );
    assert.equal(refused.body.code, "recent_mfa_required");
    const privacy = require("../support/private-material.cjs")(
      n.root,
      privateValues,
      started,
    );
    console.log(
      "Audit export native passed: " +
        checks +
        " contract responses; both reviewed projections, bounded pagination, source/tenant/realm/scope/permission/MFA bounds, cursor privacy, primary live deny and private material exclusion in " +
        privacy.files +
        " files/runtime logs",
    );
  } finally {
    clearInterval(progress);
    await n.close();
  }
})().catch((e) => {
  console.error(
    "Audit export native failed: phase=" +
      phase +
      ", error=" +
      e.name +
      " (private details withheld)",
  );
  console.error(JSON.stringify(n.diagnostics()));
  process.exitCode = 1;
});
