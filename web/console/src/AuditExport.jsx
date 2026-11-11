import React, { useEffect, useRef, useState } from "react";
import { request, errorText } from "./api.js";
import { validateAuditBatch, auditFilename } from "./audit-export.js";
export default function AuditExport({
  context,
  csrf,
  tenant,
  revision,
  onFailure,
}) {
  const [kind, setKind] = useState("management"),
    [limit, setLimit] = useState(50),
    [batch, setBatch] = useState(null),
    [busy, setBusy] = useState(false),
    [error, setError] = useState(null),
    [notice, setNotice] = useState("");
  const alive = useRef(true),
    epoch = useRef(0),
    pending = useRef(false),
    controller = useRef(null);
  function clear() {
    epoch.current++;
    controller.current?.abort();
    pending.current = false;
    setBatch(null);
    setBusy(false);
    setError(null);
    setNotice("");
  }
  useEffect(() => {
    alive.current = true;
    return () => {
      alive.current = false;
      epoch.current++;
      controller.current?.abort();
    };
  }, []);
  useEffect(() => {
    clear();
  }, [context, tenant, revision]);
  async function load(after = "", download = false) {
    if (pending.current) return;
    pending.current = true;
    setBusy(true);
    setError(null);
    setNotice("");
    setBatch(null);
    const current = ++epoch.current,
      abort = new AbortController(),
      query = { kind, limit, after };
    controller.current?.abort();
    controller.current = abort;
    try {
      const params = new URLSearchParams({ kind, limit: String(limit) });
      if (after) params.set("after", after);
      const response = await request(
        context,
        csrf,
        "/bff/v1/tenants/" + tenant + "/audit-exports?" + params,
        { signal: abort.signal },
      );
      if (!alive.current || abort.signal.aborted || current !== epoch.current)
        return;
      const data = validateAuditBatch(response.data, tenant, kind, limit);
      setBatch({ data, query });
      if (download) {
        const blob = new Blob([JSON.stringify(data, null, 2) + "\n"], {
          type: "application/json",
        });
        const url = URL.createObjectURL(blob),
          link = document.createElement("a");
        link.href = url;
        link.download = auditFilename(data);
        document.body.append(link);
        link.click();
        link.remove();
        setTimeout(() => URL.revokeObjectURL(url), 1000);
        setNotice("已发起本批 JSON 下载，内容以本次授权读取为准。");
      } else setNotice("本批已就绪。下载时会再次读取并确认当前资格。");
    } catch (failure) {
      if (alive.current && !abort.signal.aborted && current === epoch.current) {
        setError(failure);
        if (failure.status === 401) onFailure(failure);
      }
    } finally {
      if (current === epoch.current) {
        pending.current = false;
        if (alive.current) setBusy(false);
      }
    }
  }
  return (
    <section className="panel audit-export" aria-label="审计导出">
      <div className="panel-heading">
        <h2>导出审计批次</h2>
      </div>
      <p className="section-note">
        按当前空间的审计权限读取，每批最多 100
        条。文件包含受控身份标识、操作和时间；跨批次按游标继续读取。
      </p>
      <fieldset disabled={busy} className="audit-export-options">
        <label>
          导出类型
          <select
            aria-label="导出类型"
            value={kind}
            onChange={(event) => {
              clear();
              setKind(event.target.value);
            }}
          >
            <option value="management">管理操作</option>
            <option value="machine">机器签发</option>
          </select>
        </label>
        <label>
          每批条数
          <select
            aria-label="每批条数"
            value={limit}
            onChange={(event) => {
              clear();
              setLimit(Number(event.target.value));
            }}
          >
            <option value={25}>25</option>
            <option value={50}>50</option>
            <option value={100}>100</option>
          </select>
        </label>
      </fieldset>
      <div className="actions">
        <button disabled={busy} onClick={() => load()}>
          生成首批预览
        </button>
        <button
          className="secondary"
          disabled={busy || !batch}
          onClick={() => load(batch.query.after, true)}
        >
          下载这一批 JSON
        </button>
        <button
          className="secondary"
          disabled={busy || !batch?.data.next_cursor}
          onClick={() => load(batch.data.next_cursor)}
        >
          读取下一批
        </button>
        {busy && (
          <button
            className="text-button"
            onClick={() => {
              clear();
              setNotice("已取消本次读取。");
            }}
          >
            取消导出读取
          </button>
        )}
      </div>
      {busy && <p role="status">正在读取审计批次…</p>}
      {notice && (
        <p role="status" className="notice">
          {notice}
        </p>
      )}
      {error && <p role="alert">{errorText(error)}</p>}
      {batch && (
        <>
          <p data-testid="audit-export-summary">
            本批 {batch.data.items.length} 条 · 权限版本{" "}
            {batch.data.authz_version} · 生成于{" "}
            {new Date(batch.data.generated_at).toLocaleString("zh-CN")}
            {batch.data.next_cursor ? " · 还有下一批" : " · 已到当前末尾"}
          </p>
          <details>
            <summary>查看本批 JSON</summary>
            <pre data-testid="audit-export-json">
              {JSON.stringify(batch.data, null, 2)}
            </pre>
          </details>
        </>
      )}
    </section>
  );
}
