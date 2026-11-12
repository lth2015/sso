import test from "node:test";
import assert from "node:assert/strict";
import {
  parentChoices,
  moveResult,
  createdOrganization,
  organizationName,
} from "../../web/console/src/organization-hierarchy.js";
const id = (n) => n.toString(16).padStart(32, "0"),
  tag = '"org-7"';
const response = (data) => ({ data });
const plan = {
  subtree_nodes: 2,
  resulting_depth: 3,
  org_version: 7,
  dry_run: true,
};
test("moving source and every directory page share the same structure version", () => {
  const page = {
    etag: tag,
    data: {
      items: [{ id: id(1), parent_id: null, name: "Root" }],
      next_cursor: null,
    },
  };
  assert.equal(parentChoices(page, id(1), undefined, tag).items.length, 1);
  assert.throws(() => parentChoices(page, id(1), undefined, '"org-6"'));
  for (const missing of [null, ""])
    assert.throws(() => parentChoices(page, id(1), undefined, missing));
  assert.throws(
    () =>
      parentChoices(
        { ...page, data: { ...page.data, secret_key: "never-render-private" } },
        id(1),
      ),
    (e) =>
      e.code === "invalid_hierarchy_data" && !e.message.includes("private"),
  );
});
test("preview binds exact version, shape and safe bounded subtree/depth values", () => {
  assert.deepEqual(moveResult(response(plan), tag, true), {
    nodes: 2,
    depth: 3,
  });
  for (const v of [
    { ...plan, dry_run: false },
    { ...plan, org_version: 8 },
    { ...plan, subtree_nodes: 0 },
    { ...plan, resulting_depth: 17 },
    { ...plan, org_version: 9007199254740992 },
    { ...plan, resulting_depth: "3" },
    { ...plan, secret_key: "never-render-private" },
  ])
    assert.throws(() => moveResult(response(v), tag, true));
  assert.throws(() => moveResult(response(plan), '"authz-7"', true));
});
test("commit responses must show exactly the next structure version", () => {
  assert.deepEqual(
    moveResult(
      response({ ...plan, dry_run: false, org_version: 8 }),
      tag,
      false,
    ),
    { nodes: 2, depth: 3 },
  );
  const created = { etag: '"org-8"', data: { id: id(8), org_version: 8 } };
  assert.equal(createdOrganization(created, tag), id(8));
  for (const r of [
    { ...created, etag: tag },
    { ...created, data: { ...created.data, id: [id(8)] } },
    { ...created, data: { ...created.data, org_version: 9 } },
    { ...created, data: { ...created.data, secret_key: "private" } },
  ])
    assert.throws(() => createdOrganization(r, tag));
});
test("organization names use the server's trimmed UTF-8 byte bound", () => {
  assert(organizationName("  " + "界".repeat(42) + "  "));
  assert(organizationName("a".repeat(128)));
  for (const name of [null, [], "  ", "界".repeat(43), "a".repeat(129)])
    assert(!organizationName(name));
});
