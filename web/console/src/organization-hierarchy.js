import { ApiError } from "./api.js";
import { assignmentOrganizations } from "./organization-assignment.js";
export { choiceLimit, emptyChoices } from "./organization-assignment.js";
function invalid() {
  throw new ApiError(502, "invalid_hierarchy_data");
}
export function parentChoices(response, root, previous, sourceETag) {
  try {
    const value = assignmentOrganizations(response, root, previous);
    if (sourceETag !== undefined && sourceETag !== value.etag) invalid();
    return value;
  } catch {
    invalid();
  }
}
function version(etag) {
  if (typeof etag !== "string" || !/^"org-[1-9][0-9]*"$/.test(etag)) invalid();
  return BigInt(etag.slice(5, -1));
}
export function moveResult(response, etag, dryRun) {
  const v = response?.data;
  if (
    !v ||
    typeof v !== "object" ||
    Array.isArray(v) ||
    Object.keys(v).length !== 4 ||
    !["subtree_nodes", "resulting_depth", "org_version", "dry_run"].every((k) =>
      Object.hasOwn(v, k),
    ) ||
    !Number.isSafeInteger(v.subtree_nodes) ||
    v.subtree_nodes < 1 ||
    !Number.isSafeInteger(v.resulting_depth) ||
    v.resulting_depth < 1 ||
    v.resulting_depth > 16 ||
    !Number.isSafeInteger(v.org_version) ||
    v.org_version < 1 ||
    v.dry_run !== dryRun ||
    BigInt(v.org_version) !== version(etag) + (dryRun ? 0n : 1n)
  )
    invalid();
  return { nodes: v.subtree_nodes, depth: v.resulting_depth };
}
export function createdOrganization(response, etag) {
  const v = response?.data;
  if (
    !v ||
    typeof v !== "object" ||
    Array.isArray(v) ||
    Object.keys(v).length !== 2 ||
    !["id", "org_version"].every((k) => Object.hasOwn(v, k)) ||
    typeof v.id !== "string" ||
    !/^[a-f0-9]{32}$/.test(v.id) ||
    !Number.isSafeInteger(v.org_version) ||
    v.org_version < 1 ||
    BigInt(v.org_version) !== version(etag) + 1n ||
    version(response.etag) !== BigInt(v.org_version)
  )
    invalid();
  return v.id;
}
export function organizationName(value) {
  return (
    typeof value === "string" &&
    value.trim().length > 0 &&
    new TextEncoder().encode(value.trim()).length <= 128
  );
}
