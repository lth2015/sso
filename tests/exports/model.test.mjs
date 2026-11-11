import test from "node:test";
import assert from "node:assert/strict";
import {
  validateAuditBatch,
  auditFilename,
} from "../../web/console/src/audit-export.js";
const tenant = "a".repeat(32),
  at = "2026-11-11T19:00:00Z";
function management() {
  return {
    format: "sso.audit.batch.v1",
    kind: "management",
    tenant_id: tenant,
    generated_at: at,
    authz_version: 1,
    items: [
      {
        id: "b".repeat(32),
        actor_membership_id: "c".repeat(32),
        action: "tenant.created",
        target_id: "d".repeat(32),
        occurred_at: at,
        role_id: null,
        role_code: null,
        membership_id: null,
      },
    ],
    next_cursor: null,
  };
}
function machine() {
  const value = management();
  value.kind = "machine";
  value.items = [
    {
      id: "b".repeat(32),
      application_id: "c".repeat(32),
      service_principal_id: "d".repeat(32),
      credential_id: "e".repeat(32),
      action: "token.client_credentials",
      occurred_at: at,
    },
  ];
  return value;
}
test("frozen management and machine projections retain their own actor model", () => {
  const input = management(),
    result = validateAuditBatch(input, tenant, "management", 25);
  assert.deepEqual(result, input);
  assert.notEqual(result, input);
  assert.equal(
    validateAuditBatch(machine(), tenant, "machine", 25).items[0]
      .service_principal_id,
    "d".repeat(32),
  );
});
test("foreign context, unknown/secret fields and mixed actor models cannot enter downloads", () => {
  for (const change of [
    (v) => {
      v.tenant_id = "f".repeat(32);
    },
    (v) => {
      v.kind = "machine";
    },
    (v) => {
      v.secret_key = "never-echo-this-private-value";
    },
    (v) => {
      v.items[0].email = "private@example.invalid";
    },
    (v) => {
      v.items[0].role_code = "incoherent";
    },
    (v) => {
      v.items[0].actor_membership_id = "invalid";
    },
    (v) => {
      v.generated_at = "invalid";
    },
    (v) => {
      v.authz_version = 0;
    },
  ]) {
    const value = management();
    change(value);
    assert.throws(
      () => validateAuditBatch(value, tenant, "management", 25),
      (e) =>
        e.code === "invalid_export_data" && !e.message.includes("never-echo"),
    );
  }
  const value = machine();
  value.items[0].actor_membership_id = "c".repeat(32);
  assert.throws(() => validateAuditBatch(value, tenant, "machine", 25));
});
test("cursor continuity, duplicates and page bounds are checked before saving", () => {
  const value = management();
  value.next_cursor = value.items[0].id;
  assert.doesNotThrow(() => validateAuditBatch(value, tenant, "management", 1));
  assert.throws(() => validateAuditBatch(value, tenant, "management", 25));
  value.next_cursor = "f".repeat(32);
  assert.throws(() => validateAuditBatch(value, tenant, "management", 1));
  value.next_cursor = null;
  value.items.push(value.items[0]);
  assert.throws(() => validateAuditBatch(value, tenant, "management", 25));
  assert.throws(() =>
    validateAuditBatch(management(), tenant, "management", 101),
  );
});
test("download filename contains only the validated namespace, kind and UTC time", () => {
  const batch = validateAuditBatch(management(), tenant, "management", 25);
  assert.equal(
    auditFilename(batch),
    "audit-management-" + tenant + "-20261111T190000000Z.json",
  );
});
