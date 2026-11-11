import test from "node:test";
import assert from "node:assert/strict";
import { validateProtocolObservation } from "../../web/console/src/protocol-profile.js";
const tenant = "a".repeat(32),
  application = "b".repeat(32),
  resource = "c".repeat(32),
  principal = "d".repeat(32);
const expected = (kind = "public_client") => ({
  tenant,
  application,
  resource,
  principal,
  kind,
  revision: 1,
});
function observation(kind = "public_client") {
  return {
    data: {
      resource_kind: kind,
      tenant_id: tenant,
      application_id: application,
      service_principal_id: kind === "public_client" ? null : principal,
      resource_id: resource,
      revision: 1,
      resource_etag: `"${kind === "public_client" ? "client" : "cred"}-${tenant}-${resource}-1"`,
      local_state: "pending",
      profile_state: "matching",
      differences: [],
      observed_at: "2026-11-11T21:00:00.123456789Z",
      authority_checked_at: "2026-11-11T21:00:00.123456790Z",
    },
    etag: `"${kind === "public_client" ? "client" : "cred"}-${tenant}-${resource}-1"`,
  };
}
test("reviewed observations retain kind, namespace and revision without a readiness claim", () => {
  for (const kind of ["public_client", "machine_credential"]) {
    const r = observation(kind),
      v = validateProtocolObservation(r, expected(kind));
    assert.deepEqual(v, r.data);
    assert.notEqual(v, r.data);
    assert.equal(v.local_state, "pending");
  }
});
test("foreign, partial and secret-bearing observations cannot enter the UI", () => {
  for (const change of [
    (r) => (r.data.tenant_id = resource),
    (r) => (r.data.application_id = resource),
    (r) => (r.data.resource_id = tenant),
    (r) => (r.data.service_principal_id = principal),
    (r) => (r.data.secret_key = "private-observation-never-echo"),
    (r) => (r.data.observed_at = "invalid"),
    (r) => (r.data.local_state = "ready"),
  ]) {
    const r = observation();
    change(r);
    assert.throws(
      () => validateProtocolObservation(r, expected()),
      (e) =>
        e.code === "invalid_protocol_data" &&
        !e.message.includes("private-observation"),
    );
  }
});
test("difference field names are enumerated and consistent with the observed state", () => {
  const r = observation();
  r.data.profile_state = "conflict";
  r.data.differences = ["scope", "metadata"];
  assert.doesNotThrow(() => validateProtocolObservation(r, expected()));
  for (const fields of [
    [],
    ["scope", "scope"],
    ["client_secret"],
    ["__proto__"],
    [{ scope: "unsafe" }],
  ]) {
    r.data.differences = fields;
    assert.throws(() => validateProtocolObservation(r, expected()));
  }
  r.data.profile_state = "absent";
  r.data.differences = [];
  assert.doesNotThrow(() => validateProtocolObservation(r, expected()));
  r.data.differences = ["scope"];
  assert.throws(() => validateProtocolObservation(r, expected()));
});
test("strong resource ETags, safe revisions and machine principal bindings are required", () => {
  for (const change of [
    (r) => (r.data.resource_etag = "W/" + r.data.resource_etag),
    (r) =>
      (r.data.resource_etag = r.data.resource_etag.replace("client-", "cred-")),
    (r) => (r.data.revision = 2),
    (r) => (r.data.revision = Number.MAX_SAFE_INTEGER + 1),
  ]) {
    const r = observation();
    change(r);
    assert.throws(() => validateProtocolObservation(r, expected()));
  }
  const m = observation("machine_credential");
  m.data.service_principal_id = resource;
  assert.throws(() =>
    validateProtocolObservation(m, expected("machine_credential")),
  );
});
