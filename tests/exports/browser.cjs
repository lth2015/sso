// Real console/BFF/primary export, download and stale-authorization boundaries.
const s = require("../support/console-fixtures.cjs"),
  n = require("../support/native-fixtures.cjs"),
  assert = require("node:assert/strict"),
  crypto = require("node:crypto"),
  { execFileSync } = require("node:child_process");
let phase = "startup",
  checks = 0,
  downloads = 0;
const secrets = [],
  started = new Date().toISOString();
const progress = setInterval(
  () => console.log("Audit export UI phase: " + phase),
  20000,
);
function normalized(url) {
  return url
    .split("?")[0]
    .replace(/\/tenants\/[a-f0-9]{32}/, "/tenants/{tenant}")
    .replace(/\/members\/[a-f0-9]{32}/, "/members/{member}")
    .replace(/\/roles\/[a-f0-9]{32}/, "/roles/{role}")
    .replace(/\/applications\/[a-f0-9]{32}/, "/applications/{application}")
    .replace(
      /\/service-principals\/[a-f0-9]{32}/,
      "/service-principals/{principal}",
    )
    .replace(/\/credentials\/[a-f0-9]{32}/, "/credentials/{credential}");
}
function validate(url, method, r, body) {
  execFileSync("python3", ["tests/api/validate_contract.py"], {
    cwd: s.root,
    input: JSON.stringify({
      contract: "console",
      path: "/bff" + normalized(url),
      method,
      status: r.status,
      body: r.body,
      request_body: body,
      content_type: r.contentType,
      request_id: r.requestID,
    }),
    stdio: ["pipe", "pipe", "pipe"],
  });
  checks++;
}
async function call(p, method, url, body, status = 200, etag) {
  const r = await s.bff(p, url, {
    method,
    body,
    headers: etag ? { "If-Match": etag } : {},
  });
  if (r.status !== status)
    console.error(
      "Unexpected export BFF status: " +
        method +
        " " +
        normalized(url) +
        " expected=" +
        status +
        " received=" +
        r.status,
    );
  assert.equal(r.status, status);
  validate(url, method, r, body);
  return r;
}
function privateAbsent(value) {
  const text = JSON.stringify(value);
  for (const secret of secrets) assert(!text.includes(secret));
}
async function download(p, region) {
  const awaited = p.waitForEvent("download");
  await region
    .getByRole("button", { name: "下载这一批 JSON", exact: true })
    .click();
  const result = await awaited;
  assert.match(
    result.suggestedFilename(),
    /^audit-(management|machine)-[a-f0-9]{32}-[0-9TZ]+\.json$/,
  );
  const stream = await result.createReadStream(),
    chunks = [];
  for await (const chunk of stream) chunks.push(chunk);
  const bytes = Buffer.concat(chunks);
  assert(bytes.length < 256 * 1024);
  const data = JSON.parse(bytes.toString());
  privateAbsent(data);
  return data;
}
(async () => {
  try {
    await s.start();
    const owner = await s.register(true),
      guest = await s.register();
    let me = await s.login(owner);
    await s.login(guest);
    const p = owner.page,
      tenant = me.tenant_id,
      base = "/v1/tenants/" + tenant,
      path = base + "/audit-exports";
    p.on("download", () => downloads++);
    phase = "real-data-and-delegated-auditor-setup";
    const invitation = (
      await call(p, "POST", base + "/invitations", { email: guest.email }, 201)
    ).body;
    secrets.push(invitation.token);
    await call(
      guest.page,
      "POST",
      "/v1/invitations/accept",
      { token: invitation.token },
      201,
    );
    me = await s.login(owner, p, tenant);
    await s.login(guest, guest.page, tenant);
    await call(guest.page, "GET", path, undefined, 403);
    const member = (await call(p, "GET", base + "/members")).body.items.find(
      (m) => m.email === guest.email,
    ).id;
    let current = await call(p, "GET", base);
    const role = (
      await call(
        p,
        "POST",
        base + "/roles",
        {
          code: "export_reader",
          policies: [{ permission: "audit.read", effect: "allow" }],
        },
        201,
        current.etag,
      )
    ).body;
    me = await s.login(owner, p, tenant);
    current = await call(p, "GET", base);
    await call(
      p,
      "PUT",
      base + "/members/" + member + "/roles/" + role.id,
      {},
      204,
      current.etag,
    );
    me = await s.login(owner, p, tenant);
    await s.login(guest, guest.page, tenant);
    for (let i = 0; i < 26; i++) {
      const r = await call(
        p,
        "POST",
        base + "/invitations",
        {
          email:
            "export-ui-" +
            crypto.randomBytes(8).toString("hex") +
            "@example.invalid",
        },
        201,
      );
      secrets.push(r.body.token);
    }
    const application = (
        await call(
          p,
          "POST",
          base + "/applications",
          { name: "Audit export UI application" },
          201,
        )
      ).body,
      spBase = base + "/applications/" + application.id + "/service-principals";
    const principal = (
      await call(p, "POST", spBase, { name: "Audit export UI worker" }, 201)
    ).body;
    const key = (
      await call(
        p,
        "POST",
        spBase + "/" + principal.id + "/credentials",
        { name: "Export UI key", expires_in_days: 1 },
        201,
      )
    ).body;
    secrets.push(key.secret_key);
    const exchanged = await n.request(
      4464,
      "auth.localhost:4464",
      "POST",
      "/oauth2/token",
      new URLSearchParams({
        grant_type: "client_credentials",
        scope: "machine:invoke",
        audience: key.credential.audience,
      }).toString(),
      {
        "Content-Type": "application/x-www-form-urlencoded",
        Authorization:
          "Basic " +
          Buffer.from(
            key.credential.access_key + ":" + key.secret_key,
          ).toString("base64"),
      },
    );
    assert.equal(exchanged.status, 200);
    secrets.push(exchanged.body.access_token);
    await call(
      guest.page,
      "GET",
      spBase + "/" + principal.id + "/credentials",
      undefined,
      403,
    );
    phase = "management-preview-fresh-download-and-keyset";
    phase = "actual-management-response-model";
    const model = await import(
      "file://" + s.root + "/web/console/src/audit-export.js"
    );
    const actual = (await call(p, "GET", path + "?kind=management&limit=25"))
      .body;
    model.validateAuditBatch(actual, tenant, "management", 25);
    phase = "navigate-audit";
    await s.nav(p, "审计");
    phase = "locate-export-region";
    let region = p.getByRole("region", { name: "审计导出", exact: true });
    await region.waitFor();
    phase = "select-export-limit";
    await region.getByLabel("每批条数", { exact: true }).selectOption("25");
    phase = "generate-preview-click";
    await region
      .getByRole("button", { name: "生成首批预览", exact: true })
      .click();
    phase = "management-first-preview";
    try {
      await region
        .getByTestId("audit-export-summary")
        .filter({ hasText: "本批 25 条" })
        .waitFor();
    } catch {
      console.error(
        JSON.stringify({
          exportPreview: {
            alert: await region.getByRole("alert").count(),
            summary: await region.getByTestId("audit-export-summary").count(),
            invalidData: (await region.innerText()).includes(
              "导出数据格式不正确",
            ),
            pageErrors: s.pageErrors.length,
          },
        }),
      );
      throw new Error("export preview failed");
    }
    phase = "management-first-download";

    const first = await download(p, region);
    assert.equal(first.kind, "management");
    assert.equal(first.items.length, 25);
    assert(first.next_cursor);
    assert.equal(first.tenant_id, tenant);
    validate(path + "?kind=management&limit=25", "GET", {
      status: 200,
      body: first,
      contentType: "application/json",
    });
    await region
      .getByRole("button", { name: "读取下一批", exact: true })
      .click();
    await region.getByTestId("audit-export-summary").waitFor();
    phase = "management-next-download";
    const second = await download(p, region);
    assert(second.items.length > 0);
    const firstIDs = new Set(first.items.map((v) => v.id));
    assert(second.items.every((v) => !firstIDs.has(v.id)));
    phase = "machine-preview-download-mobile-and-readonly-auditor";
    await region
      .getByLabel("导出类型", { exact: true })
      .selectOption("machine");
    assert(
      await region
        .getByRole("button", { name: "下载这一批 JSON", exact: true })
        .isDisabled(),
    );
    await region
      .getByRole("button", { name: "生成首批预览", exact: true })
      .click();
    await region
      .getByTestId("audit-export-summary")
      .filter({ hasText: "本批 1 条" })
      .waitFor();
    const machine = await download(p, region);
    assert.equal(machine.kind, "machine");
    assert.equal(machine.items[0].service_principal_id, principal.id);
    assert(!Object.hasOwn(machine.items[0], "actor_membership_id"));
    validate(path + "?kind=machine&limit=25", "GET", {
      status: 200,
      body: machine,
      contentType: "application/json",
    });
    privateAbsent(await region.innerText());
    await region.screenshot({
      path: s.root + "/docs/ui/audit-export-desktop.png",
    });
    await p.setViewportSize({ width: 390, height: 844 });
    assert(
      await p.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth + 1,
      ),
    );
    await region.screenshot({
      path: s.root + "/docs/ui/audit-export-mobile.png",
    });
    await p.setViewportSize({ width: 1365, height: 900 });
    await s.nav(guest.page, "审计");
    const readonly = guest.page.getByRole("region", {
      name: "审计导出",
      exact: true,
    });
    await readonly
      .getByLabel("导出类型", { exact: true })
      .selectOption("machine");
    await readonly
      .getByRole("button", { name: "生成首批预览", exact: true })
      .click();
    await readonly
      .getByTestId("audit-export-summary")
      .filter({ hasText: "本批 1 条" })
      .waitFor();
    await download(guest.page, readonly);
    await call(guest.page, "GET", path + "?kind=machine");
    phase = "download-rereads-current-authority-not-stale-preview";
    const match = (url) => url.pathname === "/bff" + path;
    let requests = 0;
    await p.route(match, async (route) => {
      requests++;
      await route
        .fulfill({
          status: 403,
          headers: {
            "Content-Type": "application/problem+json",
            "X-Request-ID": "0".repeat(24),
          },
          body: JSON.stringify({
            type: "urn:sso:problem:forbidden",
            title: "forbidden",
            status: 403,
            code: "forbidden",
            request_id: "0".repeat(24),
          }),
        })
        .catch(() => {});
    });
    let baseline = downloads;
    await region
      .getByRole("button", { name: "下载这一批 JSON", exact: true })
      .click();
    await region.getByRole("alert").waitFor();
    assert.equal(requests, 1);
    assert.equal(downloads, baseline);
    assert(
      await region
        .getByRole("button", { name: "下载这一批 JSON", exact: true })
        .isDisabled(),
    );
    assert.equal(await region.getByTestId("audit-export-json").count(), 0);
    await p.unroute(match);
    phase = "cancelled-read-cannot-trigger-late-download";
    await region
      .getByRole("button", { name: "生成首批预览", exact: true })
      .click();
    await region.getByTestId("audit-export-summary").waitFor();
    const held = (await call(p, "GET", path + "?kind=machine&limit=25")).body;
    async function holdDownload() {
      let release, captured;
      const gate = new Promise((r) => (release = r)),
        seen = new Promise((r) => (captured = r));
      const handler = async (route) => {
        captured();
        await gate;
        await route
          .fulfill({
            status: 200,
            contentType: "application/json",
            body: JSON.stringify(held),
          })
          .catch(() => {});
      };
      await p.route(match, handler);
      await region
        .getByRole("button", { name: "下载这一批 JSON", exact: true })
        .click();
      await seen;
      return async () => {
        release();
        await p.unroute(match, handler);
      };
    }
    let release = await holdDownload();
    baseline = downloads;
    await region
      .getByRole("button", { name: "取消导出读取", exact: true })
      .click();
    await release();
    await p.waitForTimeout(150);
    assert.equal(downloads, baseline);
    assert.equal(await region.getByTestId("audit-export-json").count(), 0);
    phase = "workspace-change-drops-inflight-old-batch";
    await region
      .getByRole("button", { name: "生成首批预览", exact: true })
      .click();
    await region.getByTestId("audit-export-summary").waitFor();
    const spaces = (await s.bff(p, "/v1/tenants")).body.items,
      other = spaces.find((v) => v.id !== tenant).id,
      otherPage = await owner.context.newPage();
    await s.login(owner, otherPage, other);
    const hash = new URL(otherPage.url()).hash;
    release = await holdDownload();
    baseline = downloads;
    await p.evaluate((hash) => {
      location.hash = hash;
    }, hash);
    await p.getByRole("heading", { name: "空间概览", exact: true }).waitFor();
    assert.equal((await s.bff(p, "/v1/me")).body.tenant_id, other);
    await release();
    await p.waitForTimeout(150);
    assert.equal(downloads, baseline);
    assert.equal(
      await p.getByRole("region", { name: "审计导出", exact: true }).count(),
      0,
    );
    assert.equal(s.pageErrors.length, 0);
    assert.equal(s.leaks.length, 0);
    const privacy = require("../support/private-material.cjs")(
      s.root,
      secrets,
      started,
    );
    console.log(
      "Audit export UI passed: " +
        checks +
        " contract responses; real downloads/pagination/management-machine projections, readonly delegation, owner-only credential isolation, current-authority refusal, cancellation and workspace late-response safety; " +
        privacy.files +
        " candidate files/runtime logs private-material exclusion",
    );
  } finally {
    clearInterval(progress);
    await s.close();
  }
})().catch((e) => {
  console.error(
    "Audit export UI failed: phase=" +
      phase +
      ", error=" +
      e.name +
      " (private details withheld)",
  );
  console.error(JSON.stringify(s.diagnostics()));
  process.exitCode = 1;
});
