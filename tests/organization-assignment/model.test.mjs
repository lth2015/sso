import test from "node:test";
import assert from "node:assert/strict";
import {
  assignmentMembers,
  assignmentOrganizations,
  emptyChoices,
} from "../../web/console/src/organization-assignment.js";
const id = (n) => n.toString(16).padStart(32, "0"),
  root = id(1),
  etag = '"org-8"';
const org = (n) => ({
  id: id(n),
  parent_id: n === 1 ? null : root,
  name: "node_" + n,
});
const page = (items, next = null, tag = etag) => ({
  etag: tag,
  data: { items, next_cursor: next },
});
test("organization pages remain independent from the main list and grow beyond fifty", () => {
  let state = emptyChoices();
  for (let p = 0; p < 3; p++)
    state = assignmentOrganizations(
      page(
        Array.from({ length: 25 }, (_, i) => org(p * 25 + i + 1)),
        p < 2 ? id((p + 1) * 25) : null,
      ),
      root,
      state,
    );
  assert.equal(state.items.length, 75);
  assert.equal(state.etag, etag);
  assert.equal(state.next, null);
  assert.throws(() => assignmentOrganizations(page([org(76)]), root, state));
});
test("organization structure versions never substitute for member authorization versions", () => {
  assert.throws(() =>
    assignmentOrganizations(page([org(1)], null, '"authz-8"'), root),
  );
  const member = {
    id: id(1),
    user_id: id(2),
    email: null,
    status: "active",
    owner: false,
  };
  assert.throws(() => assignmentMembers(page([member])));
  assert.equal(
    assignmentMembers(page([member], null, '"authz-9"')).etag,
    '"authz-9"',
  );
  const state = assignmentOrganizations(
    page(
      Array.from({ length: 25 }, (_, i) => org(i + 1)),
      id(25),
    ),
    root,
  );
  assert.throws(() =>
    assignmentOrganizations(page([org(26)], null, '"org-9"'), root, state),
  );
});
test("foreign roots, partial fields, extra private values and invalid names are rejected", () => {
  for (const row of [
    { ...org(1), id: id(3) },
    { ...org(2), parent_id: null },
    { ...org(1), parent_id: id(2) },
    { ...org(2), parent_id: id(2) },
    { ...org(2), secret_key: "never-display-private-choice" },
    { ...org(2), name: " " },
    { ...org(2), name: "界".repeat(43) },
    { ...org(2), id: [id(2)] },
  ])
    assert.throws(
      () => assignmentOrganizations(page([row]), root),
      (e) =>
        e.code === "invalid_assignment_choices" &&
        !e.message.includes("never-display"),
    );
});
test("page order, unique IDs, continuation, size and field completeness fail closed", () => {
  for (const response of [
    page([org(2), org(1)]),
    page([org(2), org(2)]),
    page([org(1)], root),
    page(Array.from({ length: 26 }, (_, i) => org(i + 1))),
    { etag, data: { items: null, next_cursor: null } },
    { etag, data: { items: [] } },
  ])
    assert.throws(() => assignmentOrganizations(response, root));
  const state = assignmentOrganizations(
    page(
      Array.from({ length: 25 }, (_, i) => org(i + 1)),
      id(25),
    ),
    root,
  );
  assert.throws(() => assignmentOrganizations(page([org(25)]), root, state));
});
test("member projection errors are fixed assignment errors without private response data", () => {
  assert.throws(
    () =>
      assignmentMembers(
        page([{ secret_key: "never-display" }], null, '"authz-1"'),
      ),
    (e) =>
      e.code === "invalid_assignment_choices" &&
      !e.message.includes("never-display"),
  );
});
