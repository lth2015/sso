import React, { useEffect, useRef, useState } from "react";
import { errorText } from "./api.js";
import {
  choiceLimit,
  emptyChoices,
  choicePage,
  memberBindings,
} from "./role-choices.js";
export const permissionLabels = {
  "members.read": "查看成员与已验证邮箱",
  "members.write": "停用或恢复普通成员",
  "organizations.read": "查看组织",
  "organizations.write": "管理组织及成员归属",
  "invitations.manage": "管理成员邀请",
  "audit.read": "查看审计记录",
};
function Modal({ title, children, busy, onClose }) {
  const ref = useRef(null);
  useEffect(() => {
    ref.current.showModal();
  }, []);
  return (
    <dialog
      ref={ref}
      aria-labelledby="role-dialog-title"
      onCancel={(e) => {
        e.preventDefault();
        if (!busy) onClose();
      }}
    >
      <header className="dialog-header">
        <h2 id="role-dialog-title">{title}</h2>
        <button
          type="button"
          className="icon-button"
          aria-label="关闭角色对话框"
          disabled={busy}
          onClick={onClose}
        >
          ×
        </button>
      </header>
      {children}
    </dialog>
  );
}
export default function RoleManagement({
  roles,
  context,
  etag,
  catalog,
  base,
  me,
  permissions,
  read,
  write,
  busy,
  error,
  clearError,
  onFailure,
  onHistory,
  initialMember,
  onConsumed,
}) {
  const [modal, setModal] = useState(null),
    [memberPage, setMemberPage] = useState(emptyChoices),
    [rolePage, setRolePage] = useState(emptyChoices),
    [bound, setBound] = useState([]),
    [loading, setLoading] = useState(false);
  const alive = useRef(true),
    epoch = useRef(0),
    pending = useRef(null),
    submitted = useRef(false);
  useEffect(() => {
    alive.current = true;
    return () => {
      alive.current = false;
      epoch.current++;
      pending.current?.abort();
    };
  }, []);
  const current = (generation, abort) =>
    alive.current &&
    generation === epoch.current &&
    !abort.signal.aborted &&
    location.hash === "#workspace=" + context;
  const begin = () => {
    pending.current?.abort();
    const abort = new AbortController(),
      generation = ++epoch.current;
    pending.current = abort;
    setLoading(true);
    return { abort, generation };
  };
  const close = () => {
    epoch.current++;
    pending.current?.abort();
    pending.current = null;
    setLoading(false);
    setModal(null);
    setBound([]);
    setMemberPage(emptyChoices());
    setRolePage(emptyChoices());
    clearError();
  };
  const failed = (e) => {
    setBound([]);
    setMemberPage(emptyChoices());
    setRolePage(emptyChoices());
    setModal((old) =>
      old
        ? { ...old, member: "", role: "", etag: null, unavailable: true }
        : old,
    );
    if (e.status === 401) onFailure(e);
    else clearError(e);
  };
  async function chooseMember(member, previous) {
    if (!member || busy || pending.current || submitted.current) return;
    const { abort, generation } = begin();
    clearError();
    setBound([]);
    setModal({ ...previous, member, role: "", operation: "bind", etag: null });
    try {
      const response = await read(base + "/members/" + member + "/roles", {
        signal: abort.signal,
      });
      const items = memberBindings(response, rolePage.etag);
      if (current(generation, abort)) {
        setBound(items);
        setModal({
          ...previous,
          member,
          role: "",
          operation: "bind",
          etag: response.etag,
        });
      }
    } catch (e) {
      if (current(generation, abort)) failed(e);
    } finally {
      if (current(generation, abort)) {
        pending.current = null;
        setLoading(false);
      }
    }
  }
  async function bind(member) {
    if (busy || pending.current || submitted.current) return;
    clearError();
    const { abort, generation } = begin();
    setBound([]);
    setMemberPage(emptyChoices());
    setRolePage(emptyChoices());
    const value = {
      kind: "binding",
      member: "",
      role: "",
      operation: "bind",
      etag: null,
    };
    setModal(value);
    try {
      const roleResponse = await read(base + "/roles?limit=" + choiceLimit, {
        signal: abort.signal,
      });
      const nextRoles = choicePage(roleResponse, "roles");
      let nextMembers = emptyChoices();
      if (permissions.includes("members.read")) {
        const response = await read(base + "/members?limit=" + choiceLimit, {
          signal: abort.signal,
        });
        nextMembers = choicePage(
          response,
          "members",
          emptyChoices(),
          nextRoles.etag,
        );
      } else
        nextMembers = {
          items: [
            { id: me.membership_id, email: "当前账户", status: "active" },
          ],
          next: null,
          etag: nextRoles.etag,
        };
      // A row action can target a member from any main-list page; keep it selectable.
      if (member?.id && !nextMembers.items.some((v) => v.id === member.id))
        nextMembers.items = [member, ...nextMembers.items];
      let nextBound = [],
        nextModal = value;
      if (member?.id) {
        const response = await read(base + "/members/" + member.id + "/roles", {
          signal: abort.signal,
        });
        nextBound = memberBindings(response, nextRoles.etag);
        nextModal = { ...value, member: member.id, etag: response.etag };
      }
      if (!current(generation, abort)) return;
      setRolePage(nextRoles);
      setMemberPage(nextMembers);
      setBound(nextBound);
      setModal(nextModal);
    } catch (e) {
      if (current(generation, abort)) failed(e);
    } finally {
      if (current(generation, abort)) {
        pending.current = null;
        setLoading(false);
      }
    }
  }
  async function more(kind) {
    if (busy || pending.current || submitted.current || modal?.unavailable)
      return;
    const previous = kind === "members" ? memberPage : rolePage;
    if (!previous.next) return;
    clearError();
    const { abort, generation } = begin();
    try {
      const response = await read(
        base + "/" + kind + "?limit=" + choiceLimit + "&after=" + previous.next,
        { signal: abort.signal },
      );
      // Preserve an explicitly opened row without duplicating it on its natural page.
      const pinned =
        kind === "members"
          ? previous.items.find((v) => v.id > previous.next)
          : null;
      const ordered = pinned
        ? {
            ...previous,
            items: previous.items.filter((v) => v.id !== pinned.id),
          }
        : previous;
      const result = choicePage(response, kind, ordered);
      if (pinned && !result.items.some((v) => v.id === pinned.id))
        result.items = [pinned, ...result.items];
      if (current(generation, abort)) {
        if (kind === "members") setMemberPage(result);
        else setRolePage(result);
      }
    } catch (e) {
      if (current(generation, abort)) failed(e);
    } finally {
      if (current(generation, abort)) {
        pending.current = null;
        setLoading(false);
      }
    }
  }
  useEffect(() => {
    if (initialMember) {
      bind(initialMember);
      onConsumed();
    }
  }, [initialMember]);
  async function submit(event) {
    event.preventDefault();
    if (
      busy ||
      pending.current ||
      loading ||
      submitted.current ||
      !modal ||
      location.hash !== "#workspace=" + context
    )
      return;
    if (
      modal.kind === "binding" &&
      (!modal.etag ||
        modal.unavailable ||
        !memberPage.items.some((v) => v.id === modal.member) ||
        !(modal.operation === "bind" ? rolePage.items : bound).some(
          (v) => v.id === modal.role,
        ))
    )
      return;
    submitted.current = true;
    try {
      const values = Object.fromEntries(new FormData(event.currentTarget));
      const policies = catalog.flatMap((permission) =>
        values[permission] && values[permission] !== "none"
          ? [{ permission, effect: values[permission] }]
          : [],
      );
      if (modal.kind === "create")
        return await write(
          base + "/roles",
          { code: values.code, policies },
          {
            method: "POST",
            etag,
            invalidates: true,
            message: "角色已创建，请重新确认空间授权。",
          },
        );
      if (modal.kind === "edit")
        return await write(
          base + "/roles/" + modal.role.id,
          { policies },
          {
            method: "PUT",
            etag,
            invalidates: true,
            message: "角色规则已更新，请重新确认空间授权。",
          },
        );
      if (modal.kind === "delete")
        return await write(
          base + "/roles/" + modal.role.id,
          {},
          {
            method: "DELETE",
            etag,
            invalidates: true,
            message: "角色已删除，请重新确认空间授权。",
          },
        );
      const target = base + "/members/" + modal.member + "/roles/" + modal.role;
      return await write(
        target,
        {},
        {
          method: modal.operation === "bind" ? "PUT" : "DELETE",
          etag: modal.etag,
          invalidates: true,
          message:
            modal.operation === "bind"
              ? "角色已授予，请重新确认空间授权。"
              : "角色绑定已移除，请重新确认空间授权。",
        },
      );
    } finally {
      submitted.current = false;
    }
  }
  const selected = (
    modal?.operation === "unbind" ? bound : rolePage.items
  ).find((role) => role.id === modal?.role);
  return (
    <section className="panel">
      <div className="panel-heading">
        <h2>空间角色与权限</h2>
        <div className="actions">
          <button
            className="secondary"
            disabled={busy || loading}
            onClick={() => bind(null)}
          >
            管理成员角色
          </button>
          <button
            disabled={busy || loading}
            onClick={() => {
              clearError();
              setModal({ kind: "create" });
            }}
          >
            创建角色
          </button>
        </div>
      </div>
      <p className="section-note">
        角色规则作用于当前租户；多角色合并时显式拒绝优先。角色与所有者管理仅限空间所有者，不能通过自定义角色委托。
      </p>
      <div className="table-scroll">
        <table>
          <thead>
            <tr>
              <th>角色</th>
              <th>规则</th>
              <th>绑定数</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            {roles.map((role) => (
              <tr key={role.id}>
                <td>
                  <strong>{role.code}</strong>
                  {role.built_in && <span className="pill">内置所有者</span>}
                </td>
                <td>
                  {role.built_in
                    ? "所有者保留角色管理；其他能力受显式拒绝约束。"
                    : role.policies.length
                      ? role.policies.map((p) => (
                          <div
                            key={p.permission + p.effect}
                            className={p.effect === "deny" ? "denied-rule" : ""}
                          >
                            {p.effect === "deny" ? "拒绝" : "允许"} ·{" "}
                            {permissionLabels[p.permission]}
                          </div>
                        ))
                      : "未配置授权"}
                </td>
                <td>{role.binding_count}</td>
                <td>
                  {!role.built_in && (
                    <div className="actions">
                      <button
                        className="text-button"
                        disabled={busy || loading}
                        onClick={() => onHistory(role.id)}
                      >
                        查看策略历史
                      </button>
                      <button
                        className="text-button"
                        onClick={() => {
                          clearError();
                          setModal({ kind: "edit", role });
                        }}
                      >
                        编辑规则
                      </button>
                      <button
                        className="text-button"
                        disabled={role.binding_count > 0}
                        title={role.binding_count ? "请先移除所有成员绑定" : ""}
                        onClick={() => {
                          clearError();
                          setModal({ kind: "delete", role });
                        }}
                      >
                        删除角色
                      </button>
                    </div>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      {loading && !modal && <p role="status">正在读取成员角色…</p>}
      {modal && (
        <Modal
          title={
            {
              create: "创建角色",
              edit: "编辑角色规则",
              delete: "确认删除角色",
              binding: "管理成员角色",
            }[modal.kind]
          }
          busy={busy}
          onClose={close}
        >
          <form onSubmit={submit}>
            {modal.kind === "create" && (
              <label>
                角色标识
                <input
                  name="code"
                  required
                  pattern={"[a-z][a-z0-9_\\-]{0,62}"}
                  maxLength="63"
                  autoFocus
                  placeholder="例如 auditor"
                />
              </label>
            )}
            {["create", "edit"].includes(modal.kind) && (
              <>
                <p className="muted">
                  未配置的能力默认不授予。拒绝会覆盖其他角色的允许规则，包括绑定此角色的所有者。
                </p>
                {catalog.map((permission) => (
                  <label key={permission}>
                    {permissionLabels[permission]}
                    <select
                      aria-label={permissionLabels[permission]}
                      name={permission}
                      defaultValue={
                        modal.role?.policies.some(
                          (p) =>
                            p.permission === permission && p.effect === "deny",
                        )
                          ? "deny"
                          : modal.role?.policies.some(
                                (p) =>
                                  p.permission === permission &&
                                  p.effect === "allow",
                              )
                            ? "allow"
                            : "none"
                      }
                    >
                      <option value="none">未配置</option>
                      <option value="allow">允许</option>
                      <option value="deny">拒绝</option>
                    </select>
                  </label>
                ))}
              </>
            )}
            {modal.kind === "delete" && (
              <p>
                将删除角色「{modal.role.code}
                」。内置所有者及仍有成员绑定的角色不能删除。
              </p>
            )}
            {modal.kind === "binding" && (
              <>
                <label>
                  目标成员
                  <select
                    aria-label="目标成员"
                    value={modal.member}
                    required
                    disabled={busy || loading || modal.unavailable}
                    onChange={(e) => chooseMember(e.target.value, modal)}
                  >
                    <option value="" disabled>
                      请选择成员
                    </option>
                    {memberPage.items.map((member) => (
                      <option key={member.id} value={member.id}>
                        {member.email || member.user_id || member.id}
                      </option>
                    ))}
                  </select>
                </label>
                {permissions.includes("members.read") && (
                  <div className="actions">
                    <span className="muted">
                      已加载 {memberPage.items.length} 位成员
                    </span>
                    {memberPage.next && (
                      <button
                        type="button"
                        className="secondary"
                        disabled={busy || loading}
                        onClick={() => more("members")}
                      >
                        加载更多成员
                      </button>
                    )}
                  </div>
                )}
                <p className="muted">
                  当前角色：{bound.map((role) => role.code).join("、") || "无"}
                </p>
                <label>
                  绑定操作
                  <select
                    disabled={busy || loading || modal.unavailable}
                    aria-label="绑定操作"
                    value={modal.operation}
                    onChange={(e) => {
                      clearError();
                      setModal({
                        ...modal,
                        operation: e.target.value,
                        role: "",
                      });
                    }}
                  >
                    <option value="bind">授予角色</option>
                    <option value="unbind">移除角色</option>
                  </select>
                </label>
                <label>
                  选择角色
                  <select
                    disabled={busy || loading || modal.unavailable}
                    aria-label="选择角色"
                    value={modal.role}
                    required
                    onChange={(e) =>
                      setModal({ ...modal, role: e.target.value })
                    }
                  >
                    <option value="" disabled>
                      请选择角色
                    </option>
                    {(modal.operation === "bind" ? rolePage.items : bound).map(
                      (role) => (
                        <option key={role.id} value={role.id}>
                          {role.code}
                          {role.built_in ? "（空间所有者）" : ""}
                        </option>
                      ),
                    )}
                  </select>
                </label>
                {modal.operation === "bind" && (
                  <div className="actions">
                    <span className="muted">
                      已加载 {rolePage.items.length} 个角色（每空间最多 100 个）
                    </span>
                    {rolePage.next && (
                      <button
                        type="button"
                        className="secondary"
                        disabled={busy || loading}
                        onClick={() => more("roles")}
                      >
                        加载更多角色
                      </button>
                    )}
                  </div>
                )}
                {loading && <p role="status">正在读取成员角色…</p>}
                {modal.unavailable && (
                  <p className="muted">本次选择已清除，请关闭后重新读取。</p>
                )}
                {selected?.built_in && (
                  <label className="owner-confirm">
                    <input type="checkbox" required />
                    我确认
                    {modal.operation === "bind"
                      ? "授予此成员空间所有者权限"
                      : "移除此成员的空间所有者权限"}
                    ，并理解这会改变角色管理资格。
                  </label>
                )}
                <p className="muted">
                  变更后旧空间授权失效；成员需要重新授权。成员或组织归属不会自动授予角色。
                </p>
              </>
            )}
            {error && (
              <div className="error" role="alert">
                <p>{errorText(error)}</p>
                {error.code === "recent_mfa_required" && (
                  <a href="/login?force=1">重新验证身份</a>
                )}
              </div>
            )}
            <div className="dialog-actions">
              <button
                className="secondary"
                type="button"
                onClick={close}
                disabled={busy}
              >
                取消
              </button>
              <button
                disabled={
                  busy ||
                  loading ||
                  (modal.kind === "binding" &&
                    (!modal.member ||
                      !modal.role ||
                      !modal.etag ||
                      modal.unavailable))
                }
              >
                {busy ? "正在保存…" : "保存角色变更"}
              </button>
            </div>
          </form>
        </Modal>
      )}
    </section>
  );
}
