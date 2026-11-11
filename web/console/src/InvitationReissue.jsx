import React, { useEffect, useRef, useState } from "react";
import { request, errorText } from "./api.js";
import {
  validateInvitationDetail,
  validateReissuedInvitation,
} from "./invitation.js";

export default function InvitationReissue({
  context,
  tenant,
  csrf,
  invitation,
  close,
  onChanged,
  onFailure,
}) {
  const [value, setValue] = useState(null),
    [busy, setBusy] = useState(false),
    [reading, setReading] = useState(true),
    [error, setError] = useState(null),
    [confirmed, setConfirmed] = useState(false),
    [revealed, setRevealed] = useState(false),
    [notice, setNotice] = useState("");
  const dialog = useRef(null),
    alive = useRef(true),
    epoch = useRef(0),
    pending = useRef(false),
    abort = useRef(null),
    secret = useRef("");
  const url = "/bff/v1/tenants/" + tenant + "/invitations/" + invitation.id;
  function fail(failure) {
    setError(failure);
    if (failure.status === 401 || failure.code === "recent_mfa_required")
      onFailure(failure);
  }
  async function read() {
    if (pending.current) return;
    secret.current = "";
    setRevealed(false);
    setConfirmed(false);
    setReading(true);
    setError(null);
    setValue(null);
    setNotice("");
    const generation = ++epoch.current,
      controller = new AbortController();
    abort.current?.abort();
    abort.current = controller;
    try {
      const response = await request(context, csrf, url, {
        signal: controller.signal,
      });
      if (
        !alive.current ||
        controller.signal.aborted ||
        generation !== epoch.current
      )
        return;
      setValue(validateInvitationDetail(response, tenant, invitation.id));
    } catch (failure) {
      if (
        alive.current &&
        !controller.signal.aborted &&
        generation === epoch.current
      )
        fail(failure);
    } finally {
      if (alive.current && generation === epoch.current) setReading(false);
    }
  }
  useEffect(() => {
    alive.current = true;
    dialog.current.showModal();
    read();
    return () => {
      alive.current = false;
      epoch.current++;
      abort.current?.abort();
      secret.current = "";
    };
  }, []);
  function leave() {
    secret.current = "";
    epoch.current++;
    abort.current?.abort();
    close();
  }
  async function save(event) {
    event.preventDefault();
    if (
      pending.current ||
      reading ||
      !confirmed ||
      !value ||
      value.data.state === "closed" ||
      error ||
      revealed
    )
      return;
    pending.current = true;
    setBusy(true);
    setError(null);
    setConfirmed(false);
    secret.current = "";
    const generation = ++epoch.current,
      controller = new AbortController();
    abort.current?.abort();
    abort.current = controller;
    try {
      const response = await request(context, csrf, url + "/reissue", {
        method: "POST",
        body: {},
        etag: value.etag,
        signal: controller.signal,
      });
      if (
        !alive.current ||
        controller.signal.aborted ||
        generation !== epoch.current
      )
        return;
      const issued = validateReissuedInvitation(
        response,
        tenant,
        invitation.id,
        value.data.revision,
      );
      secret.current = issued.token;
      setRevealed(true);
      setNotice("");
      onChanged();
    } catch (failure) {
      if (
        alive.current &&
        !controller.signal.aborted &&
        generation === epoch.current
      ) {
        secret.current = "";
        setRevealed(false);
        setValue(null);
        setNotice(
          "结果可能已保存。不会自动重发；先读取当前状态，再决定是否重新更新凭证。",
        );
        fail(failure);
      }
    } finally {
      if (generation === epoch.current) {
        pending.current = false;
        if (alive.current) setBusy(false);
      }
    }
  }
  return (
    <dialog
      ref={dialog}
      className="invitation-reissue"
      aria-labelledby="invitation-reissue-title"
      onCancel={(event) => {
        event.preventDefault();
        leave();
      }}
    >
      <header className="dialog-header">
        <h2 id="invitation-reissue-title">更新一次性邀请凭证</h2>
        <button
          type="button"
          className="icon-button"
          aria-label="关闭邀请更新"
          onClick={leave}
        >
          ×
        </button>
      </header>
      <p>
        <strong>{invitation.email}</strong>
      </p>
      {error && <p role="alert">{errorText(error)}</p>}
      {notice && (
        <p role="status" className="notice">
          {notice}
        </p>
      )}
      {reading && <p role="status">正在读取当前邀请…</p>}
      {busy && (
        <p role="status">
          正在提交更新。关闭不会撤销已提交的操作，请稍后核对。
        </p>
      )}
      {revealed ? (
        <>
          <p>
            旧凭证已失效。新凭证有效 24
            小时，只在这里显示一次；请通过安全渠道转交给对应邮箱的用户。系统未发送邮件。
          </p>
          <pre className="secret" data-testid="reissued-invitation-secret">
            {secret.current}
          </pre>
          <div className="dialog-actions">
            <button
              className="secondary"
              onClick={async () => {
                if (!alive.current || !secret.current) return;
                try {
                  await navigator.clipboard.writeText(secret.current);
                  if (alive.current) setNotice("邀请凭证已复制。");
                } catch {
                  if (alive.current) setNotice("请手动选择并复制凭证。");
                }
              }}
            >
              复制新邀请凭证
            </button>
            <button onClick={leave}>我已安全保存</button>
          </div>
        </>
      ) : (
        <>
          {value && (
            <p>
              当前版本 {value.data.revision} ·{" "}
              {value.data.state === "closed"
                ? "已关闭，不能重新开启"
                : value.data.state === "expired"
                  ? "已过期，可更新凭证"
                  : "待接受"}
            </p>
          )}
          {value && value.data.state !== "closed" && (
            <form onSubmit={save}>
              <p>
                更新后旧凭证立即失效，新凭证有效 24
                小时。目标邮箱和成员资格不变；此操作不会发送邮件。
              </p>
              <label className="confirmation">
                <input
                  type="checkbox"
                  checked={confirmed}
                  disabled={busy}
                  onChange={(event) => setConfirmed(event.target.checked)}
                />
                我确认使旧凭证失效，并安全转交新凭证
              </label>
              <div className="dialog-actions">
                <button
                  type="button"
                  className="secondary"
                  disabled={busy}
                  onClick={read}
                >
                  读取当前邀请
                </button>
                <button type="button" className="secondary" onClick={leave}>
                  关闭
                </button>
                <button
                  type="submit"
                  disabled={!confirmed || busy || reading || !!error}
                >
                  确认更新一次
                </button>
              </div>
            </form>
          )}
          {!reading && !busy && (!value || value.data.state === "closed") && (
            <div className="dialog-actions">
              <button className="secondary" onClick={read}>
                读取当前邀请
              </button>
              <button className="secondary" onClick={leave}>
                关闭
              </button>
            </div>
          )}
        </>
      )}
    </dialog>
  );
}
