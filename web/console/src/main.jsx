import React, { useEffect, useRef, useState } from "react";
import { createRoot } from "react-dom/client";
import { request, errorText, ApiError } from "./api.js";
import "./style.css";
import RoleManagement from "./RoleManagement.jsx";
import Playground from "./Playground.jsx";
import AccountSecurity from "./AccountSecurity.jsx";
import ApplicationManagement from "./ApplicationManagement.jsx";
import OrganizationActions from "./OrganizationActions.jsx";
import OrganizationAssignment from "./OrganizationAssignment.jsx";
import RolePolicyHistory from "./RolePolicyHistory.jsx";
import MachineEvents from "./MachineEvents.jsx";
import AuditExport from "./AuditExport.jsx";
import InvitationReissue from "./InvitationReissue.jsx";

const tabs = [
  ["overview", "空间概览", "◈"],
  ["members", "成员", "♙"],
  ["organizations", "组织", "▦"],
  ["invitations", "邀请", "↗"],
  ["audit", "审计", "≡"],
  ["roles", "角色与权限", "⚿"],
  ["applications", "应用接入", "↔"],
  ["sessions", "安全会话", "◉"],
  ["playground", "API Playground", "⌘"],
];
const tabPermissions = {
  members: "members.read",
  organizations: "organizations.read",
  invitations: "invitations.manage",
  audit: "audit.read",
  roles: "roles.manage",
  applications: "roles.manage",
};
const contextFromURL = () =>
  /^#workspace=([a-f0-9]{32})$/.exec(location.hash)?.[1] || "";
const short = (id) => (id ? id.slice(0, 8) + "…" : "—");
const date = (value) =>
  new Date(typeof value === "number" ? value * 1000 : value).toLocaleString(
    "zh-CN",
  );
