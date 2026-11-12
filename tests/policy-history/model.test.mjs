import test from "node:test";
import assert from "node:assert/strict";
import { validateRolePolicyHistory } from "../../web/console/src/role-policy-history.js";
const id = (n) => n.toString(16).padStart(32, "0"),
  tenant = id(1),
  role = id(2);
const record = (n) => ({
  id: id(n),
  actor_membership_id: id(3),
  role_code: "auditor",
  action: "role.created",
  occurred_at: "2026-11-11T23:00:00.123456Z",
  authz_version: 2,
  before: [],
  after: [{ permission: "audit.read", effect: "allow" }],
});
const page = (items = [record(4)], next = null) => ({
  format: "sso.role.policy-history.v1",
  tenant_id: tenant,
  role_id: role,
  items,
  next_cursor: next,
});
test("reviewed owner history retains immutable role namespace and known rule projections", () => {
  const input = page(),
    result = validateRolePolicyHistory(input, tenant, role);
  assert.deepEqual(result, input);
  assert.notEqual(result.items[0].after, input.items[0].after);
  assert.deepEqual(validateRolePolicyHistory(page([]), tenant, role).items, []);
});
test("foreign, partial and private history cannot enter the view", () => {
  for (const change of [
    (p) => (p.tenant_id = id(9)),
    (p) => (p.role_id = id(9)),
    (p) => (p.format = "other-format"),
    (p) => (p.secret_key = "never-display"),
    (p) => (p.items[0].email = "never-display"),
    (p) => (p.items[0].id = [id(4)]),
    (p) => (p.items[0].authz_version = Number.MAX_SAFE_INTEGER + 1),
    (p) => (p.items[0].occurred_at = "invalid"),
    (p) => delete p.items[0].before,
  ]) {
    const input = page();
    change(input);
    assert.throws(
      () => validateRolePolicyHistory(input, tenant, role),
      (e) =>
        e.code === "invalid_policy_history" &&
        !e.message.includes("never-display"),
    );
  }
});
test("policies accept only canonical reviewed permission/effect pairs and action transitions", () => {
  for (const change of [
    (p) => p.items[0].after.push({ ...p.items[0].after[0] }),
    (p) => (p.items[0].after[0].permission = "roles.manage"),
    (p) => (p.items[0].after[0].secret_key = "never-display"),
    (p) => (p.items[0].before = null),
    (p) => (p.items[0].role_code = "owner"),
    (p) => (p.items[0].action = "role.bound"),
    (p) => (p.items[0].before = p.items[0].after),
    (p) => {
      p.items[0].action = "role.deleted";
    },
    (p) => {
      p.items[0].action = "role.updated";
      p.items[0].before = p.items[0].after;
    },
  ]) {
    const input = page();
    change(input);
    assert.throws(() => validateRolePolicyHistory(input, tenant, role));
  }
  const removed = record(4);
  removed.action = "role.deleted";
  removed.before = removed.after;
  removed.after = [];
  assert.doesNotThrow(() =>
    validateRolePolicyHistory(page([removed]), tenant, role),
  );
});
test("keyset pages enforce count, strictly advancing unique IDs and exact last cursor", () => {
  assert.doesNotThrow(() =>
    validateRolePolicyHistory(page([record(4)], id(4)), tenant, role, "", 1),
  );
  for (const input of [
    page([record(4), record(4)]),
    page([record(5), record(4)]),
    page([record(4)], id(5)),
    page([record(4)], id(4)),
  ])
    assert.throws(() => validateRolePolicyHistory(input, tenant, role));
  assert.throws(() => validateRolePolicyHistory(page(), tenant, role, id(4)));
  assert.throws(() =>
    validateRolePolicyHistory(
      page([record(4), record(5)]),
      tenant,
      role,
      "",
      1,
    ),
  );
});
