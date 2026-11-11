const id = /^[a-f0-9]{32}$/;
const proof = /^[A-Za-z0-9_-]{43}$/;
function invalid() {
  const error = new Error("邀请数据不正确，请关闭后重新核对。");
  error.code = "invalid_invitation_data";
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
export function validateInvitationDetail(response, tenant, expectedID) {
  const value = response.data;
  if (
    !id.test(tenant) ||
    !id.test(expectedID) ||
    !exact(value, ["id", "email", "expires_at", "state", "revision"]) ||
    value.id !== expectedID ||
    typeof value.email !== "string" ||
    value.email.length < 3 ||
    value.email.length > 254 ||
    /[\x00-\x20\x7f]/.test(value.email) ||
    !Number.isSafeInteger(value.expires_at) ||
    value.expires_at < 1 ||
    !["pending", "expired", "closed"].includes(value.state) ||
    !Number.isSafeInteger(value.revision) ||
    value.revision < 1 ||
    response.etag !== `"invite-${tenant}-${expectedID}-${value.revision}"`
  )
    invalid();
  return { data: { ...value }, etag: response.etag };
}
export function validateReissuedInvitation(
  response,
  tenant,
  expectedID,
  previous,
) {
  const value = response.data;
  if (
    !id.test(tenant) ||
    !id.test(expectedID) ||
    !Number.isSafeInteger(previous) ||
    previous < 1 ||
    !exact(value, ["id", "token", "expires_in", "revision"]) ||
    value.id !== expectedID ||
    typeof value.token !== "string" ||
    !proof.test(value.token) ||
    value.expires_in !== 86400 ||
    !Number.isSafeInteger(value.revision) ||
    value.revision !== previous + 1 ||
    response.etag !== `"invite-${tenant}-${expectedID}-${value.revision}"`
  )
    invalid();
  return { token: value.token, revision: value.revision };
}
