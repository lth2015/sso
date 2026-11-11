// Directory metadata is a selection aid; the primary authorizes every mutation.
import { ApiError } from "./api.js";
import { choiceLimit, emptyChoices, choicePage } from "./role-choices.js";
export { choiceLimit, emptyChoices };
const id = /^[a-f0-9]{32}$/;
const validID = (value) => typeof value === "string" && id.test(value);
function invalid() {
  throw new ApiError(502, "invalid_assignment_choices");
}
export function assignmentMembers(response, previous = emptyChoices()) {
  try {
    return choicePage(response, "members", previous);
  } catch {
    invalid();
  }
}
export function assignmentOrganizations(
  response,
  root,
  previous = emptyChoices(),
) {
  const data = response?.data;
  if (
    !validID(root) ||
    typeof response?.etag !== "string" ||
    !/^"org-[1-9][0-9]*"$/.test(response.etag) ||
    (previous.etag && response.etag !== previous.etag) ||
    !data ||
    typeof data !== "object" ||
    Array.isArray(data) ||
    Object.keys(data).length !== 2 ||
    !Object.hasOwn(data, "items") ||
    !Object.hasOwn(data, "next_cursor") ||
    !Array.isArray(data.items) ||
    data.items.length > choiceLimit ||
    (previous.items.length && !previous.next)
  )
    invalid();
  let last = previous.next || "";
  const items = data.items.map((row) => {
    if (
      !row ||
      typeof row !== "object" ||
      Array.isArray(row) ||
      Object.keys(row).length !== 3 ||
      !["id", "parent_id", "name"].every((k) => Object.hasOwn(row, k)) ||
      !validID(row.id) ||
      row.id <= last ||
      !(
        row.parent_id === null ||
        (validID(row.parent_id) && row.parent_id !== row.id)
      ) ||
      (row.parent_id === null) !== (row.id === root) ||
      typeof row.name !== "string" ||
      !row.name.trim() ||
      new TextEncoder().encode(row.name).length > 128
    )
      invalid();
    last = row.id;
    return { id: row.id, parent_id: row.parent_id, name: row.name };
  });
  const next = data.next_cursor;
  if (
    next !== null &&
    (!validID(next) ||
      items.length !== choiceLimit ||
      next !== items.at(-1)?.id)
  )
    invalid();
  return { items: [...previous.items, ...items], next, etag: response.etag };
}