const actionNames = {
  "role.created": "创建角色",
  "application.enabled": "恢复空间应用",
  "application.created": "创建应用",
  "service_principal.created": "创建机器身份",
  "service_principal.disabled": "停用机器身份",
  "service_principal.enabled": "恢复机器身份",
  "credential.pending": "登记待处理机器凭证",
  "credential.activated": "激活机器凭证",
  "credential.revoked": "撤销机器凭证",
  "credential.overlap_shortened": "缩短旧凭证重叠期",
  "credential.protocol_cleanup_checked": "核对机器凭证协议清理",
  "client.registration_pending": "登记公开客户端",
  "client.activated": "客户端登记生效",
  "client.revoked": "撤销客户端",
  "client.protocol_cleanup_checked": "核对协议清理",
  "application.disabled": "停用空间应用",
  "role.updated": "更新角色规则",
  "role.deleted": "删除角色",
  "role.bound": "授予角色",
  "role.unbound": "移除角色",
  "tenant.created": "创建空间",
  "org.created": "创建组织",
  "org.moved": "移动组织",
  "member.invited": "邀请成员",
  "member.joined": "成员加入",
  "member.suspended": "停用成员",
  "member.restored": "恢复成员",
  "org.renamed": "重命名组织",
  "org.deleted": "删除空组织",
  "org.member_unassigned": "移除组织归属",
  "org.member_assigned": "关联组织成员",
  "invitation.revoked": "撤销邀请",
  "invitation.reissued": "更新邀请凭证",
  "authorization.consented": "确认访问授权",
  "authorization.logout": "退出应用",
  "token.authorization_code": "完成访问授权",
  "token.refresh_token": "续期访问授权",
};
function ID({ value }) {
  return <code title={value}>{short(value)}</code>;
}
function Empty({ children }) {
  return (
    <div className="empty">
      <span>◇</span>
      <p>{children}</p>
    </div>
  );
}
function Dialog({ title, children, busy, onClose }) {
  const ref = useRef(null);
  useEffect(() => {
    ref.current.showModal();
  }, []);
  return (
    <dialog
      ref={ref}
      onCancel={(event) => {
        event.preventDefault();
        if (!busy) onClose();
      }}
      aria-labelledby="dialog-title"
    >
      <header className="dialog-header">
        <h2 id="dialog-title">{title}</h2>
        <button
          type="button"
          className="icon-button"
          aria-label="关闭对话框"
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
function App() {
  const [context, setContext] = useState(contextFromURL),
    [tab, setTab] = useState("overview");
  const [permissions, setPermissions] = useState(null),
    [roleMember, setRoleMember] = useState(null);
  const [orgAction, setOrgAction] = useState(null);
  const [assignmentOpen, setAssignmentOpen] = useState(false);
  const [policyHistoryRole, setPolicyHistoryRole] = useState(null);
  const [invitationAction, setInvitationAction] = useState(null);
  const can = (permission) =>
    permissions?.permissions?.includes(permission) || false;
  const [session, setSession] = useState(null),
    [me, setMe] = useState(null),
    [tenant, setTenant] = useState(null),
    [data, setData] = useState(null);
  const [error, setError] = useState(null),
    [notice, setNotice] = useState(""),
    [authNeeded, setAuthNeeded] = useState(false),
    [loading, setLoading] = useState(false);
  const [modal, setModal] = useState(null),
    [secret, setSecret] = useState(null),
    [busy, setBusy] = useState(false),
    [revision, setRevision] = useState(0);
  const epoch = useRef(0),
    current = useRef(context),
    activeTab = useRef(tab),
    csrf = useRef("");
  current.current = context;
  activeTab.current = tab;
  const base = me ? "/v1/tenants/" + me.tenant_id : "";
  const close = () => {
    setModal(null);
    setSecret(null);
    setError(null);
  };
  function fail(error) {
    if (error.name === "AbortError") return;
    if (error.status === 401) {
      setAuthNeeded(true);
      setData(null);
      setSecret(null);
      setModal(null);
    } else setError(error);
  }
  useEffect(() => {
    const change = () => setContext(contextFromURL());
    window.addEventListener("hashchange", change);
    return () => window.removeEventListener("hashchange", change);
  }, []);
  useEffect(() => {
    epoch.current++;
    const generation = epoch.current,
      abort = new AbortController();
    csrf.current = "";
    setSession(null);
    setMe(null);
    setTenant(null);
    setPermissions(null);
    setRoleMember(null);
    setOrgAction(null);
    setAssignmentOpen(false);
    setPolicyHistoryRole(null);
    setInvitationAction(null);
    setData(null);
    setError(null);
    setAuthNeeded(false);
    setModal(null);
    setSecret(null);
    setTab("overview");
    if (!context) return () => abort.abort();
    setLoading(true);
    (async () => {
      const value = (
        await request(context, "", "/session", { signal: abort.signal })
      ).data;
      const identity = (
        await request(context, value.csrf, "/bff/v1/me", {
          signal: abort.signal,
        })
      ).data;
      const detail = (
        await request(
          context,
          value.csrf,
          "/bff/v1/tenants/" + identity.tenant_id,
          { signal: abort.signal },
        )
      ).data;
      const effective = (
        await request(
          context,
          value.csrf,
          "/bff/v1/tenants/" + identity.tenant_id + "/permissions",
          { signal: abort.signal },
        )
      ).data;
      if (
        generation !== epoch.current ||
        current.current !== context ||
        abort.signal.aborted
      )
        return;
      csrf.current = value.csrf;
      setSession(value);
      setMe(identity);
      setTenant(detail);
      setPermissions(effective);
    })()
      .catch((error) => {
        if (
          generation === epoch.current &&
          current.current === context &&
          !abort.signal.aborted
        )
          fail(error);
      })
      .finally(() => {
        if (
          generation === epoch.current &&
          current.current === context &&
          !abort.signal.aborted
        )
          setLoading(false);
      });
    return () => abort.abort();
  }, [context]);
  function endpoint() {
    if (tab === "overview") return "/v1/tenants";
    if (tab === "sessions") return "/v1/sessions";
    return base + "/" + (tab === "audit" ? "audit-events" : tab);
  }
  async function loadList(signal, append = false, cursor = null) {
    const generation = epoch.current,
      target = tab,
      ctx = context;
    setLoading(true);
    try {
      const response = await request(
        ctx,
        csrf.current,
        "/bff" + endpoint() + "?limit=50" + (cursor ? "&after=" + cursor : ""),
        { signal },
      );
      if (
        generation !== epoch.current ||
        signal?.aborted ||
        current.current !== ctx ||
        activeTab.current !== target
      )
        return;
      setData((old) => ({
        tab: target,
        context: ctx,
        items:
          append && old?.tab === target && old?.context === ctx
            ? [...old.items, ...response.data.items]
            : response.data.items,
        next: response.data.next_cursor,
        etag: response.etag,
      }));
    } catch (error) {
      if (
        !signal?.aborted &&
        generation === epoch.current &&
        current.current === ctx &&
        activeTab.current === target
      )
        fail(error);
    } finally {
      if (
        !signal?.aborted &&
        generation === epoch.current &&
        current.current === ctx &&
        activeTab.current === target
      )
        setLoading(false);
    }
  }
  useEffect(() => {
    if (!me || !tenant || authNeeded) return;
    if (tab === "playground" || tab === "applications") {
      setLoading(false);
      return;
    }
    const abort = new AbortController();
    setError(null);
    loadList(abort.signal);
    return () => abort.abort();
  }, [tab, me, tenant, revision, authNeeded]);
  const visible = data?.tab === tab && data?.context === context ? data : null;
  async function mutate(
    path,
    body,
    {
      etag,
      method,
      invalidates = false,
      message = "操作已完成。",
      onSuccess,
    } = {},
  ) {
    const generation = epoch.current;
    setBusy(true);
    setError(null);
    try {
      const response = await request(context, csrf.current, "/bff" + path, {
        method:
          method ||
          (path.includes("/organizations/") && path.includes("/members/")
            ? "PUT"
            : "POST"),
        body,
        etag,
      });
      if (generation !== epoch.current || current.current !== context) return;
      setModal(null);
      setSecret(null);
      setNotice(message);
      if (onSuccess) onSuccess(response.data);
      if (invalidates) {
        setAuthNeeded(true);
        setData(null);
      } else setRevision((x) => x + 1);
    } catch (error) {
      if (generation === epoch.current && current.current === context)
        fail(error);
    } finally {
      if (generation === epoch.current && current.current === context)
        setBusy(false);
    }
  }
  function navigate(target) {
    setOrgAction(null);
    setAssignmentOpen(false);
    setPolicyHistoryRole(null);
    setInvitationAction(null);
    setTab(target);
    setError(null);
    setNotice("");
    setModal(null);
    setSecret(null);
  }
  const open = (value) => {
    setError(null);
    setModal(value);
  };
  function fields(event) {
    event.preventDefault();
    return Object.fromEntries(new FormData(event.currentTarget));
  }
  async function submit(event) {
    const v = fields(event);
    if (modal.kind === "new-tenant")
      return mutate(
        "/v1/tenants",
        { slug: v.slug, name: v.name },
        { message: "空间已创建。选择该空间后即可进入。" },
      );
    if (modal.kind === "new-org")
      return mutate(
        base + "/organizations",
        { parent_id: v.parent, name: v.name },
        { etag: visible.etag, message: "组织已创建。" },
      );
    if (modal.kind === "invite")
      return mutate(
        base + "/invitations",
        { email: v.email },
        {
          message: "邀请已创建。请安全转交一次性凭证。",
          onSuccess: (data) => setSecret(data.token),
        },
      );
    if (modal.kind === "accept")
      return mutate(
        "/v1/invitations/accept",
        { token: v.token },
        { message: "已加入空间。重新选择空间后即可进入。" },
      );
    if (modal.kind === "move") {
      const dry = event.nativeEvent.submitter?.value === "preview";
      return mutate(
        base + "/organizations/" + modal.row.id + "/move",
        { parent_id: v.parent, dry_run: dry },
        {
          etag: visible.etag,
          invalidates: !dry,
          message: dry ? "移动预览完成。" : "组织已移动，请重新确认空间授权。",
          onSuccess: dry
            ? (result) => setModal({ ...modal, plan: result, parent: v.parent })
            : undefined,
        },
      );
    }
    if (modal.kind === "member")
      return mutate(
        base +
          "/members/" +
          modal.row.id +
          "/" +
          (modal.row.status === "active" ? "suspend" : "restore"),
        {},
        {
          etag: visible.etag,
          invalidates: true,
          message: "成员状态已更新，请重新确认空间授权。",
        },
      );
    if (modal.kind === "revoke-invite")
      return mutate(
        base + "/invitations/" + modal.row.id + "/revoke",
        {},
        { message: "邀请已撤销。" },
      );
    if (modal.kind === "revoke-others")
      return mutate(
        "/v1/sessions/revoke-others",
        {},
        { message: "其他现有会话已撤销，当前统一登录保持有效。" },
      );
    if (modal.kind === "revoke-session")
      return mutate(
        "/v1/sessions/" + modal.row.id + "/revoke",
        {},
        {
          invalidates: modal.row.current,
          message: modal.row.current
            ? "本次统一登录已撤销。"
            : "所选会话已撤销。",
        },
      );
  }
  async function localLogout() {
    const generation = epoch.current;
    setBusy(true);
    try {
      await request(context, csrf.current, "/session/logout", {
        method: "POST",
        body: {},
      });
      if (generation !== epoch.current || current.current !== context) return;
      location.hash = "";
      setContext("");
      setNotice("已退出此标签页的控制台。");
    } catch (error) {
      fail(error);
    } finally {
      setBusy(false);
    }
  }
  if (!context)
    return (
      <main className="welcome">
        <div className="welcome-brand">◈ Identity</div>
        <div className="welcome-card">
          <p className="eyebrow">身份与空间管理</p>
          <h1>安全地组织你的团队</h1>
          <p>统一管理空间、成员、组织与访问会话。</p>
          <a className="button" href="/login">
            登录管理控制台
          </a>
          <a className="text-link" href="http://auth.localhost:8080/account">
            创建账户或找回密码
          </a>
        </div>
        <p className="welcome-note">
          本地开发环境 · 身份验证在统一认证页面完成
        </p>
      </main>
    );
  const rows = visible?.items || [];
  return (
    <div className="shell">
      <a
        className="skip-link"
        href="#main-content"
        onClick={(event) => {
          event.preventDefault();
          document.getElementById("main-content")?.focus();
        }}
      >
        跳到主要内容
      </a>
      <aside className="sidebar">
        <a href="/" className="brand">
          ◈ Identity
        </a>
        <div className="sidebar-caption">工作空间</div>
        <div className="space-label">
          {tenant?.name || "正在载入…"}
          <span>{tenant?.owner ? "空间所有者" : "空间成员"}</span>
        </div>
        <a className="switch-link" href="/login">
          ⇄ 切换空间
        </a>
        <nav aria-label="主导航">
          {tabs
            .filter(([id]) => !tabPermissions[id] || can(tabPermissions[id]))
            .map(([id, label, icon]) => (
              <button
                key={id}
                aria-current={tab === id ? "page" : undefined}
                onClick={() => navigate(id)}
              >
                <span aria-hidden="true">{icon}</span>
                {label}
              </button>
            ))}
        </nav>
        <div className="sidebar-bottom">
          <a
            href="http://api.localhost:8083/docs"
            target="_blank"
            rel="noreferrer"
          >
            API 接入文档 ↗
          </a>
          <span>本地开发环境</span>
        </div>
      </aside>
      <div className="workspace">
        <header className="topbar">
          <span>
            {tenant?.name || "身份管理控制台"}
            <small className="dev-badge">本地开发</small>
          </span>
          <div className="topbar-account">
            <span>
              账户 <ID value={me?.user_id} />
            </span>
            <button
              className="text-button"
              disabled={busy || authNeeded}
              onClick={localLogout}
            >
              退出此标签页
            </button>
          </div>
        </header>
        <main id="main-content" tabIndex="-1">
          <div className="page-heading">
            <div>
              <p className="eyebrow">IDENTITY CONSOLE</p>
              <h1>{tabs.find(([id]) => id === tab)?.[1]}</h1>
              <p className="muted">
                {tab === "overview"
                  ? "查看你的空间，并选择要管理的团队。"
                  : tab === "playground"
                    ? "依据 OpenAPI 选择接口，在当前空间内发起真实请求。"
                    : tab === "sessions"
                      ? "查看并撤销自己的登录会话。"
                      : "仅在当前空间内管理，操作将记录到审计日志。"}
              </p>
            </div>
            {!authNeeded && me && (
              <button
                className="secondary"
                onClick={() => {
                  setError(null);
                  setRevision((x) => x + 1);
                }}
              >
                刷新资料
              </button>
            )}
          </div>
          {notice && (
            <p className="notice" role="status">
              {notice}
            </p>
          )}
          {error && !modal && <ErrorNotice error={error} />}
          {authNeeded ? (
            <section className="reauth panel">
              <div className="reauth-icon">◉</div>
              <h2>请重新确认访问权限</h2>
              <p>空间权限或会话已更新。重新授权后才能继续查看和操作。</p>
              <a className="button" href="/login">
                重新确认空间授权
              </a>
            </section>
          ) : !me ? (
            <section className="panel">
              <p role="status">
                {loading
                  ? "正在载入身份与空间…"
                  : "资料暂时不可用，请重新登录。"}
              </p>
            </section>
          ) : (
            <>
              {tab === "overview" && (
                <>
                  <section className="summary-grid">
                    <article className="summary-card">
                      <p>当前空间</p>
                      <h2>{tenant.name}</h2>
                      <span className="pill">
                        {tenant.owner ? "所有者" : "成员"}
                      </span>
                    </article>
                    <article className="summary-card">
                      <p>本次访问</p>
                      <h2>统一身份验证</h2>
                      <span className="muted">密码与双重验证已完成</span>
                    </article>
                    <article className="summary-card">
                      <p>访问隔离</p>
                      <h2>独立标签页</h2>
                      <span className="muted">切换空间将建立新的授权</span>
                    </article>
                  </section>
                  <section className="panel">
                    <div className="panel-heading">
                      <h2>我的空间</h2>
                      <div className="actions">
                        <button
                          className="secondary"
                          onClick={() => open({ kind: "accept" })}
                        >
                          接受邀请
                        </button>
                        <button onClick={() => open({ kind: "new-tenant" })}>
                          创建空间
                        </button>
                      </div>
                    </div>
                    <div className="space-grid">
                      {rows.map((row) => (
                        <article className="space-card" key={row.id}>
                          <div className="space-icon">▦</div>
                          <h3>{row.name}</h3>
                          <p>{row.owner ? "空间所有者" : "空间成员"}</p>
                          {!/^personal-|^demo-/.test(row.slug) && (
                            <p className="space-slug">{row.slug}</p>
                          )}
                          {row.id === me.tenant_id ? (
                            <span className="pill">当前空间</span>
                          ) : (
                            <a href="/login">选择空间并进入 ↗</a>
                          )}
                        </article>
                      ))}
                    </div>
                  </section>
                </>
              )}
              {tab === "members" && (
                <section className="panel">
                  <div className="panel-heading">
                    <h2>空间成员</h2>
                    {can("invitations.manage") && (
                      <button onClick={() => open({ kind: "invite" })}>
                        邀请成员
                      </button>
                    )}
                  </div>
                  <Table
                    headers={["账户", "角色", "状态", "操作"]}
                    rows={rows}
                    render={(row) => (
                      <>
                        <td>{row.email || <ID value={row.user_id} />}</td>
                        <td>{row.owner ? "所有者" : "成员"}</td>
                        <td>
                          <span
                            className={
                              "pill " +
                              (row.status === "active" ? "" : "neutral")
                            }
                          >
                            {row.status === "active"
                              ? "有效"
                              : row.status === "suspended"
                                ? "已停用"
                                : row.status}
                          </span>
                        </td>
                        <td>
                          {can("members.write") &&
                            (!row.owner || tenant.owner) && (
                              <button
                                className="text-button"
                                onClick={() => open({ kind: "member", row })}
                              >
                                {row.status === "active"
                                  ? "停用成员"
                                  : "恢复成员"}
                              </button>
                            )}
                          {can("roles.manage") && (
                            <button
                              className="text-button"
                              onClick={() => {
                                setRoleMember(row);
                                navigate("roles");
                              }}
                            >
                              管理角色
                            </button>
                          )}
                        </td>
                      </>
                    )}
                  />
                </section>
              )}
              {tab === "organizations" && (
                <section className="panel">
                  <div className="panel-heading">
                    <h2>组织结构</h2>
                    <div className="actions">
                      {can("organizations.write") && can("members.read") && (
                        <button
                          className="secondary"
                          disabled={busy}
                          onClick={() => {
                            setError(null);
                            setModal(null);
                            setAssignmentOpen(true);
                          }}
                        >
                          关联成员
                        </button>
                      )}
                      {can("organizations.write") && (
                        <button onClick={() => open({ kind: "new-org" })}>
                          创建组织
                        </button>
                      )}
                    </div>
                  </div>
                  <Table
                    headers={["组织", "上级组织", "操作"]}
                    rows={rows}
                    render={(row) => (
                      <>
                        <td>
                          <strong>{row.name}</strong>
                          <span className="sub-id">
                            <ID value={row.id} />
                          </span>
                        </td>
                        <td>
                          {row.parent_id
                            ? rows.find((x) => x.id === row.parent_id)?.name ||
                              short(row.parent_id)
                            : "空间根组织"}
                        </td>
                        <td>
                          <div className="actions">
                            {can("members.read") && (
                              <button
                                className="text-button"
                                onClick={() =>
                                  setOrgAction({
                                    kind: "members",
                                    org: row,
                                    etag: visible.etag,
                                  })
                                }
                              >
                                查看直属成员
                              </button>
                            )}
                            {row.parent_id && can("organizations.write") ? (
                              <>
                                <button
                                  className="text-button"
                                  onClick={() => open({ kind: "move", row })}
                                >
                                  移动组织
                                </button>
                                <button
                                  className="text-button"
                                  onClick={() =>
                                    setOrgAction({
                                      kind: "rename",
                                      org: row,
                                      etag: visible.etag,
                                    })
                                  }
                                >
                                  重命名
                                </button>
                                <button
                                  className="danger-link"
                                  onClick={() =>
                                    setOrgAction({
                                      kind: "delete",
                                      org: row,
                                      etag: visible.etag,
                                    })
                                  }
                                >
                                  删除空组织
                                </button>
                              </>
                            ) : (
                              <span className="muted">
                                {row.parent_id ? "只读" : "根组织"}
                              </span>
                            )}
                          </div>
                        </td>
                      </>
                    )}
                  />
                </section>
              )}
              {tab === "organizations" && orgAction && (
                <OrganizationActions
                  key={context + orgAction.kind + orgAction.org.id}
                  action={orgAction}
                  context={context}
                  tenant={me.tenant_id}
                  csrf={csrf.current}
                  etag={orgAction.etag}
                  canWrite={can("organizations.write")}
                  onClose={() => setOrgAction(null)}
                  onFailure={fail}
                  onChanged={({ invalidates, message }) => {
                    setOrgAction(null);
                    setAssignmentOpen(false);
                    setPolicyHistoryRole(null);
                    setInvitationAction(null);
                    setNotice(message);
                    if (invalidates) {
                      setAuthNeeded(true);
                      setData(null);
                    } else setRevision((n) => n + 1);
                  }}
                />
              )}
              {tab === "invitations" && (
                <section className="panel">
                  <div className="panel-heading">
                    <h2>成员邀请</h2>
                    <button onClick={() => open({ kind: "invite" })}>
                      创建邀请
                    </button>
                  </div>
                  <p className="section-note">
                    邀请绑定对方已验证的邮箱，仅授予成员资格。关闭凭证页面后无法再次查看凭证。
                  </p>
                  <Table
                    headers={["受邀邮箱", "有效期", "状态", "操作"]}
                    rows={rows}
                    render={(row) => (
                      <>
                        <td>{row.email}</td>
                        <td>{date(row.expires_at)}</td>
                        <td>
                          <span className="pill neutral">
                            {
                              {
                                pending: "待接受",
                                expired: "已过期",
                                closed: "已关闭",
                              }[row.state]
                            }
                          </span>
                        </td>
                        <td>
                          {row.state !== "closed" && (
                            <button
                              className="text-button"
                              onClick={() => setInvitationAction(row)}
                            >
                              更新邀请凭证
                            </button>
                          )}
                          {row.state === "pending" && (
                            <button
                              className="text-button"
                              onClick={() =>
                                open({ kind: "revoke-invite", row })
                              }
                            >
                              撤销邀请
                            </button>
                          )}
                        </td>
                      </>
                    )}
                  />
                </section>
              )}
              {tab === "playground" && (
                <Playground
                  key={context}
                  context={context}
                  csrf={csrf.current}
                  me={me}
                  tenant={tenant}
                  onUnauthorized={() =>
                    fail(new ApiError(401, "invalid_token"))
                  }
                />
              )}
              {tab === "applications" && (
                <ApplicationManagement
                  key={context}
                  context={{ context, tenant_id: me.tenant_id }}
                  csrf={csrf.current}
                  onFailure={fail}
                  revision={revision}
                />
              )}
              {tab === "audit" && (
                <MachineEvents
                  key={context + "machine-events"}
                  context={context}
                  csrf={csrf.current}
                  tenant={me.tenant_id}
                  revision={revision}
                  onFailure={fail}
                />
              )}
              {tab === "audit" && (
                <AuditExport
                  key={context + "audit-export"}
                  context={context}
                  csrf={csrf.current}
                  tenant={me.tenant_id}
                  revision={revision}
                  onFailure={fail}
                />
              )}
              {tab === "roles" && (
                <RoleManagement
                  key={context + base + revision}
                  context={context}
                  roles={rows}
                  etag={visible?.etag}
                  catalog={permissions?.catalog || []}
                  base={base}
                  me={me}
                  permissions={permissions?.permissions || []}
                  read={(path, options) =>
                    request(context, csrf.current, "/bff" + path, options)
                  }
                  write={mutate}
                  busy={busy}
                  error={error}
                  clearError={(e) => setError(e || null)}
                  initialMember={roleMember}
                  onFailure={fail}
                  onHistory={(role) => setPolicyHistoryRole(role)}
                  onConsumed={() => setRoleMember(null)}
                />
              )}
              {tab === "audit" && (
                <section className="panel">
                  <div className="panel-heading">
                    <h2>空间审计</h2>
                  </div>
                  <Table
                    headers={["操作", "操作者", "目标", "时间"]}
                    rows={rows}
                    render={(row) => (
                      <>
                        <td>{actionNames[row.action] || row.action}</td>
                        <td>
                          <ID value={row.actor_membership_id} />
                        </td>
                        <td>
                          {row.role_code ? (
                            <span>
                              {row.role_code} · <ID value={row.membership_id} />
                            </span>
                          ) : (
                            <ID value={row.target_id} />
                          )}
                          {can("roles.manage") &&
                            [
                              "role.created",
                              "role.updated",
                              "role.deleted",
                            ].includes(row.action) && (
                              <button
                                className="text-button"
                                onClick={() =>
                                  setPolicyHistoryRole(row.target_id)
                                }
                              >
                                查看角色策略历史
                              </button>
                            )}
                        </td>
                        <td>{date(row.occurred_at)}</td>
                      </>
                    )}
                  />
                </section>
              )}
              {tab === "sessions" && (
                <section className="panel">
                  <AccountSecurity
                    key={context + ":" + revision}
                    context={context}
                    csrf={csrf.current}
                    onFailure={fail}
                    onRevoke={() => open({ kind: "revoke-others" })}
                  />
                  <div className="panel-heading">
                    <h2>我的统一登录会话</h2>
                    <a
                      href="http://auth.localhost:8080/sso/logout"
                      className="danger-link"
                    >
                      退出所有设备
                    </a>
                  </div>
                  <Table
                    headers={["会话", "身份验证", "状态", "操作"]}
                    rows={rows}
                    render={(row) => (
                      <>
                        <td>
                          <ID value={row.id} />
                          {row.current && (
                            <span className="pill">当前会话</span>
                          )}
                        </td>
                        <td>
                          {date(row.authenticated_at)}
                          <small>
                            {row.second_factor === "otp"
                              ? "认证器验证码"
                              : "一次性恢复码"}
                          </small>
                        </td>
                        <td>{row.active ? "有效" : "已结束"}</td>
                        <td>
                          {row.active && (
                            <button
                              className="text-button"
                              onClick={() =>
                                open({ kind: "revoke-session", row })
                              }
                            >
                              撤销会话
                            </button>
                          )}
                        </td>
                      </>
                    )}
                  />
                </section>
              )}
              {loading && (
                <p className="loading" role="status">
                  正在载入资料…
                </p>
              )}
              {visible?.next && (
                <div className="pagination">
                  <button
                    className="secondary"
                    disabled={loading}
                    onClick={() => loadList(undefined, true, visible.next)}
                  >
                    加载更多
                  </button>
                </div>
              )}
            </>
          )}
          <footer className="page-footer">
            Identity · 身份与空间管理<span>所有管理操作均受服务端权限检查</span>
          </footer>
        </main>
      </div>
      {visible &&
        invitationAction &&
        me &&
        tenant &&
        !authNeeded &&
        tab === "invitations" && (
          <InvitationReissue
            key={context + ":" + invitationAction.id}
            context={context}
            tenant={me.tenant_id}
            csrf={csrf.current}
            invitation={invitationAction}
            close={() => {
              setInvitationAction(null);
              setRevision((x) => x + 1);
            }}
            onChanged={() => setRevision((x) => x + 1)}
            onFailure={fail}
          />
        )}
      {secret && (
        <Dialog title="保存一次性邀请凭证" busy={busy} onClose={close}>
          <p>请通过安全渠道转交给对应邮箱的用户。此凭证只在这里显示一次。</p>
          <pre className="secret" data-testid="invitation-secret">
            {secret}
          </pre>
          <div className="dialog-actions">
            <button
              className="secondary"
              onClick={async () => {
                try {
                  await navigator.clipboard.writeText(secret);
                  setNotice("邀请凭证已复制。");
                } catch {
                  setNotice("请手动选择并复制凭证。");
                }
              }}
            >
              复制邀请凭证
            </button>
            <button onClick={close}>我已安全保存</button>
          </div>
        </Dialog>
      )}
      {assignmentOpen &&
        me &&
        tenant &&
        tab === "organizations" &&
        !authNeeded &&
        can("organizations.write") &&
        can("members.read") && (
          <OrganizationAssignment
            key={context + me.tenant_id + revision}
            context={context}
            csrf={csrf.current}
            tenant={me.tenant_id}
            root={tenant.root_id}
            onClose={() => setAssignmentOpen(false)}
            onFailure={fail}
            onChanged={() => {
              setAssignmentOpen(false);
              setPolicyHistoryRole(null);
              setData(null);
              setAuthNeeded(true);
              setNotice("组织归属已更新，请重新确认空间授权。");
            }}
          />
        )}
      {policyHistoryRole &&
        me &&
        tenant &&
        !authNeeded &&
        can("roles.manage") && (
          <RolePolicyHistory
            key={context + me.tenant_id + policyHistoryRole + revision}
            context={context}
            csrf={csrf.current}
            tenant={me.tenant_id}
            role={policyHistoryRole}
            onClose={() => setPolicyHistoryRole(null)}
            onFailure={fail}
          />
        )}
      {modal && !secret && (
        <Dialog
          title={
            {
              "new-tenant": "创建空间",
              "new-org": "创建组织",
              invite: "邀请成员",
              accept: "接受成员邀请",
              move: "移动组织",
              member:
                modal.row?.status === "active"
                  ? "确认停用成员"
                  : "确认恢复成员",
              "revoke-invite": "确认撤销邀请",
              "revoke-session": "确认撤销会话",
              "revoke-others": "撤销其他统一登录会话",
            }[modal.kind]
          }
          busy={busy}
          onClose={close}
        >
          <form onSubmit={submit}>
            {modal.kind === "new-tenant" && (
              <>
                <label>
                  空间名称
                  <input name="name" required maxLength="128" autoFocus />
                </label>
                <label>
                  空间标识
                  <input
                    name="slug"
                    required
                    pattern="[a-z0-9][a-z0-9-]{0,62}"
                    maxLength="63"
                    placeholder="例如 design-team"
                  />
                </label>
                <p className="muted">标识只包含小写字母、数字和连字符。</p>
              </>
            )}
            {modal.kind === "new-org" && (
              <>
                <label>
                  组织名称
                  <input name="name" required maxLength="128" autoFocus />
                </label>
                <ParentSelect rows={rows} root={tenant.root_id} />
              </>
            )}
            {modal.kind === "invite" && (
              <>
                <label>
                  受邀邮箱
                  <input
                    name="email"
                    type="email"
                    required
                    maxLength="254"
                    autoFocus
                  />
                </label>
                <p className="muted">
                  对方使用此邮箱完成身份验证后，可接受邀请。邀请不会授予管理权限。
                </p>
              </>
            )}
            {modal.kind === "accept" && (
              <>
                <label>
                  一次性邀请凭证
                  <input
                    name="token"
                    required
                    pattern="[A-Za-z0-9_-]{43}"
                    autoComplete="off"
                    autoFocus
                  />
                </label>
                <p className="muted">受邀邮箱需与你账户中已验证的邮箱一致。</p>
              </>
            )}
            {modal.kind === "move" && (
              <>
                <p>将「{modal.row.name}」及其下级组织移到新的上级。</p>
                <ParentSelect
                  rows={rows.filter((row) => row.id !== modal.row.id)}
                  root={tenant.root_id}
                  value={modal.parent}
                />
                {modal.plan && (
                  <p className="notice" role="status">
                    将移动 {modal.plan.subtree_nodes} 个组织，移动后最深层级为{" "}
                    {modal.plan.resulting_depth}。
                  </p>
                )}
              </>
            )}
            {modal.kind === "member" && (
              <p>
                目标账户：
                <ID value={modal.row.user_id} />。
                {modal.row.status === "active"
                  ? "停用后，对方在此空间的访问资格将失效。"
                  : "恢复后，对方需要重新授权才能访问此空间。"}
              </p>
            )}
            {modal.kind === "revoke-invite" && (
              <p>
                将撤销发给 {modal.row.email} 的邀请。撤销后，原凭证无法再接受。
              </p>
            )}
            {modal.kind === "revoke-others" && (
              <p>
                将撤销此账户所有其他现有统一登录会话及其应用授权，保留当前统一登录会话。此后新完成的登录仍可创建会话。确认后只发送一次。
              </p>
            )}
            {modal.kind === "revoke-session" && (
              <p>
                {modal.row.current
                  ? "这会结束当前统一登录，当前控制台访问也将失效。"
                  : "这会结束所选统一登录会话，其后续访问资格将失效。"}
              </p>
            )}
            {error && <ErrorNotice error={error} />}
            <div className="dialog-actions">
              <button
                type="button"
                className="secondary"
                onClick={close}
                disabled={busy}
              >
                取消
              </button>
              {modal.kind === "move" && (
                <button
                  type="submit"
                  name="action"
                  value="preview"
                  className="secondary"
                  disabled={busy}
                >
                  预览移动
                </button>
              )}
              <button
                type="submit"
                name="action"
                value="confirm"
                disabled={busy}
              >
                {busy
                  ? "正在处理…"
                  : modal.kind === "move"
                    ? "确认移动"
                    : modal.kind === "member" ||
                        modal.kind.startsWith("revoke-")
                      ? "确认操作"
                      : "保存并继续"}
              </button>
            </div>
          </form>
        </Dialog>
      )}
    </div>
  );
}
function ErrorNotice({ error }) {
  return (
    <div role="alert" className="error">
      <p>{errorText(error)}</p>
      {error.code === "recent_mfa_required" && (
        <a href="/login?force=1">重新验证身份</a>
      )}
      {error.requestID && <small>请求编号：{error.requestID}</small>}
    </div>
  );
}
function ParentSelect({ rows, root, value }) {
  return (
    <label>
      上级组织
      <select name="parent" required defaultValue={value || root}>
        {!rows.some((row) => row.id === root) && (
          <option value={root}>空间根组织</option>
        )}
        {rows.map((row) => (
          <option key={row.id} value={row.id}>
            {row.name}
          </option>
        ))}
      </select>
    </label>
  );
}
function Table({ headers, rows, render }) {
  return rows.length ? (
    <div className="table-scroll">
      <table>
        <thead>
          <tr>
            {headers.map((x) => (
              <th key={x}>{x}</th>
            ))}
          </tr>
        </thead>
        <tbody>
          {rows.map((row) => (
            <tr key={row.id} data-record-id={row.id}>
              {render(row)}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  ) : (
    <Empty>暂无记录</Empty>
  );
}
class Boundary extends React.Component {
  state = { failed: false };
  static getDerivedStateFromError() {
    return { failed: true };
  }
  render() {
    return this.state.failed ? (
      <main className="welcome">
        <h1>页面暂时不可用</h1>
        <a href="/">返回控制台</a>
      </main>
    ) : (
      this.props.children
    );
  }
}
createRoot(document.getElementById("root")).render(
  <Boundary>
    <App />
  </Boundary>,
);
