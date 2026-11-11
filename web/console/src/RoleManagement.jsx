import React, { useEffect, useRef, useState } from "react";
import { errorText } from "./api.js";
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
  initialMember,
  onConsumed,
}) {
  const [modal, setModal] = useState(null),
    [members, setMembers] = useState([]),
    [allRoles, setAllRoles] = useState([]),
    [bound, setBound] = useState([]),
    [loading, setLoading] = useState(false);
  const alive = useRef(true),
    epoch = useRef(0);
  useEffect(
    () => () => {
      alive.current = false;
      epoch.current++;
    },
    [],
  );
  const close = () => {
    epoch.current++;
    setModal(null);
    clearError();
  };
  async function chooseMember(member, previous) {
    const generation = ++epoch.current;
    setLoading(true);
    setModal({ ...previous, member, role: "", operation: "bind" });
    try {
      const response = await read(base + "/members/" + member + "/roles");
      if (alive.current && generation === epoch.current) {
        setBound(response.data.items);
        setModal({
          ...previous,
          member,
          role: previous.role || "",
          operation: "bind",
          etag: response.etag,
        });
      }
    } catch (e) {
      if (alive.current && generation === epoch.current) clearError(e);
    } finally {
      if (alive.current && generation === epoch.current) setLoading(false);
    }
  }
  async function bind(member) {
    clearError();
    const generation = ++epoch.current;
    setLoading(true);
    try {
      const roleResponse = await read(base + "/roles?limit=100");
      const choices = permissions.includes("members.read")
        ? (await read(base + "/members?limit=100")).data.items
        : [{ id: me.membership_id, email: "当前账户", status: "active" }];
      if (!alive.current || generation !== epoch.current) return;
      setAllRoles(roleResponse.data.items);
      setMembers(choices);
      setBound([]);
      const value = {
        kind: "binding",
        member: member?.id || "",
        role: "",
        operation: "bind",
        etag: roleResponse.etag,
      };
      setModal(value);
      if (member?.id) await chooseMember(member.id, value);
    } catch (e) {
      if (alive.current && generation === epoch.current) clearError(e);
    } finally {
      if (alive.current && generation === epoch.current) setLoading(false);
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
    const values = Object.fromEntries(new FormData(event.currentTarget));
    const policies = catalog.flatMap((permission) =>
      values[permission] && values[permission] !== "none"
        ? [{ permission, effect: values[permission] }]
        : [],
    );
    if (modal.kind === "create")
      return write(
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
      return write(
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
      return write(
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
    return write(
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
  }
  const selected = allRoles.find((role) => role.id === modal?.role);
  return (
    <section className="panel">
      <div className="panel-heading">
        <h2>空间角色与权限</h2>
        <div className="actions">
          <button className="secondary" onClick={() => bind(null)}>
            管理成员角色
          </button>
          <button
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
          busy={busy || loading}
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
                    disabled={loading}
                    onChange={(e) => chooseMember(e.target.value, modal)}
                  >
                    <option value="" disabled>
                      请选择成员
                    </option>
                    {members.map((member) => (
                      <option key={member.id} value={member.id}>
                        {member.email || member.user_id || member.id}
                      </option>
                    ))}
                  </select>
                </label>
                <p className="muted">
                  当前角色：{bound.map((role) => role.code).join("、") || "无"}
                </p>
                <label>
                  绑定操作
                  <select
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
                    {(modal.operation === "bind" ? allRoles : bound).map(
                      (role) => (
                        <option key={role.id} value={role.id}>
                          {role.code}
                          {role.built_in ? "（空间所有者）" : ""}
                        </option>
                      ),
                    )}
                  </select>
                </label>
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
                disabled={busy || loading}
              >
                取消
              </button>
              <button
                disabled={
                  busy ||
                  loading ||
                  (modal.kind === "binding" && (!modal.member || !modal.role))
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
