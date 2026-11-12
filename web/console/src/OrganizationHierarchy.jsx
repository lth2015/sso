import React, { useEffect, useRef, useState } from "react";
import { request, errorText } from "./api.js";
import {
  choiceLimit,
  emptyChoices,
  parentChoices,
  moveResult,
  createdOrganization,
  organizationName,
} from "./organization-hierarchy.js";

export default function OrganizationHierarchy({
  context,
  csrf,
  tenant,
  root,
  action,
  onClose,
  onFailure,
  onChanged,
}) {
  const moving = action.kind === "move";
  const [choices, setChoices] = useState(emptyChoices),
    [parent, setParent] = useState(""),
    [name, setName] = useState(""),
    [plan, setPlan] = useState(null),
    [confirmed, setConfirmed] = useState(false),
    [loading, setLoading] = useState(true),
    [previewing, setPreviewing] = useState(false),
    [writing, setWriting] = useState(false),
    [unavailable, setUnavailable] = useState(false),
    [error, setError] = useState(null);
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
  function clearPlan() {
    setPlan(null);
    setConfirmed(false);
  }
  function fail(e) {
    clearPlan();
    setParent("");
    setChoices(emptyChoices());
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
  async function load(more = false) {
    if (pending.current || submitted.current || unavailable) return;
    const abort = new AbortController(),
      generation = ++epoch.current;
    pending.current = abort;
    setLoading(true);
    setError(null);
    clearPlan();
    try {
      const previous = more ? choices : emptyChoices();
      const r = await request(
        context,
        csrf,
        base +
          "/organizations?limit=" +
          choiceLimit +
          (previous.next ? "&after=" + previous.next : ""),
        { signal: abort.signal },
      );
      if (!current(generation, abort)) return;
      setChoices(
        parentChoices(
          r,
          root,
          previous,
          moving ? action.etag || null : undefined,
        ),
      );
      if (!more) setParent("");
    } catch (e) {
      if (current(generation, abort)) fail(e);
    } finally {
      if (current(generation, abort)) {
        pending.current = null;
        setLoading(false);
      }
    }
  }
  const selected = choices.items.find(
    (v) =>
      v.id === parent &&
      (!moving || (v.id !== action.row.id && v.id !== action.row.parent_id)),
  );
  async function preview() {
    if (pending.current || submitted.current || unavailable || !selected)
      return;
    const abort = new AbortController(),
      generation = ++epoch.current;
    pending.current = abort;
    setPreviewing(true);
    setError(null);
    clearPlan();
    try {
      const r = await request(
        context,
        csrf,
        base + "/organizations/" + action.row.id + "/move",
        {
          method: "POST",
          body: { parent_id: parent, dry_run: true },
          etag: choices.etag,
          signal: abort.signal,
        },
      );
      if (!current(generation, abort)) return;
      setPlan({
        ...moveResult(r, choices.etag, true),
        parent,
        etag: choices.etag,
      });
    } catch (e) {
      if (current(generation, abort)) fail(e);
    } finally {
      if (current(generation, abort)) {
        pending.current = null;
        setPreviewing(false);
      }
    }
  }
  const validPlan =
    plan && plan.parent === parent && plan.etag === choices.etag;
  async function submit(e) {
    e.preventDefault();
    if (
      pending.current ||
      submitted.current ||
      unavailable ||
      !selected ||
      !choices.etag ||
      location.hash !== "#workspace=" + context ||
      (moving ? !validPlan || !confirmed : !organizationName(name))
    )
      return;
    submitted.current = true;
    setWriting(true);
    setError(null);
    const generation = ++epoch.current;
    try {
      const r = await request(
        context,
        csrf,
        base + "/organizations" + (moving ? "/" + action.row.id + "/move" : ""),
        {
          method: "POST",
          body: moving
            ? { parent_id: parent, dry_run: false }
            : { parent_id: parent, name: name.trim() },
          etag: choices.etag,
        },
      );
      if (!current(generation)) return;
      if (moving) moveResult(r, choices.etag, false);
      else createdOrganization(r, choices.etag);
      onChanged(moving);
    } catch (e) {
      if (current(generation)) fail(e);
    } finally {
      submitted.current = false;
      if (current(generation)) setWriting(false);
    }
  }
  const disabled = loading || previewing || writing || unavailable;
  return (
    <dialog
      ref={dialog}
      aria-labelledby="organization-hierarchy-title"
      onCancel={(e) => {
        e.preventDefault();
        close();
      }}
    >
      <header className="dialog-header">
        <h2 id="organization-hierarchy-title">
          {moving ? "移动组织" : "创建组织"}
        </h2>
        <button
          type="button"
          className="icon-button"
          aria-label="关闭组织编辑"
          disabled={writing}
          onClick={close}
        >
          ×
        </button>
      </header>
      <form onSubmit={submit}>
        {moving ? (
          <p>
            将「{action.row.name}
            」及其下级组织移到新的上级。不能移入自身下级；请选择上级并预览。
          </p>
        ) : (
          <label>
            组织名称
            <input
              name="name"
              aria-label="组织名称"
              aria-describedby="organization-name-help"
              required
              maxLength="128"
              autoFocus
              value={name}
              disabled={disabled}
              onChange={(e) => setName(e.target.value)}
            />
            <small id="organization-name-help">
              名称最多 128 个 UTF-8 字节。
            </small>
          </label>
        )}
        <label>
          上级组织
          <select
            name="parent"
            aria-label="上级组织"
            required
            value={parent}
            disabled={disabled}
            onChange={(e) => {
              setParent(e.target.value);
              clearPlan();
            }}
          >
            <option value="" disabled>
              请选择上级组织
            </option>
            {choices.items
              .filter(
                (v) =>
                  !moving ||
                  (v.id !== action.row.id && v.id !== action.row.parent_id),
              )
              .map((v) => (
                <option key={v.id} value={v.id}>
                  {v.name} · {v.id.slice(0, 8)}
                </option>
              ))}
          </select>
        </label>
        <div className="actions">
          <span className="muted">已加载 {choices.items.length} 个组织</span>
          {choices.next && (
            <button
              type="button"
              className="secondary"
              disabled={disabled}
              onClick={() => load(true)}
            >
              加载更多组织
            </button>
          )}
          <button
            type="button"
            className="secondary"
            disabled={disabled}
            onClick={() => load()}
          >
            重新读取上级
          </button>
        </div>
        {(loading || previewing) && (
          <p role="status">
            {previewing ? "正在预览移动…" : "正在读取上级组织…"}
          </p>
        )}
        {selected && <p className="notice">所选上级：「{selected.name}」。</p>}
        {moving && validPlan && (
          <p className="notice" role="status">
            将移动 {plan.nodes} 个组织，移动后最深层级为 {plan.depth}。
          </p>
        )}
        {moving && (
          <label className="owner-confirm">
            <input
              type="checkbox"
              checked={confirmed}
              disabled={disabled || !validPlan}
              onChange={(e) => setConfirmed(e.target.checked)}
            />
            我确认按当前预览移动组织及其下级
          </label>
        )}
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
            本次选择和预览已清除。请关闭并刷新组织目录、核对实际结构和审计后再操作，系统不会自动重发。
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
          {moving && (
            <button
              type="button"
              className="secondary"
              disabled={disabled || !selected}
              onClick={preview}
            >
              预览移动
            </button>
          )}
          <button
            disabled={
              disabled ||
              !selected ||
              (moving ? !validPlan || !confirmed : !organizationName(name))
            }
          >
            {writing ? "正在保存…" : moving ? "确认移动" : "保存并继续"}
          </button>
        </div>
      </form>
    </dialog>
  );
}
