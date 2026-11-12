import test from "node:test";
import assert from "node:assert/strict";
import { accessHistory } from "../../web/console/src/application-access-history.js";
const id = (n) => n.toString(16).padStart(32, "0"),
  tenant = id(1),
  app = id(2);
const entry = (n) => ({
  id: id(n),
  actor_membership_id: id(3),
  membership_id: id(4),
  action: "application.user_access.granted",
  occurred_at: "2026-11-12T01:00:00Z",
  mode: "selected_members",
  version: "9223372036854775807",
});
const response = (items, next = null) => ({
  etag: null,
  data: {
    format: "sso.application.user-access-history.v1",
    tenant_id: tenant,
    application_id: app,
    items,
    next_cursor: next,
  },
});
test("captured admission history has a fixed namespace and no HTTP validator", () => {
  const r = response([entry(5)]);
  assert.equal(
    accessHistory(r, tenant, app).items[0].version,
    "9223372036854775807",
  );
  for (const bad of [
    { ...r, etag: '"ua-any"' },
    { ...r, data: { ...r.data, application_id: id(8) } },
    { ...r, data: { ...r.data, secret_key: "hidden" } },
  ])
    assert.throws(() => accessHistory(bad, tenant, app));
});
test("history target/action semantics and decimal revisions reject malformed metadata", () => {
  for (const v of [
    { ...entry(5), membership_id: null },
    { ...entry(5), action: "application.user_access.mode_changed" },
    { ...entry(5), version: 2 },
    { ...entry(5), version: "01" },
    { ...entry(5), version: "9223372036854775808" },
    { ...entry(5), occurred_at: "invalid" },
    { ...entry(5), actor_membership_id: id(0) },
    { ...entry(5), private_payload: "hidden" },
  ])
    assert.throws(() => accessHistory(response([v]), tenant, app));
  assert.equal(
    accessHistory(
      response([
        {
          ...entry(5),
          action: "application.user_access.mode_changed",
          membership_id: null,
        },
      ]),
      tenant,
      app,
    ).items[0].membership_id,
    null,
  );
});
test("history pages remain bounded and advance from an exact last event", () => {
  const rows = Array.from({ length: 20 }, (_, n) => entry(n + 5));
  assert.equal(accessHistory(response(rows, id(24)), tenant, app).next, id(24));
  assert.equal(
    accessHistory(response([entry(25)]), tenant, app, id(24)).items.length,
    1,
  );
  for (const r of [
    response([...rows, entry(25)]),
    response([entry(5)], id(5)),
    response([entry(5), entry(5)]),
    response(rows, id(23)),
  ])
    assert.throws(() => accessHistory(r, tenant, app));
  assert.throws(() =>
    accessHistory(response([entry(24)]), tenant, app, id(24)),
  );
});
test("an empty captured history is distinct from current admission", () => {
  assert.deepEqual(accessHistory(response([]), tenant, app), {
    items: [],
    next: null,
  });
  for (const bad of [
    { items: [], next_cursor: null },
    { policy: { mode: "selected_members" }, items: [], next_cursor: null },
  ])
    assert.throws(() => accessHistory({ etag: null, data: bad }, tenant, app));
});
