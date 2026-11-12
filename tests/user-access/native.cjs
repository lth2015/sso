// Real User PKCE/Bearer -> current application admission on the primary.
const n = require("../support/native-fixtures.cjs"),
  assert = require("node:assert/strict"),
  { execFileSync } = require("node:child_process"),
  crypto = require("node:crypto");
let phase = "startup",
  checks = 0;
const newOperations = new Set();
const secrets = [],
  started = new Date().toISOString(),
  progress = setInterval(
    () => console.log("Application User access phase: " + phase),
    20000,
  );
async function auth(f, tenant, scopes, force = false, client) {
  const t = await n.authorize(f, tenant, scopes, force, client);
  secrets.push(
    ...[t.access_token, t.id_token, t.refresh_token].filter(
      (v) => typeof v === "string",
    ),
  );
  return t.access_token;
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
  if (r.status === 200 && url.includes("/user-access"))
    newOperations.add(
      method +
        " " +
        (url.includes("/user-access/members/") ? "member" : "policy"),
    );
  if (typeof r.body?.token === "string") secrets.push(r.body.token);
  if (typeof r.body?.secret_key === "string") secrets.push(r.body.secret_key);
  return r;
}
function absent(v) {
  const text = JSON.stringify(v);
  for (const secret of secrets) assert(!text.includes(secret));
  assert(!/eyJ[\w-]+\.[\w-]+\.[\w-]+/.test(text));
}
async function machineIdentity(token, app) {
  const r = await n.request(
    8083,
    "api.localhost:8083",
    "GET",
    "/v1/machine/applications/" + app + "/identity",
    undefined,
    { Authorization: "Bearer " + token },
  );
  assert.equal(r.status, 200);
  execFileSync("python3", ["tests/api/validate_contract.py"], {
    cwd: n.root,
    input: JSON.stringify({
      contract: "machine",
      path: "/v1/machine/applications/{application}/identity",
      method: "GET",
      status: 200,
      body: r.body,
      content_type: r.headers["content-type"],
    }),
    stdio: ["pipe", "pipe", "pipe"],
  });
  checks++;
}
async function refreshDenied(refresh, client) {
  const r = await n.request(
    4464,
    "auth.localhost:4464",
    "POST",
    "/oauth2/token",
    new URLSearchParams({
      grant_type: "refresh_token",
      client_id: client,
      refresh_token: refresh,
    }).toString(),
    { "Content-Type": "application/x-www-form-urlencoded" },
  );
  assert.equal(r.status, 403);
  assert.equal(typeof r.body.error, "string");
  assert(!r.body.access_token);
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
    const me = (await call("GET", "/v1/me", token)).body,
      foreign = (await call("GET", "/v1/me", otherToken)).body;
    const tenant = me.tenant_id,
      base = "/v1/tenants/" + tenant;
    const invitation = (
      await call(
        "POST",
        base + "/invitations",
        token,
        { email: guest.email },
        201,
      )
    ).body;
    const joined = (
      await call(
        "POST",
        "/v1/invitations/accept",
        guestToken,
        { token: invitation.token },
        201,
      )
    ).body;
    token = await auth(owner, tenant);
    guestToken = await auth(guest, tenant);
    const app = (
      await call(
        "POST",
        base + "/applications",
        token,
        { name: "原生准入演示" },
        201,
      )
    ).body.id;
    const client = (
      await call(
        "POST",
        base + "/applications/" + app + "/clients",
        token,
        {
          name: "准入演示客户端",
          redirect_uri: "http://127.0.0.1:9876/callback",
        },
        201,
      )
    ).body;
    const clientURL =
      base + "/applications/" + app + "/clients/" + client.client_id;
    await call(
      "POST",
      clientURL + "/reconcile",
      token,
      {},
      200,
      `"client-${tenant}-${client.client_id}-${client.version}"`,
    );
    const url = base + "/applications/" + app + "/user-access";
    let page = await call("GET", url, token),
      tag = page.headers.etag;
    assert.equal(page.body.policy.mode, "all_members");
    assert.equal(page.body.policy.version, "1");
    assert.deepEqual(page.body.items, []);
    let original = await n.authorize(
      guest,
      tenant,
      "openid offline_access platform:read",
      false,
      client.client_id,
    );
    secrets.push(
      original.access_token,
      original.refresh_token,
      original.id_token,
    );
    await call("GET", "/v1/me", original.access_token);
    const delayedVerifier = crypto.randomBytes(32).toString("base64url"),
      delayedState = crypto.randomBytes(32).toString("base64url");
    const delayedParams = new URLSearchParams({
      client_id: client.client_id,
      response_type: "code",
      redirect_uri: "http://127.0.0.1:9876/callback",
      scope: "platform:read",
      audience: "https://api.sso.local",
      state: delayedState,
      code_challenge: crypto
        .createHash("sha256")
        .update(delayedVerifier)
        .digest("base64url"),
      code_challenge_method: "S256",
    });
    await guest.page.goto(
      "http://auth.localhost:4464/oauth2/auth?" + delayedParams,
    );
    await guest.page
      .getByRole("heading", { name: "确认本次授权", exact: true })
      .waitFor();
    await guest.page.locator("#tenant").selectOption(tenant);
    await guest.page
      .getByRole("button", { name: "同意并继续", exact: true })
      .click();
    await guest.page.waitForURL(
      (u) => u.origin === "http://127.0.0.1:9876" && u.pathname === "/callback",
    );
    const delayedCallback = new URL(guest.page.url());
    assert.equal(delayedCallback.searchParams.get("state"), delayedState);
    const delayedCode = delayedCallback.searchParams.get("code");
    assert(delayedCode);
    secrets.push(delayedCode, delayedVerifier);
    await guest.page.evaluate(() =>
      history.replaceState(null, "", "/callback"),
    );
    phase = "machine-and-other-application-isolation";
    const sp = (
      await call(
        "POST",
        base + "/applications/" + app + "/service-principals",
        token,
        { name: "隔离的机器身份" },
        201,
      )
    ).body;
    const key = (
      await call(
        "POST",
        base +
          "/applications/" +
          app +
          "/service-principals/" +
          sp.id +
          "/credentials",
        token,
        { name: "准入隔离测试", expires_in_days: 1 },
        201,
        `"sp-${tenant}-${sp.id}-${sp.version}"`,
      )
    ).body;
    const form = {
      grant_type: "client_credentials",
      scope: "machine:invoke",
      audience: "urn:sso:application:" + app,
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
    await machineIdentity(minted.body.access_token, app);
    phase = "restrict-denies-existing-code-refresh-and-consent";
    let changed = await call(
      "PUT",
      url,
      token,
      { mode: "selected_members" },
      200,
      tag,
    );
    tag = changed.headers.etag;
    assert.equal(changed.body.version, "2");
    await call("GET", "/v1/me", original.access_token, undefined, 401);
    await refreshDenied(original.refresh_token, client.client_id);
    const delayedResponse = await n.request(
      4464,
      "auth.localhost:4464",
      "POST",
      "/oauth2/token",
      new URLSearchParams({
        grant_type: "authorization_code",
        client_id: client.client_id,
        redirect_uri: "http://127.0.0.1:9876/callback",
        code: delayedCode,
        code_verifier: delayedVerifier,
      }).toString(),
      { "Content-Type": "application/x-www-form-urlencoded" },
    );
    assert.equal(delayedResponse.status, 403);
    assert(!delayedResponse.body.access_token);

    await call("GET", "/v1/me", token);
    await call("GET", "/v1/me", guestToken);
    await machineIdentity(minted.body.access_token, app);
    const params = new URLSearchParams({
      client_id: client.client_id,
      response_type: "code",
      redirect_uri: "http://127.0.0.1:9876/callback",
      scope: "platform:read",
      audience: "https://api.sso.local",
      state: crypto.randomBytes(32).toString("base64url"),
      code_challenge: crypto.randomBytes(32).toString("base64url"),
      code_challenge_method: "S256",
    });
    await guest.page.goto("http://auth.localhost:4464/oauth2/auth?" + params);
    await guest.page
      .getByRole("heading", { name: "确认本次授权", exact: true })
      .waitFor();
    await guest.page
      .getByText(
        "没有可访问的空间。请联系空间管理员确认应用启用和成员访问授权。",
        { exact: true },
      )
      .waitFor();
    assert.equal(await guest.page.locator("#tenant option").count(), 0);
    // Bypass only the presentation filter; the real consent POST must still deny.
    await guest.page.locator("#tenant").evaluate((select, tenant) => {
      const option = document.createElement("option");
      option.value = tenant;
      option.textContent = "forged option";
      select.append(option);
      select.value = tenant;
      document.querySelector("#consent-form button").disabled = false;
    }, tenant);
    const deniedConsent = guest.page.waitForResponse(
      (r) =>
        /^\/v1\/flows\/[a-f0-9]{32}\/consent$/.test(
          new URL(r.url()).pathname,
        ) && r.request().method() === "POST",
    );
    await guest.page
      .getByRole("button", { name: "同意并继续", exact: true })
      .click();
    assert.equal((await deniedConsent).status(), 403);
    phase = "explicit-grant-owner-bypass-and-no-op";
    let decision = await call(
      "GET",
      url + "/members/" + joined.membership_id,
      token,
    );
    assert(!decision.body.allowed);
    assert.equal(decision.body.reason, "not_granted");
    assert.equal(decision.headers.etag, undefined);
    changed = await call(
      "PUT",
      url + "/members/" + joined.membership_id,
      token,
      {},
      200,
      tag,
    );
    tag = changed.headers.etag;
    assert.equal(changed.body.version, "3");
    const auditBefore = (
      await call("GET", base + "/audit-events?limit=100", token)
    ).body.items;
    const noop = await call(
      "PUT",
      url + "/members/" + joined.membership_id,
      token,
      {},
      200,
      tag,
    );
    assert.equal(noop.headers.etag, tag);
    assert.deepEqual(
      (await call("GET", base + "/audit-events?limit=100", token)).body.items,
      auditBefore,
    );
    decision = await call(
      "GET",
      url + "/members/" + joined.membership_id,
      token,
    );
    assert(decision.body.allowed);
    assert.equal(decision.body.reason, "explicit_grant");
    decision = await call("GET", url + "/members/" + me.membership_id, token);
    assert(decision.body.allowed);
    assert.equal(decision.body.reason, "owner");
    const ownedOwner = await auth(
      owner,
      tenant,
      "openid platform:read",
      false,
      client.client_id,
    );
    await call("GET", "/v1/me", ownedOwner);
    const granted = await n.authorize(
      guest,
      tenant,
      "openid offline_access platform:read",
      false,
      client.client_id,
    );
    secrets.push(granted.access_token, granted.refresh_token, granted.id_token);
    await call("GET", "/v1/me", granted.access_token);
    const renewed = await n.request(
      4464,
      "auth.localhost:4464",
      "POST",
      "/oauth2/token",
      new URLSearchParams({
        grant_type: "refresh_token",
        client_id: client.client_id,
        refresh_token: granted.refresh_token,
      }).toString(),
      { "Content-Type": "application/x-www-form-urlencoded" },
    );
    assert.equal(renewed.status, 200);
    Object.assign(granted, renewed.body);
    secrets.push(granted.access_token, granted.refresh_token, granted.id_token);
    await call("GET", "/v1/me", granted.access_token);
    phase = "revoke-regrant-never-resurrects-and-independent-version";
    changed = await call(
      "DELETE",
      url + "/members/" + joined.membership_id,
      token,
      {},
      200,
      tag,
    );
    tag = changed.headers.etag;
    await call("GET", "/v1/me", granted.access_token, undefined, 401);
    await refreshDenied(granted.refresh_token, client.client_id);
    await call("GET", "/v1/me", ownedOwner, undefined, 401);
    changed = await call(
      "PUT",
      url + "/members/" + joined.membership_id,
      token,
      {},
      200,
      tag,
    );
    tag = changed.headers.etag;
    await call("GET", "/v1/me", granted.access_token, undefined, 401);
    const fresh = await auth(
      guest,
      tenant,
      "openid platform:read",
      false,
      client.client_id,
    );
    await call("GET", "/v1/me", fresh);
    changed = await call("PUT", url, token, { mode: "all_members" }, 200, tag);
    tag = changed.headers.etag;
    await call("GET", "/v1/me", fresh, undefined, 401);
    changed = await call(
      "PUT",
      url,
      token,
      { mode: "selected_members" },
      200,
      tag,
    );
    tag = changed.headers.etag;
    assert.equal((await call("GET", url, token)).body.items.length, 1);
    await call("GET", "/v1/me", token);
    await call("GET", "/v1/me", guestToken);
    await machineIdentity(minted.body.access_token, app);
    changed = await call(
      "PUT",
      url + "/members/" + me.membership_id,
      token,
      {},
      200,
      tag,
    );
    tag = changed.headers.etag;
    const first = await call("GET", url + "?limit=1", token);
    assert(first.body.next_cursor);
    const second = await call(
      "GET",
      url + "?limit=1&after=" + first.body.next_cursor,
      token,
    );
    assert.equal(second.body.items.length, 1);
    assert(
      second.body.items[0].membership_id > first.body.items[0].membership_id,
    );
    changed = await call(
      "DELETE",
      url + "/members/" + me.membership_id,
      token,
      {},
      200,
      tag,
    );
    tag = changed.headers.etag;
    phase = "input-scope-tenant-etag-and-machine-realm";
    for (const query of [
      "?limit=0",
      "?limit=101",
      "?limit=1&limit=2",
      "?mode=all_members",
      "?after=",
    ])
      await call("GET", url + query, token, undefined, 400);
    await call("GET", url, token, {}, 400);
    await call(
      "GET",
      url + "/members/" + joined.membership_id + "?limit=1",
      token,
      undefined,
      400,
    );
    await call("PUT", url, token, { mode: "all_members" }, 428);
    await call(
      "PUT",
      url,
      token,
      { mode: "all_members" },
      400,
      '"app-' + tenant + "-" + app + '-1"',
    );
    await call(
      "PUT",
      url,
      token,
      { mode: "all_members" },
      412,
      '"ua-' + tenant + "-" + "f".repeat(32) + '-1"',
    );
    await call(
      "PUT",
      url,
      token,
      { mode: "all_members" },
      412,
      '"ua-' + tenant + "-" + app + '-1"',
    );
    for (const body of [
      { mode: "owner" },
      { mode: "all_members", secret_key: "private-access-marker" },
    ])
      await call("PUT", url, token, body, 400, tag);
    await call("GET", url, guestToken, undefined, 403);
    await call("PUT", url, guestToken, { mode: "all_members" }, 403, tag);
    await call(
      "GET",
      url + "/members/" + foreign.membership_id,
      token,
      undefined,
      404,
    );
    await call(
      "GET",
      "/v1/tenants/" +
        foreign.tenant_id +
        "/applications/" +
        app +
        "/user-access",
      token,
      undefined,
      404,
    );
    const shared = (
      await call("GET", base + "/applications?limit=100", token)
    ).body.items.find((a) => a.protected).id;
    await call(
      "GET",
      base + "/applications/" + shared + "/user-access",
      token,
      undefined,
      409,
    );
    await call("GET", url, minted.body.access_token, undefined, 401);
    const read = await auth(owner, tenant, "openid platform:read"),
      write = await auth(owner, tenant, "openid platform:write");
    await call("GET", url, read);
    await call("PUT", url, read, { mode: "all_members" }, 403, tag);
    await call("GET", url, write, undefined, 403);
    phase = "primary-recent-mfa-and-owner-loss";
    const current = (await call("GET", "/v1/me", token)).body;
    execFileSync(
      "python3",
      [
        "tests/api/fixtures.py",
        "age-session",
        current.user_id,
        current.session_id,
      ],
      { cwd: n.root, stdio: "pipe" },
    );
    await call("GET", url, token);
    await call("GET", url + "/members/" + joined.membership_id, token);
    const aged = await call(
      "PUT",
      url,
      token,
      { mode: "all_members" },
      403,
      tag,
    );
    assert.equal(aged.body.code, "recent_mfa_required");
    token = await auth(owner, tenant, undefined, true);
    const roles = (await call("GET", base + "/roles", token)).body.items,
      ownerRole = roles.find((r) => r.built_in).id;
    let av = (await call("GET", base + "/permissions", token)).headers.etag;
    await call(
      "PUT",
      base + "/members/" + joined.membership_id + "/roles/" + ownerRole,
      token,
      {},
      204,
      av,
    );
    guestToken = await auth(guest, tenant);
    av = (await call("GET", base + "/permissions", guestToken)).headers.etag;
    await call(
      "DELETE",
      base + "/members/" + me.membership_id + "/roles/" + ownerRole,
      guestToken,
      {},
      204,
      av,
    );
    await call("GET", url, token, undefined, 401);
    token = await auth(owner, tenant);
    await call("GET", url, token, undefined, 403);
    guestToken = await auth(guest, tenant);
    page = await call("GET", url, guestToken);
    absent(page.body);
    assert.deepEqual([...newOperations].sort(), [
      "DELETE member",
      "GET member",
      "GET policy",
      "PUT member",
      "PUT policy",
    ]);
    const privacy = require("../support/private-material.cjs")(
      n.root,
      secrets,
      started,
    );
    console.log(
      "Application User access native passed: " +
        checks +
        " real contract responses; real owned PKCE, consent tamper rejection, code/refresh/current revoke, no resurrection, owner/target/scope/tenant/MFA bounds and machine/other-app isolation; private exclusion in " +
        privacy.files +
        " files/runtime logs",
    );
  } finally {
    clearInterval(progress);
    await n.close();
  }
})().catch((e) => {
  console.error(
    "Application User access native failed: phase=" +
      phase +
      ", error=" +
      e.name +
      " (private details withheld)",
  );
  console.error(JSON.stringify(n.diagnostics()));
  console.error(
    JSON.stringify({
      frames: [
        ...String(e.stack).matchAll(/user-access\/native\.cjs:(\d+):/g),
      ].map((v) => Number(v[1])),
      actual:
        typeof e.actual === "number"
          ? e.actual
          : [
                "invalid_grant",
                "invalid_request",
                "request_forbidden",
                "access_denied",
              ].includes(e.actual)
            ? e.actual
            : "withheld",
    }),
  );
  process.exitCode = 1;
});
