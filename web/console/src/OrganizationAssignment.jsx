import React, { useEffect, useRef, useState } from "react";
import { request, errorText } from "./api.js";
import {
  choiceLimit,
  emptyChoices,
  assignmentMembers,
  assignmentOrganizations,
} from "./organization-assignment.js";
export default function OrganizationAssignment({
  context,
  csrf,
  tenant,
  root,
  onClose,
  onFailure,
  onChanged,
}) {
  const [members, setMembers] = useState(emptyChoices),
    [organizations, setOrganizations] = useState(emptyChoices),
    [member, setMember] = useState(""),
    [organization, setOrganization] = useState(""),
    [confirmed, setConfirmed] = useState(false),
    [loading, setLoading] = useState(false),
    [writing, setWriting] = useState(false),
    [error, setError] = useState(null),
    [unavailable, setUnavailable] = useState(false);
  const dialog = useRef(null),
    alive = useRef(true),
    epoch = useRef(0),
    pending = useRef(null),
    submitted = useRef(false),
    base = "/bff/v1/tenants/" + tenant;
  const current = (generation, abort) =>
    alive.current &&
    generation === epoch.current &&
    !abort?.signal.aborted &&
    location.hash === "#workspace=" + context;
  function clearSelection() {
    setMember("");
    setOrganization("");
    setConfirmed(false);
  }
  function fail(e) {
    clearSelection();
    setMembers(emptyChoices());
    setOrganizations(emptyChoices());
    setUnavailable(true);
    setError(e);
    if (e.status === 401) onFailure(e);
  }
  function close() {
    if (submitted.current) return;
    epoch.current++;
    pending.current?.abort();
    onClose();
  }
  useEffect(() => {
    alive.current = true;
    dialog.current.showModal();
    load();
    return () => {
      alive.current = false;
      epoch.current++;
      pending.current?.abort();
    };
  }, []);
  async function load(kind) {
    if (pending.current || submitted.current || unavailable) return;
    const abort = new AbortController(),
      generation = ++epoch.current;
    pending.current = abort;
    setLoading(true);
    setConfirmed(false);
    setError(null);
    try {
      if (!kind || kind === "members") {
        const previous = kind ? members : emptyChoices(),
          r = await request(
            context,
            csrf,
            base +
              "/members?limit=" +
              choiceLimit +
              (previous.next ? "&after=" + previous.next : ""),
            { signal: abort.signal },
          );
        if (!current(generation, abort)) return;
        setMembers(assignmentMembers(r, previous));
      }
      if (!kind || kind === "organizations") {
        const previous = kind ? organizations : emptyChoices(),
          r = await request(
            context,
            csrf,
            base +
              "/organizations?limit=" +
              choiceLimit +
              (previous.next ? "&after=" + previous.next : ""),
            { signal: abort.signal },
          );
        if (!current(generation, abort)) return;
        setOrganizations(assignmentOrganizations(r, root, previous));
      }
    } catch (e) {
      if (current(generation, abort)) fail(e);
    } finally {
      if (current(generation, abort)) {
        pending.current = null;
        setLoading(false);
      }
    }
  }
  const selectedMember = members.items.find(
      (v) => v.id === member && v.status === "active",
    ),
    selectedOrganization = organizations.items.find(
      (v) => v.id === organization,
    );
  async function submit(e) {
    e.preventDefault();
    if (
      submitted.current ||
      pending.current ||
      unavailable ||
      !confirmed ||
      !selectedMember ||
      !selectedOrganization ||
      !members.etag ||
      !organizations.etag ||
      location.hash !== "#workspace=" + context
    )
      return;
    submitted.current = true;
    setWriting(true);
    setError(null);
    const generation = ++epoch.current;
    try {
      await request(
        context,
        csrf,
        base + "/organizations/" + organization + "/members/" + member,
        { method: "PUT", body: {}, etag: members.etag },
      );
      if (current(generation)) onChanged();
    } catch (e) {
      if (current(generation)) fail(e);
    } finally {
      submitted.current = false;
      if (current(generation)) setWriting(false);
    }
  }
  const disabled = loading || writing || unavailable;
  return (
    <dialog
      ref={dialog}
      aria-labelledby="organization-assignment-title"
      onCancel={(e) => {
        e.preventDefault();
        close();
      }}
    >
      <header className="dialog-header">
        <h2 id="organization-assignment-title">关联组织成员</h2>
        <button
          type="button"
          className="icon-button"
          aria-label="关闭组织关联"
          disabled={writing}
          onClick={close}
        >
          ×
        </button>
      </header>
      <form onSubmit={submit}>
        <p className="section-note">
          组织归属不会授予角色或管理权限。请选择成员与组织，再确认本次关联。
        </p>
        <label>
          选择成员
          <select
            name="member"
            aria-label="选择成员"
            required
            value={member}
            disabled={disabled}
            onChange={(e) => {
              setMember(e.target.value);
              setConfirmed(false);
            }}
          >
            <option value="" disabled>
              请选择成员
            </option>
            {members.items
              .filter((v) => v.status === "active")
              .map((v) => (
                <option key={v.id} value={v.id}>
                  {v.email || v.user_id}
                </option>
              ))}
          </select>
        </label>
        <div className="actions">
          <span className="muted">
            已加载 {members.items.length} 条成员记录
          </span>
          {members.next && (
            <button
              type="button"
              className="secondary"
              disabled={disabled}
              onClick={() => load("members")}
            >
              加载更多成员
            </button>
          )}
        </div>
        <label>
          选择组织
          <select
            name="org"
            aria-label="选择组织"
            required
            value={organization}
            disabled={disabled}
            onChange={(e) => {
              setOrganization(e.target.value);
              setConfirmed(false);
            }}
          >
            <option value="" disabled>
              请选择组织
            </option>
            {organizations.items.map((v) => (
              <option key={v.id} value={v.id}>
                {v.name} · {v.id.slice(0, 8)}
              </option>
            ))}
          </select>
        </label>
        <div className="actions">
          <span className="muted">
            已加载 {organizations.items.length} 个组织
          </span>
          {organizations.next && (
            <button
              type="button"
              className="secondary"
              disabled={disabled}
              onClick={() => load("organizations")}
            >
              加载更多组织
            </button>
          )}
        </div>
        {loading && <p role="status">正在读取关联选项…</p>}
        {selectedMember && selectedOrganization && (
          <p className="notice">
            将 {selectedMember.email || selectedMember.user_id} 关联到「
            {selectedOrganization.name}」。
          </p>
        )}
        <label className="owner-confirm">
          <input
            type="checkbox"
            required
            checked={confirmed}
            disabled={disabled || !selectedMember || !selectedOrganization}
            onChange={(e) => setConfirmed(e.target.checked)}
          />
          我确认关联所选成员与组织，归属不授予角色
        </label>
        {error && (
          <div className="error" role="alert">
            <p>{errorText(error)}</p>
            {error.code === "recent_mfa_required" && (
              <a href="/login?force=1">重新验证身份</a>
            )}
          </div>
        )}
        {unavailable && (
          <p className="muted">
            本次选择已清除。请关闭并核对实际组织直属成员后再操作，系统不会自动重发关联。
          </p>
        )}
        <div className="dialog-actions">
          <button
            type="button"
            className="secondary"
            disabled={writing}
            onClick={close}
          >
            取消
          </button>
          <button
            disabled={
              disabled || !confirmed || !selectedMember || !selectedOrganization
            }
          >
            {writing ? "正在保存…" : "保存并继续"}
          </button>
        </div>
      </form>
    </dialog>
  );
}
