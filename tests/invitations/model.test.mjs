import test from "node:test";
import assert from "node:assert/strict";
import {
  validateInvitationDetail,
  validateReissuedInvitation,
} from "../../web/console/src/invitation.js";
const tenant = "a".repeat(32),
  id = "b".repeat(32),
  token = "A".repeat(43);
const tag = (n) => `"invite-${tenant}-${id}-${n}"`;
const metadata = () => ({
  data: {
    id,
    email: "target@example.invalid",
    expires_at: 1794427200,
    state: "pending",
    revision: 1,
  },
  etag: tag(1),
});
const issued = () => ({
  data: { id, token, expires_in: 86400, revision: 2 },
  etag: tag(2),
});
test("metadata is a frozen nonsecret projection bound to its tenant/resource/revision", () => {
  assert.deepEqual(
    validateInvitationDetail(metadata(), tenant, id),
    metadata(),
  );
  for (const change of [
    (r) => (r.data.token = token),
    (r) => (r.data.id = "c".repeat(32)),
    (r) => (r.etag = '"invite-other"'),
    (r) => (r.data.revision = 0),
    (r) => (r.data.email = "unsafe\n@example.invalid"),
    (r) => (r.data.expires_at = 0),
    (r) => (r.data.state = "active"),
  ]) {
    const r = metadata();
    change(r);
    assert.throws(
      () => validateInvitationDetail(r, tenant, id),
      (e) => e.code === "invalid_invitation_data" && !e.message.includes(token),
    );
  }
});
test("closed and expired remain distinct valid metadata states without a proof", () => {
  for (const state of ["expired", "closed"]) {
    const r = metadata();
    r.data.state = state;
    assert.equal(validateInvitationDetail(r, tenant, id).data.state, state);
  }
});
test("one-time result requires the exact next revision and matching strong ETag", () => {
  assert.deepEqual(validateReissuedInvitation(issued(), tenant, id, 1), {
    token,
    revision: 2,
  });
  for (const change of [
    (r) => (r.data.revision = 1),
    (r) => (r.data.id = "c".repeat(32)),
    (r) => (r.data.expires_in = 999),
    (r) => (r.data.token = "bad"),
    (r) => (r.data.secret_key = "extra"),
    (r) => (r.etag = "W/" + tag(2)),
    (r) => (r.etag = tag(1)),
  ]) {
    const r = issued();
    change(r);
    assert.throws(
      () => validateReissuedInvitation(r, tenant, id, 1),
      (e) => e.code === "invalid_invitation_data" && !e.message.includes(token),
    );
  }
});
test("foreign namespace, invalid IDs and unsafe numeric revisions fail closed", () => {
  assert.throws(() => validateInvitationDetail(metadata(), "c".repeat(32), id));
  assert.throws(() => validateInvitationDetail(metadata(), tenant, "invalid"));
  assert.throws(() =>
    validateReissuedInvitation(
      issued(),
      tenant,
      id,
      Number.MAX_SAFE_INTEGER + 1,
    ),
  );
  const r = metadata();
  r.data.revision = Number.MAX_SAFE_INTEGER + 1;
  assert.throws(() => validateInvitationDetail(r, tenant, id));
});
