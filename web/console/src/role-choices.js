// Project only existing role/member metadata; no authorization is cached here.
import { ApiError } from "./api.js";
const id = /^[a-f0-9]{32}$/;
const validID = (value) => typeof value === "string" && id.test(value);
const permissions = new Set([
  "members.read",
  "members.write",
  "organizations.read",
  "organizations.write",
  "invitations.manage",
  "audit.read",
]);
export const choiceLimit = 25;
export const emptyChoices = () => ({ items: [], next: null, etag: null });
function invalid() {
  throw new ApiError(502, "invalid_role_choices");
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
function tag(value, expected) {
  if (
    typeof value !== "string" ||
    !/^"authz-[1-9][0-9]*"$/.test(value) ||
    (expected && value !== expected)
  )
    invalid();
}
function project(row, kind) {
  if (kind === "members") {
    exact(row, ["id", "user_id", "email", "status", "owner"]);
    if (
      !validID(row.id) ||
      !validID(row.user_id) ||
      typeof row.owner !== "boolean" ||
      !["active", "invited", "suspended", "left"].includes(row.status) ||
      !(
        row.email === null ||
        (typeof row.email === "string" &&
          row.email.length <= 254 &&
          !/[\r\n]/.test(row.email))
      )
    )
      invalid();
    return {
      id: row.id,
      user_id: row.user_id,
      email: row.email,
      status: row.status,
      owner: row.owner,
    };
  }
  if (kind !== "roles") invalid();
  exact(row, ["id", "code", "built_in", "scope", "policies", "binding_count"]);
  if (
    !validID(row.id) ||
    typeof row.code !== "string" ||
    !/^[a-z][a-z0-9_-]{0,62}$/.test(row.code) ||
    typeof row.built_in !== "boolean" ||
    row.built_in !== (row.code === "owner") ||
    row.scope !== "tenant" ||
    !Number.isSafeInteger(row.binding_count) ||
    row.binding_count < 0 ||
    !Array.isArray(row.policies) ||
    row.policies.length > 12
  )
    invalid();
  const seen = new Set(),
    policies = row.policies.map((p) => {
      exact(p, ["permission", "effect"]);
      const key = p.permission + ":" + p.effect;
      if (
        !permissions.has(p.permission) ||
        !["allow", "deny"].includes(p.effect) ||
        seen.has(key)
      )
        invalid();
      seen.add(key);
      return { permission: p.permission, effect: p.effect };
    });
  return {
    id: row.id,
    code: row.code,
    built_in: row.built_in,
    scope: row.scope,
    policies,
    binding_count: row.binding_count,
  };
}
function rows(items, kind, maximum, after = "") {
  if (!Array.isArray(items) || items.length > maximum) invalid();
  let last = after;
  return items.map((row) => {
    const value = project(row, kind);
    if (value.id <= last) invalid();
    last = value.id;
    return value;
  });
}
export function choicePage(
  response,
  kind,
  previous = emptyChoices(),
  expectedETag,
) {
  tag(response?.etag, previous.etag || expectedETag);
  exact(response.data, ["items", "next_cursor"]);
  const after = previous.next || "";
  if (previous.items.length && !previous.next) invalid();
  const items = rows(response.data.items, kind, choiceLimit, after),
    next = response.data.next_cursor;
  if (
    next !== null &&
    (!validID(next) ||
      items.length !== choiceLimit ||
      next !== items.at(-1)?.id)
  )
    invalid();
  if (kind === "roles" && previous.items.length + items.length > 100) invalid();
  return { items: [...previous.items, ...items], next, etag: response.etag };
}
export function memberBindings(response, expectedETag) {
  tag(response?.etag, expectedETag);
  exact(response.data, ["items"]);
  return rows(response.data.items, "roles", 100);
}
