// Real PKCE/Bearer and captured application events. Private material stays in RAM.
const n = require("../support/native-fixtures.cjs"),
  assert = require("node:assert/strict"),
  { execFileSync } = require("node:child_process");
let phase = "startup",
  checks = 0;
const secrets = [],
  started = new Date().toISOString(),
  progress = setInterval(
    () => console.log("Access history native phase: " + phase),
    20000,
  );
async function auth(f, tenant, scope, force = false) {
  const r = await n.authorize(f, tenant, scope, force);
  secrets.push(
    ...[r.access_token, r.id_token, r.refresh_token].filter(
      (v) => typeof v === "string",
    ),
  );
  return r.access_token;
}
async function call(method, url, token, body, status = 200, etag) {
  const r = await n.api(
    method,
    url,
    token,
    body,
    etag ? { "If-Match": etag } : {},
    status,
  );
  checks++;
  if (typeof r.body?.token === "string") secrets.push(r.body.token);
  if (typeof r.body?.secret_key === "string") secrets.push(r.body.secret_key);
  return r;
}
(async () => {
  try {
    await n.start();
    const owner = await n.register(),
      guest = await n.register(),
      other = await n.register();
    let token = await auth(owner),
      guestToken = await auth(guest),
      otherToken = await auth(other);
    let me = (await call("GET", "/v1/me", token)).body;
    const tenant = me.tenant_id,
      base = "/v1/tenants/" + tenant;
    const invite = (
      await call(
        "POST",
        base + "/invitations",
        token,
        { email: guest.email },
        201,
      )
    ).body;
    const member = (
      await call(
        "POST",
        "/v1/invitations/accept",
        guestToken,
        { token: invite.token },
        201,
      )
    ).body.membership_id;
    token = await auth(owner, tenant);
    guestToken = await auth(guest, tenant);
    const app = (
      await call(
        "POST",
        base + "/applications",
        token,
        { name: "Native admission history" },
        201,
      )
    ).body.id;
    const second = (
      await call(
        "POST",
        base + "/applications",
        token,
        { name: "Foreign history cursor" },
        201,
      )
    ).body.id;
    const appURL = base + "/applications/" + app,
      policy = appURL + "/user-access",
      url = policy + "/history";
    const version = async () =>
      (await call("GET", base + "/permissions", token)).headers.etag;
    const tag = async (path = policy) =>
      (await call("GET", path, token)).headers.etag;
    phase = "empty-captured-events-and-noop";
    const empty = await call("GET", url, token);
    assert.equal(empty.headers.etag, undefined);
    assert.deepEqual(empty.body.items, []);
    assert.equal(empty.body.format, "sso.application.user-access-history.v1");
    await call(
      "PUT",
      policy,
      token,
      { mode: "selected_members" },
      200,
      await tag(),
    );
    await call(
      "PUT",
      policy + "/members/" + member,
      token,
      {},
      200,
      await tag(),
    );
    await call(
      "DELETE",
      policy + "/members/" + member,
      token,
      {},
      200,
      await tag(),
    );
    await call(
      "DELETE",
      policy + "/members/" + member,
      token,
      {},
      200,
      await tag(),
    );
    const history = await call("GET", url, token);
    assert.equal(history.body.tenant_id, tenant);
    assert.equal(history.body.application_id, app);
    assert.equal(history.body.items.length, 3);
    assert.deepEqual(
      history.body.items.map((v) => v.version),
      ["2", "3", "4"],
    );
    assert.deepEqual(
      history.body.items.map((v) => v.membership_id),
      [null, member, member],
    );
    assert.deepEqual(
      history.body.items.map((v) => v.action),
      [
        "application.user_access.mode_changed",
        "application.user_access.granted",
        "application.user_access.revoked",
      ],
    );
    assert(
      history.body.items.every(
        (v) => v.actor_membership_id === me.membership_id,
      ),
    );
    const frozen = JSON.stringify(history.body);
    assert(!/eyJ[\w-]+\.[\w-]+\.[\w-]+/.test(frozen));
    for (const value of secrets) assert(!frozen.includes(value));
    const beforeAudit = (
      await call("GET", base + "/audit-events?limit=100", token)
    ).body;
    await call("GET", url, token);
    assert.deepEqual(
      (await call("GET", base + "/audit-events?limit=100", token)).body,
      beforeAudit,
    );
    phase = "actual-cursor-scope-and-strict-http";
    const first = await call("GET", url + "?limit=1", token);
    assert.equal(first.body.items.length, 1);
    assert(first.body.next_cursor);
    const next = await call(
      "GET",
      url + "?limit=1&after=" + first.body.next_cursor,
      token,
    );
    assert.equal(next.body.items[0].id, history.body.items[1].id);
    const last = await call(
      "GET",
      url + "?after=" + history.body.items[2].id,
      token,
    );
    assert.deepEqual(last.body.items, []);
    assert.equal(last.body.next_cursor, null);
    await call(
      "GET",
      base +
        "/applications/" +
        second +
        "/user-access/history?after=" +
        first.body.next_cursor,
      token,
      undefined,
      404,
    );
    await call("GET", url + "?after=" + "0".repeat(32), token, undefined, 404);
    for (const query of [
      "?after=",
      "?limit=",
      "?limit=1&limit=2",
      "?after=" + first.body.next_cursor + "&after=" + first.body.next_cursor,
      "?limit=101",
      "?limit=0",
      "?q=private",
      "?after=invalid",
    ])
      await call("GET", url + query, token, undefined, 400);
    await call("GET", url, token, {}, 400);
    await call("GET", url, undefined, undefined, 401);
    await call("GET", url, guestToken, undefined, 403);
    await call("GET", url, otherToken, undefined, 404);
    await call(
      "GET",
      base + "/applications/" + "f".repeat(32) + "/user-access/history",
      token,
      undefined,
      404,
    );
    const shared = (
      await call("GET", base + "/applications", token)
    ).body.items.find((v) => v.protected).id;
    await call(
      "GET",
      base + "/applications/" + shared + "/user-access/history",
      token,
      undefined,
      409,
    );
    const readonly = await auth(owner, tenant, "openid platform:read"),
      writeonly = await auth(owner, tenant, "openid platform:write");
    await call("GET", url, readonly);
    await call("GET", url, writeonly, undefined, 403);
    phase = "disabled-target-and-application-preserve-history";
    await call(
      "POST",
      base + "/members/" + member + "/suspend",
      token,
      {},
      204,
      await version(),
    );
    await call("GET", url, token, undefined, 401);
    token = await auth(owner, tenant);
    assert.deepEqual((await call("GET", url, token)).body, history.body);
    const appTag = (await call("GET", appURL, token)).headers.etag;
    await call("POST", appURL + "/disable", token, {}, 200, appTag);
    assert.deepEqual((await call("GET", url, token)).body, history.body);
    phase = "real-machine-realm-refusal";
    const machineURL = base + "/applications/" + second;
    const sp = (
      await call(
        "POST",
        machineURL + "/service-principals",
        token,
        { name: "History realm probe" },
        201,
      )
    ).body;
    const key = (
      await call(
        "POST",
        machineURL + "/service-principals/" + sp.id + "/credentials",
        token,
        { name: "History realm key", expires_in_days: 1 },
        201,
        '"sp-' + tenant + "-" + sp.id + "-" + sp.version + '"',
      )
    ).body;
    const form = {
      grant_type: "client_credentials",
      scope: "machine:invoke",
      audience: "urn:sso:application:" + second,
    };
    const minted = await n.request(
      4464,
      "auth.localhost:4464",
      "POST",
      "/oauth2/token",
      new URLSearchParams(form).toString(),
      {
        "Content-Type": "application/x-www-form-urlencoded",
        Authorization:
          "Basic " +
          Buffer.from(
            key.credential.access_key + ":" + key.secret_key,
          ).toString("base64"),
      },
    );
    assert.equal(minted.status, 200);
    secrets.push(minted.body.access_token);
    execFileSync("python3", ["tests/api/validate_contract.py"], {
      cwd: n.root,
      input: JSON.stringify({
        contract: "machine",
        path: "/oauth2/token",
        method: "POST",
        status: 200,
        body: minted.body,
        request_body: form,
        content_type: minted.headers["content-type"],
      }),
      stdio: ["pipe", "pipe", "pipe"],
    });
    checks++;
    await call("GET", url, minted.body.access_token, undefined, 401);
    phase = "aged-mfa-read-and-current-audit-delegation";
    me = (await call("GET", "/v1/me", token)).body;
    execFileSync(
      "python3",
      ["tests/api/fixtures.py", "age-session", me.user_id, me.session_id],
      { cwd: n.root, stdio: "pipe" },
    );
    await call("GET", url, token);
    token = await auth(owner, tenant, undefined, true);
    await call(
      "POST",
      base + "/members/" + member + "/restore",
      token,
      {},
      204,
      await version(),
    );
    token = await auth(owner, tenant);
    async function role(code, policies) {
      const r = await call(
        "POST",
        base + "/roles",
        token,
        { code, policies },
        201,
        await version(),
      );
      token = await auth(owner, tenant);
      return r.body.id;
    }
    const audit = await role("admission_history_audit", [
      { permission: "audit.read", effect: "allow" },
    ]);
    await call(
      "PUT",
      base + "/members/" + member + "/roles/" + audit,
      token,
      {},
      204,
      await version(),
    );
    token = await auth(owner, tenant);
    guestToken = await auth(guest, tenant);
    await call("GET", url, guestToken, undefined, 403);
    const batch = await call(
      "GET",
      base + "/audit-exports?kind=management&limit=100",
      guestToken,
    );
    assert(
      batch.body.items.some(
        (v) => v.action === "application.user_access.granted",
      ),
    );
    assert(
      batch.body.items
        .filter((v) => v.action.startsWith("application.user_access."))
        .every((v) => v.membership_id === null),
    );
    const deny = await role("admission_history_owner_privacy", [
      { permission: "members.read", effect: "deny" },
      { permission: "audit.read", effect: "deny" },
    ]);
    await call(
      "PUT",
      base + "/members/" + me.membership_id + "/roles/" + deny,
      token,
      {},
      204,
      await version(),
    );
    token = await auth(owner, tenant);
    await call("GET", base + "/members", token, undefined, 403);
    await call("GET", url, token);
    phase = "owner-transfer-does-not-cache-read-authority";
    const ownerRole = (
      await call("GET", base + "/roles", token)
    ).body.items.find((v) => v.built_in).id;
    await call(
      "PUT",
      base + "/members/" + member + "/roles/" + ownerRole,
      token,
      {},
      204,
      await version(),
    );
    guestToken = await auth(guest, tenant);
    const guestVersion = (await call("GET", base + "/permissions", guestToken))
      .headers.etag;
    await call(
      "DELETE",
      base + "/members/" + me.membership_id + "/roles/" + ownerRole,
      guestToken,
      {},
      204,
      guestVersion,
    );
    await call("GET", url, token, undefined, 401);
    token = await auth(owner, tenant);
    await call("GET", url, token, undefined, 403);
    guestToken = await auth(guest, tenant);
    await call("GET", url, guestToken);
    const privacy = require("../support/private-material.cjs")(
      n.root,
      [
        ...secrets,
        ...n.fixtures.flatMap((f) => [f.password, f.totpSecret, ...f.codes]),
      ],
      started,
    );
    console.log(
      "Access history native passed: " +
        checks +
        " real contract responses; captured modes/targets/actors, no-op, bounded exact cursors, disabled state, readonly/aged MFA, machine realm, audit privacy and live ownership; private exclusion in " +
        privacy.files +
        " files/runtime logs",
    );
  } finally {
    clearInterval(progress);
    await n.close();
  }
})().catch((e) => {
  console.error(
    "Access history native failed: phase=" +
      phase +
      ", error=" +
      e.name +
      " (private details withheld)",
  );
  process.exitCode = 1;
});
