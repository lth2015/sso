import test from "node:test";
import assert from "node:assert/strict";
import {
  choicePage,
  emptyChoices,
  memberBindings,
} from "../../web/console/src/role-choices.js";
const id = (n) => n.toString(16).padStart(32, "0"),
  etag = '"authz-5"';
const member = (n) => ({
  id: id(n),
  user_id: id(n + 1000),
  email: null,
  status: "active",
  owner: false,
});
const role = (n) => ({
  id: id(n),
  code: "role_" + n,
  built_in: false,
  scope: "tenant",
  policies: [],
  binding_count: 0,
});
const page = (items, next = null) => ({
  etag,
  data: { items, next_cursor: next },
});
test("bounded pages advance without truncating after one hundred members", () => {
  let state = emptyChoices();
  for (let p = 0; p < 5; p++)
    state = choicePage(
      page(
        Array.from({ length: 25 }, (_, i) => member(p * 25 + i + 1)),
        p < 4 ? id((p + 1) * 25) : null,
      ),
      "members",
      state,
    );
  assert.equal(state.items.length, 125);
  assert.equal(state.next, null);
  assert.throws(() => choicePage(page([member(126)]), "members", state));
});
test("role/member projections reject mismatched versions, duplicate or regressing pages and extra secrets", () => {
  const state = choicePage(
    page(
      Array.from({ length: 25 }, (_, i) => member(i + 1)),
      id(25),
    ),
    "members",
  );
  for (const r of [
    page([member(25)]),
    page([member(27), member(26)]),
    page([member(26), member(26)]),
    { etag: '"authz-6"', data: { items: [member(26)], next_cursor: null } },
    page([{ ...member(26), secret_key: "never-display" }]),
    page([member(26)], id(26)),
    page(Array.from({ length: 26 }, (_, i) => member(26 + i))),
  ]) {
    assert.throws(
      () => choicePage(r, "members", state),
      (e) =>
        e.code === "invalid_role_choices" &&
        !e.message.includes("never-display"),
    );
  }
});
test("initial choices require a strong version and the expected authority version", () => {
  for (const tag of [null, 'W/"authz-5"', '"org-5"', '"authz-0"', '"authz-6"'])
    assert.throws(() =>
      choicePage(
        { etag: tag, data: { items: [member(1)], next_cursor: null } },
        "members",
        emptyChoices(),
        etag,
      ),
    );
  assert.throws(() => choicePage(page(null), "members"));
});
test("bindings remain complete and independent from a role choice page", () => {
  const items = Array.from({ length: 100 }, (_, i) => role(i + 1)),
    r = { etag, data: { items } };
  assert.equal(memberBindings(r, etag).length, 100);
  assert.notEqual(memberBindings(r, etag)[0], items[0]);
  assert.throws(() => memberBindings({ ...r, etag: '"authz-6"' }, etag));
  assert.throws(() =>
    memberBindings({ etag, data: { items: [role(2), role(1)] } }, etag),
  );
});
test("malformed policies and owner metadata cannot enter the chooser", () => {
  for (const row of [
    { ...role(1), code: "owner" },
    { ...role(1), built_in: true },
    { ...role(1), scope: "application" },
    { ...role(1), binding_count: -1 },
    { ...role(1), policies: [{ permission: "roles.manage", effect: "allow" }] },
    {
      ...role(1),
      policies: [
        { permission: "audit.read", effect: "allow" },
        { permission: "audit.read", effect: "allow" },
      ],
    },
  ])
    assert.throws(() => choicePage(page([row]), "roles"));
  const owner = { ...role(1), code: "owner", built_in: true };
  assert.equal(choicePage(page([owner]), "roles").items[0].built_in, true);
});
test("role choices retain the service's existing one-hundred-role cap", () => {
  const previous = {
    items: Array.from({ length: 100 }, (_, i) => role(i + 1)),
    next: id(100),
    etag,
  };
  assert.throws(() => choicePage(page([role(101)]), "roles", previous));
});
