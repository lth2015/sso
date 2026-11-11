const id = /^[a-f0-9]{32}$/;
const date = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?Z$/;
export const profileFields = {
  client_id: "客户端标识",
  client_name: "登记名称",
  token_endpoint_auth_method: "客户端认证方式",
  redirect_uris: "精确回调 URI",
  grant_types: "授权类型",
  response_types: "响应类型",
  scope: "授权范围",
  audience: "Audience",
  metadata: "所有权元数据",
};
const keys = [
  "resource_kind",
  "tenant_id",
  "application_id",
  "service_principal_id",
  "resource_id",
  "revision",
  "resource_etag",
  "local_state",
  "profile_state",
  "differences",
  "observed_at",
  "authority_checked_at",
];
export function validateProtocolObservation(response, expected) {
  const v = response.data;
  const bad = () => {
    const e = new Error("协议观察数据不正确，请刷新当前记录后重新核对。");
    e.code = "invalid_protocol_data";
    throw e;
  };
  if (
    !v ||
    typeof v !== "object" ||
    Array.isArray(v) ||
    Object.keys(v).length !== keys.length ||
    !keys.every((k) => Object.hasOwn(v, k)) ||
    ![expected.tenant, expected.application, expected.resource].every(
      (x) => typeof x === "string" && id.test(x),
    ) ||
    !["public_client", "machine_credential"].includes(expected.kind) ||
    v.resource_kind !== expected.kind ||
    v.tenant_id !== expected.tenant ||
    v.application_id !== expected.application ||
    v.resource_id !== expected.resource ||
    !Number.isSafeInteger(v.revision) ||
    v.revision < 1 ||
    v.revision !== expected.revision ||
    (expected.kind === "public_client"
      ? v.service_principal_id !== null
      : typeof expected.principal !== "string" ||
        !id.test(expected.principal) ||
        v.service_principal_id !== expected.principal) ||
    !(
      expected.kind === "public_client"
        ? ["pending", "active", "revoked"]
        : ["pending", "active", "expired", "revoked"]
    ).includes(v.local_state) ||
    !["matching", "absent", "conflict"].includes(v.profile_state) ||
    !Array.isArray(v.differences) ||
    v.differences.length > 9 ||
    new Set(v.differences).size !== v.differences.length ||
    !v.differences.every(
      (x) => typeof x === "string" && Object.hasOwn(profileFields, x),
    ) ||
    (v.profile_state === "conflict"
      ? v.differences.length === 0
      : v.differences.length !== 0) ||
    ![v.observed_at, v.authority_checked_at].every(
      (x) =>
        typeof x === "string" &&
        x.length <= 30 &&
        date.test(x) &&
        Number.isFinite(Date.parse(x)),
    ) ||
    v.resource_etag !==
      `"${expected.kind === "public_client" ? "client" : "cred"}-${expected.tenant}-${expected.resource}-${v.revision}"`
  )
    bad();
  return JSON.parse(JSON.stringify(v));
}
