// Real PKCE/User/Bearer/primary invitation lifecycle. Secrets stay on stdin/in memory.
const n = require("../support/native-fixtures.cjs"),
  assert = require("node:assert/strict"),
  crypto = require("node:crypto"),
  { execFileSync } = require("node:child_process");
let phase = "startup",
  checks = 0;
const secrets = [],
  started = new Date().toISOString();
const progress = setInterval(
  () => console.log("Invitation native phase: " + phase),
  20000,
);
async function call(
  method,
  path,
  bearer,
  body,
  status = 200,
  etag,
  extra = {},
) {
  const r = await n.api(
    method,
    path,
    bearer,
    body,
    { ...extra, ...(etag ? { "If-Match": etag } : {}) },
    status,
  );
  checks++;
  return r;
}
async function auth(f, tenant, scope, force = false) {
  const r = await n.authorize(f, tenant, scope, force);
  secrets.push(
    ...[r.access_token, r.id_token, r.refresh_token].filter(
      (v) => typeof v === "string",
    ),
  );
  return r.access_token;
}
function remember(result) {
  secrets.push(result.body.token);
  return result;
}
function absent(value) {
  const text = JSON.stringify(value);
  for (const secret of secrets) assert(!text.includes(secret));
}
(async () => {
  try {
    await n.start();
    const owner = await n.register(),
      recipient = await n.register(),
      manager = await n.register();
    let token = await auth(owner),
      recipientToken = await auth(recipient),
      managerToken = await auth(manager);
    let me = (await call("GET", "/v1/me", token)).body;
    const tenant = me.tenant_id,
      base = "/v1/tenants/" + tenant,
      invitations = base + "/invitations";
    phase = "metadata-strong-resource-etag-and-input-bounds";
    const original = remember(
      await call("POST", invitations, token, { email: recipient.email }, 201),
    );
    const path = invitations + "/" + original.body.id;
    let detail = await call("GET", path, token);
    assert.equal(detail.body.revision, 1);
    assert.equal(detail.body.email, recipient.email);
    assert.equal(
      detail.headers.etag,
      `"invite-${tenant}-${original.body.id}-1"`,
    );
    absent(detail.body);
    await call("GET", path, undefined, undefined, 401);
    await call(
      "POST",
      path + "/reissue",
      undefined,
      {},
      401,
      detail.headers.etag,
    );
    await call("GET", path + "?tenant_id=" + tenant, token, undefined, 400);
    await call("GET", path, token, {}, 400);
    await call(
      "GET",
      invitations + "/" + "f".repeat(32),
      token,
      undefined,
      404,
    );
    await call("POST", path + "/reissue", token, {}, 428);
    for (const bad of [
      "*",
      "W/" + detail.headers.etag,
      detail.headers.etag.replace('-1"', '-01"'),
    ])
      await call("POST", path + "/reissue", token, {}, 400, bad);
    await call("POST", path + "/reissue", token, {}, 400, undefined, {
      "If-Match": [detail.headers.etag, detail.headers.etag],
    });
    await call(
      "POST",
      path + "/reissue",
      token,
      {},
      412,
      detail.headers.etag.replace(original.body.id, "f".repeat(32)),
    );
    await call(
      "POST",
      path + "/reissue",
      token,
      { email: owner.email },
      400,
      detail.headers.etag,
    );
    await call(
      "POST",
      path + "/reissue?kind=other",
      token,
      {},
      400,
      detail.headers.etag,
    );
    const issued = remember(
      await call(
        "POST",
        path + "/reissue",
        token,
        {},
        200,
        detail.headers.etag,
      ),
    );
    assert.equal(issued.body.revision, 2);
    assert.notEqual(issued.body.token, original.body.token);
    await call("GET", "/v1/me", token);
    await call(
      "POST",
      "/v1/invitations/accept",
      recipientToken,
      { token: original.body.token },
      400,
    );
    await call(
      "POST",
      "/v1/invitations/accept",
      token,
      { token: issued.body.token },
      403,
    );
    absent((await call("GET", path, token)).body);
    absent((await call("GET", invitations, token)).body);
    phase = "concurrent-update-and-closed-never-reopens";
    const race = await Promise.all(
      [0, 1].map(() =>
        call(
          "POST",
          path + "/reissue",
          token,
          {},
          [200, 412],
          issued.headers.etag,
        ),
      ),
    );
    assert.deepEqual(race.map((r) => r.status).sort(), [200, 412]);
    const winner = remember(race.find((r) => r.status === 200));
    await call(
      "POST",
      "/v1/invitations/accept",
      recipientToken,
      { token: issued.body.token },
      400,
    );
    await call(
      "POST",
      "/v1/invitations/accept",
      recipientToken,
      { token: winner.body.token },
      201,
    );
    await call("GET", path, token, undefined, 401);
    token = await auth(owner, tenant);
    detail = await call("GET", path, token);
    assert.equal(detail.body.state, "closed");
    assert.equal(detail.body.revision, 4);
    await call("POST", path + "/reissue", token, {}, 412, winner.headers.etag);
    await call("POST", path + "/reissue", token, {}, 409, detail.headers.etag);
    await call("POST", path + "/revoke", token, {}, 204);
    assert.equal((await call("GET", path, token)).body.revision, 4);
    phase = "same-user-other-space-and-expired-proof";
    const other = (
      await call(
        "POST",
        "/v1/tenants",
        token,
        {
          slug: "invite-other-" + crypto.randomBytes(5).toString("hex"),
          name: "Other invitation fixture",
        },
        201,
      )
    ).body.id;
    const otherToken = await auth(owner, other);
    await call("GET", path, otherToken, undefined, 404);
    const foreign = "/v1/tenants/" + other + "/invitations/" + original.body.id;
    await call("GET", foreign, otherToken, undefined, 404);
    await call(
      "POST",
      foreign + "/reissue",
      otherToken,
      {},
      404,
      `"invite-${other}-${original.body.id}-4"`,
    );
    const expired = remember(
      await call(
        "POST",
        invitations,
        token,
        {
          email:
            "expired-" +
            crypto.randomBytes(5).toString("hex") +
            "@example.invalid",
        },
        201,
      ),
    );
    const expiredPath = invitations + "/" + expired.body.id;
    execFileSync(
      "python3",
      ["tests/invitations/fixture.py", "expire", me.user_id, expired.body.id],
      { cwd: n.root, stdio: "pipe" },
    );
    detail = await call("GET", expiredPath, token);
    assert.equal(detail.body.state, "expired");
    const renewed = remember(
      await call(
        "POST",
        expiredPath + "/reissue",
        token,
        {},
        200,
        detail.headers.etag,
      ),
    );
    detail = await call("GET", expiredPath, token);
    assert.equal(detail.body.state, "pending");
    assert.equal(detail.body.revision, 2);
    await call("POST", expiredPath + "/revoke", token, {}, 204);
    detail = await call("GET", expiredPath, token);
    assert.equal(detail.body.revision, 3);
    await call(
      "POST",
      expiredPath + "/reissue",
      token,
      {},
      409,
      detail.headers.etag,
    );
    phase = "current-delegation-default-deny-and-live-explicit-deny";
    const managerInvite = remember(
      await call("POST", invitations, token, { email: manager.email }, 201),
    );
    await call(
      "POST",
      "/v1/invitations/accept",
      managerToken,
      { token: managerInvite.body.token },
      201,
    );
    token = await auth(owner, tenant);
    managerToken = await auth(manager, tenant);
    const pending = remember(
      await call(
        "POST",
        invitations,
        token,
        { email: "delegated@example.invalid" },
        201,
      ),
    );
    const pendingPath = invitations + "/" + pending.body.id;
    detail = await call("GET", pendingPath, token);
    await call("GET", pendingPath, managerToken, undefined, 403);
    await call(
      "POST",
      pendingPath + "/reissue",
      managerToken,
      {},
      403,
      detail.headers.etag,
    );
    const member = (
      await call("GET", base + "/members", token)
    ).body.items.find((v) => v.email === manager.email).id;
    async function policy(code, effect) {
      let current = await call("GET", base, token);
      const role = (
        await call(
          "POST",
          base + "/roles",
          token,
          { code, policies: [{ permission: "invitations.manage", effect }] },
          201,
          current.headers.etag,
        )
      ).body.id;
      token = await auth(owner, tenant);
      current = await call("GET", base, token);
      await call(
        "PUT",
        base + "/members/" + member + "/roles/" + role,
        token,
        {},
        204,
        current.headers.etag,
      );
      token = await auth(owner, tenant);
      managerToken = await auth(manager, tenant);
    }
    await policy("invitation_delegate", "allow");
    detail = await call("GET", pendingPath, managerToken);
    remember(
      await call(
        "POST",
        pendingPath + "/reissue",
        managerToken,
        {},
        200,
        detail.headers.etag,
      ),
    );
    await call("GET", base + "/members", managerToken, undefined, 403);
    await policy("invitation_blocked", "deny");
    detail = await call("GET", pendingPath, token);
    await call("GET", pendingPath, managerToken, undefined, 403);
    await call(
      "POST",
      pendingPath + "/reissue",
      managerToken,
      {},
      403,
      detail.headers.etag,
    );
    phase = "scope-separation-aged-mfa-and-controlled-audit";
    const readOnly = await auth(owner, tenant, "openid platform:read"),
      writeOnly = await auth(owner, tenant, "openid platform:write");
    await call("GET", pendingPath, readOnly);
    await call(
      "POST",
      pendingPath + "/reissue",
      readOnly,
      {},
      403,
      detail.headers.etag,
    );
    await call("GET", pendingPath, writeOnly, undefined, 403);
    remember(
      await call(
        "POST",
        pendingPath + "/reissue",
        writeOnly,
        {},
        200,
        detail.headers.etag,
      ),
    );
    detail = await call("GET", pendingPath, token);
    me = (await call("GET", "/v1/me", token)).body;
    execFileSync(
      "python3",
      ["tests/api/fixtures.py", "age-session", me.user_id, me.session_id],
      { cwd: n.root, stdio: "pipe" },
    );
    await call("GET", pendingPath, token);
    const aged = await call(
      "POST",
      pendingPath + "/reissue",
      token,
      {},
      403,
      detail.headers.etag,
    );
    assert.equal(aged.body.code, "recent_mfa_required");
    const audit = await call("GET", base + "/audit-exports?limit=100", token);
    assert(audit.body.items.some((v) => v.action === "invitation.reissued"));
    absent(audit.body);
    const privacy = require("../support/private-material.cjs")(
      n.root,
      secrets,
      started,
    );
    console.log(
      "Invitation native passed: " +
        checks +
        " real contract responses; resource revisions, one-time proof replacement, old-proof refusal, concurrent updates, closed/expired/foreign bounds, readonly/write scope, current delegation/deny/MFA and secret exclusion in " +
        privacy.files +
        " files/runtime logs",
    );
  } finally {
    clearInterval(progress);
    await n.close();
  }
})().catch((error) => {
  console.error(
    "Invitation native failed: phase=" +
      phase +
      ", error=" +
      error.name +
      " (private details withheld)",
  );
  console.error(JSON.stringify(n.diagnostics()));
  process.exitCode = 1;
});
