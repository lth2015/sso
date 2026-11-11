const id = /^[a-f0-9]{32}$/;
const date =
  /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?(?:Z|[+-]\d{2}:\d{2})$/;
const meta = [
  "format",
  "kind",
  "tenant_id",
  "generated_at",
  "authz_version",
  "items",
  "next_cursor",
];
const management = [
  "id",
  "actor_membership_id",
  "action",
  "target_id",
  "occurred_at",
  "role_id",
  "role_code",
  "membership_id",
];
const machine = [
  "id",
  "application_id",
  "service_principal_id",
  "credential_id",
  "action",
  "occurred_at",
];
function invalid() {
  const error = new Error("导出数据格式不正确，请重新生成。");
  error.code = "invalid_export_data";
  throw error;
}
function exact(value, keys) {
  return (
    value &&
    typeof value === "object" &&
    !Array.isArray(value) &&
    Object.keys(value).length === keys.length &&
    keys.every((key) => Object.hasOwn(value, key))
  );
}
function isDate(value) {
  return (
    typeof value === "string" &&
    value.length <= 40 &&
    date.test(value) &&
    Number.isFinite(Date.parse(value))
  );
}
export function validateAuditBatch(value, tenant, kind, limit) {
  if (
    !exact(value, meta) ||
    value.format !== "sso.audit.batch.v1" ||
    typeof tenant !== "string" ||
    !id.test(tenant) ||
    value.tenant_id !== tenant ||
    !["management", "machine"].includes(kind) ||
    value.kind !== kind ||
    !Number.isInteger(limit) ||
    limit < 1 ||
    limit > 100 ||
    !Number.isSafeInteger(value.authz_version) ||
    value.authz_version < 1 ||
    !isDate(value.generated_at) ||
    !Array.isArray(value.items) ||
    value.items.length > limit ||
    !(
      value.next_cursor === null ||
      (typeof value.next_cursor === "string" && id.test(value.next_cursor))
    )
  )
    invalid();
  const seen = new Set();
  for (const item of value.items) {
    if (
      !exact(item, kind === "management" ? management : machine) ||
      typeof item.id !== "string" ||
      !id.test(item.id) ||
      seen.has(item.id) ||
      typeof item.action !== "string" ||
      item.action.length < 1 ||
      item.action.length > 64 ||
      !isDate(item.occurred_at)
    )
      invalid();
    seen.add(item.id);
    if (kind === "machine") {
      if (
        ![
          item.application_id,
          item.service_principal_id,
          item.credential_id,
        ].every((v) => typeof v === "string" && id.test(v)) ||
        item.action !== "token.client_credentials"
      )
        invalid();
    } else {
      if (
        ![item.actor_membership_id, item.target_id].every(
          (v) => typeof v === "string" && id.test(v),
        )
      )
        invalid();
      if (item.role_id === null) {
        if (item.role_code !== null || item.membership_id !== null) invalid();
      } else if (
        typeof item.role_id !== "string" ||
        !id.test(item.role_id) ||
        typeof item.membership_id !== "string" ||
        !id.test(item.membership_id) ||
        typeof item.role_code !== "string" ||
        item.role_code.length < 1 ||
        item.role_code.length > 64
      )
        invalid();
    }
  }
  if (
    value.next_cursor !== null &&
    (value.items.length !== limit ||
      value.items.at(-1)?.id !== value.next_cursor)
  )
    invalid();
  return JSON.parse(JSON.stringify(value));
}
export function auditFilename(batch) {
  return (
    "audit-" +
    batch.kind +
    "-" +
    batch.tenant_id +
    "-" +
    new Date(batch.generated_at).toISOString().replace(/[-:.]/g, "") +
    ".json"
  );
}
