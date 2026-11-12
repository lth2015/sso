import { ApiError } from "./api.js";
import { choicePage, emptyChoices } from "./role-choices.js";
export { emptyChoices };
export const accessLimit = 25;
export const validMember = (v) =>
  typeof v === "string" && /^[a-f0-9]{32}$/.test(v);
function invalid() {
  throw new ApiError(502, "invalid_user_access_data");
}
function exact(v, keys) {
  return (
    v &&
    typeof v === "object" &&
    !Array.isArray(v) &&
    Object.keys(v).length === keys.length &&
    keys.every((k) => Object.hasOwn(v, k))
  );
}
export function accessPolicy(data, tenant, app) {
  if (
    !exact(data, ["tenant_id", "application_id", "mode", "version"]) ||
    data.tenant_id !== tenant ||
    data.application_id !== app ||
    !["all_members", "selected_members"].includes(data.mode) ||
    typeof data.version !== "string" ||
    !/^[1-9][0-9]{0,18}$/.test(data.version) ||
    BigInt(data.version) > 9223372036854775807n
  )
    invalid();
  return { ...data };
}
export function accessTag(policy) {
  return `"ua-${policy.tenant_id}-${policy.application_id}-${policy.version}"`;
}
export function accessPage(response, tenant, app, previous) {
  const d = response?.data;
  if (
    !exact(d, ["policy", "items", "next_cursor"]) ||
    !Array.isArray(d.items) ||
    d.items.length > accessLimit
  )
    invalid();
  const policy = accessPolicy(d.policy, tenant, app),
    etag = accessTag(policy);
  if (
    response.etag !== etag ||
    (previous && (previous.etag !== etag || !previous.next))
  )
    invalid();
  let last = previous?.next || "";
  const items = d.items.map((v) => {
    if (
      !exact(v, ["membership_id"]) ||
      !validMember(v.membership_id) ||
      v.membership_id <= last
    )
      invalid();
    last = v.membership_id;
    return { membership_id: last };
  });
  if (
    d.next_cursor !== null &&
    (!validMember(d.next_cursor) ||
      items.length !== accessLimit ||
      d.next_cursor !== last)
  )
    invalid();
  return {
    policy,
    etag,
    items: [...(previous?.items || []), ...items],
    next: d.next_cursor,
  };
}
export function accessDecision(response, policy, member) {
  const d = response?.data;
  if (
    !exact(d, [
      "policy",
      "membership_id",
      "allowed",
      "reason",
      "explicit_grant",
    ]) ||
    !validMember(member) ||
    d.membership_id !== member ||
    typeof d.allowed !== "boolean" ||
    typeof d.explicit_grant !== "boolean" ||
    response.etag !== null
  )
    invalid();
  const p = accessPolicy(d.policy, policy.tenant_id, policy.application_id);
  if (p.version !== policy.version || p.mode !== policy.mode) invalid();
  const allowed = ["owner", "all_members", "explicit_grant"],
    denied = [
      "not_granted",
      "user_inactive",
      "membership_inactive",
      "application_disabled",
    ];
  if (
    !(d.allowed ? allowed : denied).includes(d.reason) ||
    (d.reason === "all_members" && p.mode !== "all_members") ||
    (d.reason === "explicit_grant" &&
      (p.mode !== "selected_members" || !d.explicit_grant)) ||
    (d.reason === "not_granted" &&
      (p.mode !== "selected_members" || d.explicit_grant))
  )
    invalid();
  return { ...d, policy: p };
}
export function accessWrite(response, previous, mode = previous.mode) {
  const p = accessPolicy(
    response?.data,
    previous.tenant_id,
    previous.application_id,
  );
  if (
    response.etag !== accessTag(p) ||
    p.mode !== mode ||
    BigInt(p.version) !== BigInt(previous.version) + 1n
  )
    invalid();
  return p;
}
export function accessMembers(response, previous = emptyChoices()) {
  try {
    return choicePage(response, "members", previous);
  } catch {
    invalid();
  }
}
export const decisionText = {
  owner: "当前所有者始终可访问",
  all_members: "当前模式允许全体有效成员",
  explicit_grant: "已获得此应用的显式授权",
  not_granted: "尚未获得此应用的访问授权",
  membership_inactive: "成员当前未启用",
  user_inactive: "账户当前未启用",
  application_disabled: "应用当前未启用或已被平台暂停",
};
