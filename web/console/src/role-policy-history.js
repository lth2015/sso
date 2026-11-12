import { ApiError } from "./api.js";
const validID = (v) => typeof v === "string" && /^[a-f0-9]{32}$/.test(v);
const date =
  /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?(?:Z|[+-]\d{2}:\d{2})$/;
const permissions = new Set([
  "audit.read",
  "invitations.manage",
  "members.read",
  "members.write",
  "organizations.read",
  "organizations.write",
]);
function invalid() {
  throw new ApiError(502, "invalid_policy_history");
}
function exact(value, keys) {
  if (
    !value ||
    typeof value !== "object" ||
    Array.isArray(value) ||
    Object.keys(value).length !== keys.length ||
    keys.some((k) => !Object.hasOwn(value, k))
  )
    invalid();
}
function rules(value) {
  if (!Array.isArray(value) || value.length > 12) invalid();
  let last = "";
  return value.map((row) => {
    exact(row, ["permission", "effect"]);
    const key = row.permission + ":" + row.effect;
    if (
      !permissions.has(row.permission) ||
      !["allow", "deny"].includes(row.effect) ||
      key <= last
    )
      invalid();
    last = key;
    return { permission: row.permission, effect: row.effect };
  });
}
export function validateRolePolicyHistory(
  data,
  tenant,
  role,
  after = "",
  limit = 20,
) {
  exact(data, ["format", "tenant_id", "role_id", "items", "next_cursor"]);
  if (
    !validID(tenant) ||
    !validID(role) ||
    (after !== "" && !validID(after)) ||
    !Number.isInteger(limit) ||
    limit < 1 ||
    limit > 100 ||
    data.format !== "sso.role.policy-history.v1" ||
    data.tenant_id !== tenant ||
    data.role_id !== role ||
    !Array.isArray(data.items) ||
    data.items.length > limit
  )
    invalid();
  let last = after;
  const items = data.items.map((row) => {
    exact(row, [
      "id",
      "actor_membership_id",
      "role_code",
      "action",
      "occurred_at",
      "authz_version",
      "before",
      "after",
    ]);
    if (
      !validID(row.id) ||
      row.id <= last ||
      !validID(row.actor_membership_id) ||
      typeof row.role_code !== "string" ||
      !/^[a-z][a-z0-9_-]{0,62}$/.test(row.role_code) ||
      row.role_code === "owner" ||
      !["role.created", "role.updated", "role.deleted"].includes(row.action) ||
      typeof row.occurred_at !== "string" ||
      row.occurred_at.length > 40 ||
      !date.test(row.occurred_at) ||
      !Number.isFinite(Date.parse(row.occurred_at)) ||
      !Number.isSafeInteger(row.authz_version) ||
      row.authz_version < 1
    )
      invalid();
    const before = rules(row.before),
      after = rules(row.after);
    if (
      (row.action === "role.created" && before.length) ||
      (row.action === "role.deleted" && after.length) ||
      (row.action === "role.updated" &&
        JSON.stringify(before) === JSON.stringify(after))
    )
      invalid();
    last = row.id;
    return {
      id: row.id,
      actor_membership_id: row.actor_membership_id,
      role_code: row.role_code,
      action: row.action,
      occurred_at: row.occurred_at,
      authz_version: row.authz_version,
      before,
      after,
    };
  });
  const next = data.next_cursor;
  if (
    next !== null &&
    (!validID(next) || items.length !== limit || next !== items.at(-1)?.id)
  )
    invalid();
  return {
    format: data.format,
    tenant_id: tenant,
    role_id: role,
    items,
    next_cursor: next,
  };
}
