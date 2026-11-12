import { ApiError } from "./api.js";
export const historyLimit = 20;
export const historyActions = {
  "application.user_access.mode_changed": "修改访问模式",
  "application.user_access.granted": "授予成员访问",
  "application.user_access.revoked": "撤销成员访问",
};
const id = (v) =>
  typeof v === "string" && /^[a-f0-9]{32}$/.test(v) && !/^0+$/.test(v);
const date =
  /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?(?:Z|[+-]\d{2}:\d{2})$/;
function invalid() {
  throw new ApiError(502, "invalid_access_history");
}
function exact(v, keys) {
  if (
    !v ||
    typeof v !== "object" ||
    Array.isArray(v) ||
    Object.keys(v).length !== keys.length ||
    keys.some((k) => !Object.hasOwn(v, k))
  )
    invalid();
}
export function accessHistory(response, tenant, application, after = "") {
  const d = response?.data;
  exact(d, ["format", "tenant_id", "application_id", "items", "next_cursor"]);
  if (
    response.etag !== null ||
    !id(tenant) ||
    !id(application) ||
    (after && !id(after)) ||
    d.format !== "sso.application.user-access-history.v1" ||
    d.tenant_id !== tenant ||
    d.application_id !== application ||
    !Array.isArray(d.items) ||
    d.items.length > historyLimit
  )
    invalid();
  let last = after;
  const items = d.items.map((v) => {
    exact(v, [
      "id",
      "actor_membership_id",
      "membership_id",
      "action",
      "occurred_at",
      "mode",
      "version",
    ]);
    if (
      !id(v.id) ||
      v.id <= last ||
      !id(v.actor_membership_id) ||
      !Object.hasOwn(historyActions, v.action) ||
      !["all_members", "selected_members"].includes(v.mode) ||
      typeof v.version !== "string" ||
      !/^[1-9][0-9]{0,18}$/.test(v.version) ||
      BigInt(v.version) > 9223372036854775807n ||
      typeof v.occurred_at !== "string" ||
      v.occurred_at.length > 40 ||
      !date.test(v.occurred_at) ||
      !Number.isFinite(Date.parse(v.occurred_at)) ||
      (v.action === "application.user_access.mode_changed"
        ? v.membership_id !== null
        : !id(v.membership_id))
    )
      invalid();
    last = v.id;
    return { ...v };
  });
  if (
    d.next_cursor !== null &&
    (!id(d.next_cursor) ||
      items.length !== historyLimit ||
      d.next_cursor !== last)
  )
    invalid();
  return { items, next: d.next_cursor };
}
