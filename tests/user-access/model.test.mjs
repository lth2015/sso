import test from "node:test";
import assert from "node:assert/strict";
import {
  accessPolicy,
  accessTag,
  accessPage,
  accessDecision,
  accessWrite,
  accessMembers,
} from "../../web/console/src/user-access.js";
const id = (n) => n.toString(16).padStart(32, "0"),
  tenant = id(1),
  app = id(2),
  member = id(3);
const policy = {
  tenant_id: tenant,
  application_id: app,
  mode: "selected_members",
  version: "7",
};
const response = (items, next = null, p = policy) => ({
  etag: accessTag(p),
  data: { policy: p, items, next_cursor: next },
});
test("User admission policy namespace and signed-int64 decimal strings stay lossless", () => {
  assert.equal(
    accessPolicy({ ...policy, version: "9223372036854775807" }, tenant, app)
      .version,
    "9223372036854775807",
  );
  for (const p of [
    { ...policy, version: 7 },
    { ...policy, version: "01" },
    { ...policy, version: "9223372036854775808" },
    { ...policy, application_id: id(9) },
    { ...policy, secret_key: "never-render" },
  ])
    assert.throws(() => accessPolicy(p, tenant, app));
});
test("bounded explicit member pages advance only within the same policy version", () => {
  const first = accessPage(
    response(
      Array.from({ length: 25 }, (_, i) => ({ membership_id: id(i + 3) })),
      id(27),
    ),
    tenant,
    app,
  );
  const second = accessPage(
    response([{ membership_id: id(28) }]),
    tenant,
    app,
    first,
  );
  assert.equal(second.items.length, 26);
  for (const r of [
    response([{ membership_id: member }], member),
    response([{ membership_id: member }, { membership_id: member }]),
    response([{ membership_id: id(28) }], null, { ...policy, version: "8" }),
  ])
    assert.throws(() => accessPage(r, tenant, app, first));
  assert.throws(() =>
    accessPage({ etag: '"app-wrong"', data: response([]).data }, tenant, app),
  );
});
test("current decisions are fixed observations, not HTTP validators or old successful grants", () => {
  const d = {
    policy,
    membership_id: member,
    allowed: true,
    reason: "explicit_grant",
    explicit_grant: true,
  };
  assert(accessDecision({ data: d, etag: null }, policy, member).allowed);
  for (const v of [
    { ...d, allowed: false },
    { ...d, explicit_grant: false },
    { ...d, policy: { ...policy, version: "8" } },
    { ...d, membership_id: id(4) },
    { ...d, reason: "all_members" },
    { ...d, secret_key: "never-render" },
  ])
    assert.throws(() =>
      accessDecision({ data: v, etag: null }, policy, member),
    );
  assert.throws(() =>
    accessDecision({ data: d, etag: accessTag(policy) }, policy, member),
  );
});
test("UI change responses require the exact next revision and expected mode", () => {
  const next = { ...policy, version: "8", mode: "all_members" };
  assert.equal(
    accessWrite({ data: next, etag: accessTag(next) }, policy, "all_members")
      .version,
    "8",
  );
  for (const p of [
    policy,
    { ...next, version: "9" },
    { ...next, mode: "selected_members" },
  ])
    assert.throws(() =>
      accessWrite({ data: p, etag: accessTag(p) }, policy, "all_members"),
    );
});
test("member choices retain their independent authorization ETag and reviewed email projection", () => {
  const row = {
      id: member,
      user_id: id(4),
      email: null,
      status: "active",
      owner: false,
    },
    r = { etag: '"authz-7"', data: { items: [row], next_cursor: null } };
  assert.equal(accessMembers(r).items[0].id, member);
  assert.throws(() => accessMembers({ ...r, etag: accessTag(policy) }));
  assert.throws(() =>
    accessMembers({
      ...r,
      data: {
        items: [{ ...row, secret_key: "never-render" }],
        next_cursor: null,
      },
    }),
  );
});
