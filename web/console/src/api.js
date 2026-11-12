export class ApiError extends Error {
  constructor(status, code, requestID = "") {
    super(code);
    this.status = status;
    this.code = code;
    this.requestID = requestID;
  }
}
export async function request(
  context,
  csrf,
  path,
  { method = "GET", body, etag, signal } = {},
) {
  const headers = { "X-Console-Context": context };
  if (method !== "GET") {
    headers["X-CSRF-Token"] = csrf;
    headers["Content-Type"] = "application/json";
  }
  if (etag) headers["If-Match"] = etag;
  let response;
  try {
    response = await fetch(path, {
      method,
      headers,
      body: body === undefined ? undefined : JSON.stringify(body),
      signal,
      redirect: "error",
    });
  } catch (error) {
    if (error.name === "AbortError") throw error;
    throw new ApiError(0, "network_unavailable");
  }
  let data = null;
  if (response.status !== 204) {
    try {
      data = await response.json();
    } catch {
      throw new ApiError(502, "temporarily_unavailable");
    }
  }
  if (!response.ok)
    throw new ApiError(
      response.status,
      data?.code || "temporarily_unavailable",
      data?.request_id || response.headers.get("X-Request-ID") || "",
    );
  return { data, etag: response.headers.get("ETag") };
}
export const errorText = (error) =>
  ({
    network_unavailable:
      "连接中断，操作结果可能已保存。请刷新核对后再决定是否重试。",
    recent_mfa_required: "请重新验证身份，再发起这项操作。",
    reserved_role: "内置所有者角色不能编辑或删除。",
    service_principal_disabled: "机器身份已停用，请先恢复后再创建凭证。",
    credential_limit: "机器身份或凭证达到配额，请先检查待处理及有效凭证。",
    invalid_credential_state:
      "凭证状态或创建期间的资格已变化；不能恢复 SK，请核对后撤销并新建。",
    protocol_client_conflict:
      "协议配置与此登记不一致，请先核对；不会覆盖或删除不匹配的登记。",
    application_unmanaged: "当前空间仅能查看此共享应用，不能改写其客户端登记。",
    invalid_client_state: "此客户端状态不允许执行当前操作。",
    protected_organization: "空间根组织不能重命名或删除。",
    organization_not_empty:
      "请先移动子组织并移除所有直接成员归属，再删除空组织。",
    protected_application: "平台管理入口必须保持启用。",
    application_unavailable: "应用已被平台暂停，当前空间无法恢复。",
    role_in_use: "请先移除该角色的所有成员绑定。",
    last_active_owner: "至少需要保留一位有效的空间所有者。",
    version_conflict: "资料已被其他操作更新，请刷新后重新确认。",
    already_exists: "相同记录已存在，请核对后再操作。",
    root_cannot_move: "空间根组织不能移动。",
    organization_cycle: "不能把组织移动到它自身或下级组织中。",
    observation_changed: "观察期间登记已变化，请刷新当前记录后重新核对。",
    invalid_user_access_data:
      "应用准入设置或检查结果已变化，请关闭后重新读取。",
    invalid_access_history: "应用访问历史数据无效，请重新读取或关闭后再试。",
    invalid_hierarchy_data:
      "上级目录或移动预览已变化或数据无效，请关闭并刷新后重新确认。",
    invalid_assignment_choices: "关联目录已变化或数据无效，请关闭后重新读取。",
    invalid_policy_history: "策略历史数据不正确，请关闭后重新读取。",
    invalid_role_choices: "成员或角色列表已变化或数据无效，请关闭后重新读取。",
    invalid_protocol_data: "协议观察数据不正确，请刷新当前记录后重新核对。",
    invalid_invitation_data: "邀请数据不正确，请关闭后重新核对。",
    invalid_invitation_state: "邀请已关闭，不能重新开启。请创建新的邀请。",
    invalid_export_data: "导出数据格式不正确，请重新生成。",
    forbidden: "你没有执行这项管理操作的权限。",
    insufficient_scope: "当前授权不包含这项操作，请重新登录。",
    invalid_request: "请检查必填内容、字段格式、长度及请求内容后再试。",
    not_found: "找不到这项记录，或它不属于当前空间。",
    rate_limited: "操作过于频繁，请稍后再试。",
    temporarily_unavailable: "服务暂时不可用，请稍后刷新。",
  })[error?.code] || "操作未完成，请核对资料或稍后重试。";
