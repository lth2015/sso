import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import {
  operations,
  buildCall,
  redact,
  execute,
  curlTemplate,
} from "../../web/console/src/playground.js";
const spec = JSON.parse(
  readFileSync(new URL("../../api/openapi.json", import.meta.url)),
);
const all = operations(spec),
  tenant = "a".repeat(32);
const op = (name) => all.find((x) => x.operationId === name);
test("tenant path is bound to the session; query and resource IDs are constrained", () => {
  assert.equal(all.length, 43);
  assert.equal(
    buildCall(
      op("listMembers"),
      { tenant: "b".repeat(32), limit: "2" },
      "",
      "",
      tenant,
    ).path,
    "/v1/tenants/" + tenant + "/members?limit=2",
  );
  for (const limit of ["0", "101", "1&url=https://outside.invalid", "-1"])
    assert.throws(() =>
      buildCall(op("listMembers"), { limit }, "", "", tenant),
    );
  assert.throws(() =>
    buildCall(
      op("bindRole"),
      { member: "../admin", role: "c".repeat(32) },
      "{}",
      '"authz-1"',
      tenant,
    ),
  );
});
test("write JSON bounds and operation-specific ETags are enforced before preview", () => {
  for (const text of [
    "[]",
    "null",
    "{",
    JSON.stringify({ name: "中".repeat(1500) }),
  ])
    assert.throws(() =>
      buildCall(op("createOrganization"), {}, text, '"org-1"', tenant),
    );
  assert.throws(() =>
    buildCall(op("createOrganization"), {}, "{}", '"authz-1"', tenant),
  );
  assert.equal(
    buildCall(op("createOrganization"), {}, "{}", '"org-2"', tenant).etag,
    '"org-2"',
  );
});
test("nested proofs and JWT-shaped strings are hidden without mutating the response", () => {
  const original = {
    items: [{ token: "private-proof", safe: "value" }],
    password: "private",
    note: "eyJabc.def.ghi",
  };
  original.recovery_codes_remaining = 9;
  original.recovery_codes = ["private-code"];
  const safe = JSON.stringify(redact(original));
  assert.equal(redact(original).recovery_codes_remaining, 9);
  assert(!safe.includes("private"));
  assert(!safe.includes("eyJ"));
  assert.equal(original.items[0].token, "private-proof");
  assert(safe.includes("value"));
});
test("cURL uses a placeholder, redacts proof and keeps shell metacharacters quoted", () => {
  const text = curlTemplate({
    method: "POST",
    path: "/v1/invitations/accept",
    body: { token: "private-proof", name: "a'$(touch /tmp/never-executed)" },
  });
  assert(!text.includes("private-proof"));
  assert(text.includes("<ACCESS_TOKEN>"));
  assert(text.includes("'\"'\"'"));
  assert.equal(text.split("\n").length, 5);
});
test("transport forwards exactly once, sends no Authorization, and preserves failure metadata", async () => {
  const original = globalThis.fetch,
    controller = new AbortController();
  let calls = 0;
  try {
    globalThis.fetch = async (path, options) => {
      calls++;
      assert.equal(path, "/bff/v1/me");
      assert.equal(options.headers.Authorization, undefined);
      assert.equal(options.signal, controller.signal);
      assert.equal(options.redirect, "error");
      return new Response(
        JSON.stringify({
          code: "temporarily_unavailable",
          token: "private-proof",
        }),
        {
          status: 503,
          headers: { "X-Request-ID": "example-request", "Retry-After": "2" },
        },
      );
    };
    const result = await execute(
      "context",
      "csrf",
      { method: "GET", path: "/v1/me" },
      controller.signal,
    );
    assert.equal(calls, 1);
    assert.equal(result.status, 503);
    assert.equal(result.retryAfter, "2");
    assert.equal(result.requestID, "example-request");
    assert.equal(result.body.token, "[已隐藏]");
  } finally {
    globalThis.fetch = original;
  }
});

test("client cursors are encoded and application ETags have their own schema", () => {
  const application = "b".repeat(32),
    client = op("listApplicationClients");
  assert.equal(
    buildCall(client, { application, after: "local-demo-a" }, "", "", tenant)
      .path,
    "/v1/tenants/" +
      tenant +
      "/applications/" +
      application +
      "/clients?after=local-demo-a",
  );
  assert(
    buildCall(
      client,
      { application, after: "client?&=" },
      "",
      "",
      tenant,
    ).path.endsWith("after=client%3F%26%3D"),
  );
  for (const after of ["client\n", "a".repeat(129), "含中文"])
    assert.throws(() =>
      buildCall(client, { application, after }, "", "", tenant),
    );
  const tag = '"app-' + tenant + "-" + application + '-2"';
  assert.equal(
    buildCall(
      op("disableTenantApplication"),
      { application },
      "{}",
      tag,
      tenant,
    ).etag,
    tag,
  );
  assert.throws(() =>
    buildCall(
      op("disableTenantApplication"),
      { application },
      "{}",
      '"authz-2"',
      tenant,
    ),
  );
});
