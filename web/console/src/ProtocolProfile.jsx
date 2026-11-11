import React, { useEffect, useRef, useState } from "react";
import { request, errorText } from "./api.js";
import {
  validateProtocolObservation,
  profileFields,
} from "./protocol-profile.js";
export default function ProtocolProfile({
  context,
  csrf,
  tenant,
  application,
  principal = null,
  resource,
  revision,
  kind,
  disabled = false,
  onFailure,
}) {
  const [value, setValue] = useState(null),
    [busy, setBusy] = useState(false),
    [error, setError] = useState(null);
  const alive = useRef(true),
    epoch = useRef(0),
    pending = useRef(false),
    abort = useRef(null);
  const url =
    "/bff/v1/tenants/" +
    tenant +
    "/applications/" +
    application +
    (kind === "public_client"
      ? "/clients/" + resource
      : "/service-principals/" + principal + "/credentials/" + resource) +
    "/protocol-status";
  function clear() {
    epoch.current++;
    abort.current?.abort();
    pending.current = false;
    setBusy(false);
    setValue(null);
    setError(null);
  }
  useEffect(() => {
    alive.current = true;
    return () => {
      alive.current = false;
      epoch.current++;
      abort.current?.abort();
    };
  }, []);
  useEffect(() => {
    clear();
  }, [context, url, revision, disabled]);
  async function read() {
    if (pending.current || disabled) return;
    pending.current = true;
    setBusy(true);
    setError(null);
    setValue(null);
    const current = ++epoch.current,
      controller = new AbortController();
    abort.current?.abort();
    abort.current = controller;
    try {
      const r = await request(context, csrf, url, {
        signal: controller.signal,
      });
      if (
        !alive.current ||
        controller.signal.aborted ||
        current !== epoch.current
      )
        return;
      setValue(
        validateProtocolObservation(r, {
          tenant,
          application,
          principal,
          resource,
          revision,
          kind,
        }),
      );
    } catch (e) {
      if (
        alive.current &&
        !controller.signal.aborted &&
        current === epoch.current
      ) {
        setError(e);
        if (e.status === 401) onFailure(e);
      }
    } finally {
      if (current === epoch.current) {
        pending.current = false;
        if (alive.current) setBusy(false);
      }
    }
  }
  return (
    <section className="protocol-observation" aria-label="协议登记核对">
      <h4>协议登记核对</h4>
      <p className="section-note">
        只读比较受控登记字段。观察结果不改变本地生效状态；激活、撤销和清理仍需明确确认。
        {kind === "machine_credential"
          ? "核对不能验证或恢复 SK，未知凭证请撤销后重建。"
          : "待完成登记仍需确认激活后才能登录。"}
      </p>
      <div className="actions">
        <button
          type="button"
          className="secondary"
          disabled={disabled || busy}
          onClick={read}
        >
          只读核对协议登记
        </button>
        {busy && (
          <button type="button" className="text-button" onClick={clear}>
            取消核对读取
          </button>
        )}
      </div>
      {busy && <p role="status">正在核对协议登记…</p>}
      {error && <p role="alert">{errorText(error)}</p>}
      {value && (
        <div data-testid="protocol-observation-result">
          <p role="status">
            {
              {
                matching: "受控字段一致",
                absent: "未发现协议登记",
                conflict: "受控字段存在差异",
              }[value.profile_state]
            }
          </p>
          {value.differences.length > 0 && (
            <ul aria-label="差异字段">
              {value.differences.map((field) => (
                <li key={field}>{profileFields[field]}</li>
              ))}
            </ul>
          )}
          <p className="section-note">
            登记版本 {value.revision} · 观察于{" "}
            {new Date(value.observed_at).toLocaleString("zh-CN")} ·
            当前主库资格已复查
          </p>
        </div>
      )}
    </section>
  );
}
