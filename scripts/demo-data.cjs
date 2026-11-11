const d = require("./lib/demo-support.cjs"),
  { chromium } = require("playwright");
let phase = "startup",
  browser,
  releaseLock;
const errors = [];
const progress = setInterval(
  () => console.log("Demo preparation phase: " + phase),
  20000,
);
const operation = process.argv[2];
async function lock() {
  await d.fs.mkdir(d.directory, { recursive: true, mode: 0o700 });
  const stat = await d.fs.lstat(d.directory);
  d.assert(stat.isDirectory() && !stat.isSymbolicLink());
  await d.fs.chmod(d.directory, 0o700);
  const filename = d.path.join(d.directory, "command.lock"),
    file = await d.fs.open(filename, "wx", 0o600);
  await file.writeFile(String(process.pid) + "\n");
  releaseLock = async () => {
    await file.close();
    await d.fs.unlink(filename);
  };
}
async function launch() {
  browser = await chromium.launch({
    executablePath: process.env.CHROMIUM_PATH || "/usr/bin/chromium",
    headless: true,
    args: ["--no-sandbox"],
  });
  return browser;
}
async function purgeMail(m) {
  for (const name of await d.fs.readdir(
    d.path.join(d.root, ".local/mailbox"),
  )) {
    if (!/^[a-f0-9]{32}\.json$/.test(name)) continue;
    const filename = d.path.join(d.root, ".local/mailbox", name);
    let message;
    try {
      message = JSON.parse(await d.fs.readFile(filename, "utf8"));
    } catch {
      continue;
    }
    if (m.accounts.some((a) => a.email === message.recipient))
      await d.fs.unlink(filename);
  }
}
async function seed(m) {
  if (m) {
    d.assert.equal(
      m.state,
      "ready",
      "incomplete demo: reset only this dataset before reseeding",
    );
    await verify(m, false);
    return;
  }
  m = {
    format: 1,
    tool: "scripts/demo",
    runId: d.crypto.randomBytes(16).toString("hex"),
    fingerprint: await d.fingerprint(),
    state: "creating",
    accounts: [],
    createdAt: new Date().toISOString(),
  };
  await d.save(m);
  await launch();
  phase = "register-and-enroll-owner";
  const owner = await d.register(browser, m, "owner", errors);
  phase = "register-and-enroll-auditor";
  const auditor = await d.register(browser, m, "auditor", errors);
  phase = "create-two-isolated-demo-spaces";
  m.primary = (
    await d.api(
      owner.page,
      "/v1/tenants",
      {
        method: "POST",
        body: { slug: "friday-" + m.runId + "-main", name: "星河科技" },
      },
      201,
    )
  ).body;
  await d.save(m);
  m.secondary = (
    await d.api(
      owner.page,
      "/v1/tenants",
      {
        method: "POST",
        body: { slug: "friday-" + m.runId + "-other", name: "远山合作社" },
      },
      201,
    )
  ).body;
  await d.save(m);
  d.execFileSync("scripts/sso", ["enable", "--email", owner.email], {
    cwd: d.root,
    stdio: "pipe",
  });
  await d.s.login(owner, owner.page, m.primary.id);
  phase = "seed-organization-tree";
  const base = "/v1/tenants/" + m.primary.id;
  m.organizations = [];
  async function org(name, parent = m.primary.root_id) {
    const current = await d.api(owner.page, base + "/organizations");
    const result = (
      await d.api(
        owner.page,
        base + "/organizations",
        {
          method: "POST",
          body: { name, parent_id: parent },
          headers: { "If-Match": current.etag },
        },
        201,
      )
    ).body;
    m.organizations.push({ id: result.id, name, parent });
    await d.save(m);
    return result.id;
  }
  const engineering = await org("研发中心");
  await org("平台组", engineering);
  await org("产品组", engineering);
  await org("商务协作");
  phase = "invite-readonly-auditor";
  const invitation = await d.api(
    owner.page,
    base + "/invitations",
    { method: "POST", body: { email: auditor.email } },
    201,
  );
  const joined = await d.api(
    auditor.page,
    "/v1/invitations/accept",
    { method: "POST", body: { token: invitation.body.token } },
    201,
  );
  await d.s.login(owner, owner.page, m.primary.id);
  let current = await d.api(owner.page, base);
  const role = await d.api(
    owner.page,
    base + "/roles",
    {
      method: "POST",
      headers: { "If-Match": current.etag },
      body: {
        code: "demo_auditor",
        policies: [
          { permission: "audit.read", effect: "allow" },
          { permission: "members.read", effect: "allow" },
          { permission: "organizations.read", effect: "allow" },
          { permission: "organizations.write", effect: "deny" },
        ],
      },
    },
    201,
  );
  m.auditorRole = role.body.id;
  await d.save(m);
  await d.s.login(owner, owner.page, m.primary.id);
  current = await d.api(owner.page, base);
  await d.api(
    owner.page,
    base + "/members/" + joined.body.membership_id + "/roles/" + m.auditorRole,
    { method: "PUT", body: {}, headers: { "If-Match": current.etag } },
    204,
  );
  await d.s.login(owner, owner.page, m.primary.id);
  await d.s.login(auditor, auditor.page, m.primary.id);
  phase = "verify-demo-owner-and-delegated-boundaries";
  await verifyActors(m, owner, auditor);
  m.state = "ready";
  m.lastVerifiedAt = new Date().toISOString();
  await d.save(m);
  await d.presenter(m);
  await purgeMail(m);
  console.log(
    "Demo dataset ready. Private login material: .local/demo/presenter.md; storyboard: docs/runbooks/friday-demo.md",
  );
}
async function verifyActors(m, owner, auditor) {
  const base = "/v1/tenants/" + m.primary.id;
  const own = (await d.api(owner.page, base + "/permissions")).body,
    read = (await d.api(auditor.page, base + "/permissions")).body;
  d.assert(own.permissions.includes("roles.manage"));
  for (const permission of ["members.read", "organizations.read", "audit.read"])
    d.assert(read.permissions.includes(permission));
  d.assert(!read.permissions.includes("organizations.write"));
  d.assert(read.denied.includes("organizations.write"));
  const directory = await d.api(owner.page, base + "/organizations");
  for (const name of ["研发中心", "平台组", "产品组", "商务协作"])
    d.assert(directory.body.items.some((item) => item.name === name));
  await d.api(auditor.page, base + "/organizations");
  await d.api(auditor.page, base + "/audit-events");
  await d.api(
    auditor.page,
    base + "/organizations",
    {
      method: "POST",
      body: { name: "必须拒绝的写入", parent_id: m.primary.root_id },
      headers: { "If-Match": directory.etag },
    },
    403,
  );
  await d.api(
    auditor.page,
    "/v1/tenants/" + m.secondary.id + "/organizations",
    {},
    404,
  );
  await d.api(owner.page, "/v1/me/security-events");
}
async function verify(m, rehearse) {
  d.assert(
    m && m.state === "ready" && m.accounts.length === 2,
    "demo dataset not ready",
  );
  await launch();
  phase = "real-password-and-totp-login";
  const [owner, auditor] = await Promise.all([
    d.login(
      browser,
      m,
      m.accounts.find((a) => a.role === "owner"),
      errors,
    ),
    d.login(
      browser,
      m,
      m.accounts.find((a) => a.role === "auditor"),
      errors,
    ),
  ]);
  phase = "verify-organizations-roles-and-isolation";
  await verifyActors(m, owner, auditor);
  phase = "verify-two-application-sso";
  const appA = await d.authorizeApp(
      owner,
      "http://app-a.localhost:3001",
      m.primary.id,
    ),
    appB = await d.authorizeApp(
      owner,
      "http://app-b.localhost:3002",
      m.primary.id,
    );
  await appA.getByRole("button", { name: "续期当前会话", exact: true }).click();
  await appA.getByRole("status").filter({ hasText: "会话已续期" }).waitFor();
  if (rehearse) {
    phase = "rehearse-console-and-playground";
    await d.s.nav(owner.page, "组织");
    await owner.page
      .getByRole("table")
      .getByText("平台组", { exact: true })
      .waitFor();
    await owner.page.screenshot({
      path: d.path.join(d.root, "docs/ui/friday-organizations.png"),
      fullPage: true,
    });
    await d.s.nav(owner.page, "角色与权限");
    await owner.page.getByText("demo_auditor", { exact: true }).waitFor();
    await owner.page.screenshot({
      path: d.path.join(d.root, "docs/ui/friday-roles.png"),
      fullPage: true,
    });
    await d.s.nav(auditor.page, "审计");
    await auditor.page
      .getByRole("heading", { name: "空间审计", exact: true })
      .waitFor();
    d.assert.equal(
      await auditor.page
        .getByRole("button", { name: "创建组织", exact: true })
        .count(),
      0,
    );
    await auditor.page.screenshot({
      path: d.path.join(d.root, "docs/ui/friday-auditor.png"),
      fullPage: true,
    });
    await d.s.nav(owner.page, "API Playground");
    await owner.page
      .getByLabel("选择 API", { exact: true })
      .selectOption("listOrganizations");
    await owner.page
      .getByRole("button", { name: "发送读取请求", exact: true })
      .click();
    await owner.page
      .getByRole("region", { name: "API 响应", exact: true })
      .getByRole("heading", { name: "HTTP 200", exact: true })
      .waitFor();
    await owner.page.screenshot({
      path: d.path.join(d.root, "docs/ui/friday-playground.png"),
      fullPage: true,
    });
    await d.s.nav(auditor.page, "API Playground");
    await auditor.page
      .getByLabel("选择 API", { exact: true })
      .selectOption("listRoles");
    await auditor.page
      .getByRole("button", { name: "发送读取请求", exact: true })
      .click();
    await auditor.page
      .getByRole("region", { name: "API 响应", exact: true })
      .getByRole("heading", { name: "HTTP 403", exact: true })
      .waitFor();
    await d.s.nav(owner.page, "安全会话");
    await owner.page
      .getByRole("region", { name: "个人安全记录", exact: true })
      .getByRole("table")
      .waitFor();
    await owner.page.screenshot({
      path: d.path.join(d.root, "docs/ui/friday-security.png"),
      fullPage: true,
    });
    phase = "rehearse-local-application-logout";
    await appB
      .getByRole("button", { name: "仅退出此应用", exact: true })
      .click();
    await appB
      .getByRole("status")
      .filter({ hasText: "已退出当前应用" })
      .waitFor();
    const me = await appA.evaluate(async () => {
      const r = await fetch("/api/me");
      return r.status;
    });
    d.assert.equal(me, 200);
  }
  d.assert.deepEqual(errors, []);
  m.lastVerifiedAt = new Date().toISOString();
  m.lastVerification = rehearse ? "rehearsal" : "check";
  await d.save(m);
  await d.presenter(m);
  await purgeMail(m);
  const report = {
    verifiedAt: m.lastVerifiedAt,
    result: "passed",
    mode: m.lastVerification,
    realServices: true,
    checks: [
      "password+TOTP",
      "organization data",
      "owner/delegated permissions",
      "cross-space isolation",
      "two-application SSO",
      "refresh",
      ...(rehearse
        ? [
            "console",
            "Playground 200/403",
            "security history",
            "application-only logout",
          ]
        : []),
    ],
  };
  await d.writePrivate(
    "verification.json",
    JSON.stringify(report, null, 2) + "\n",
  );
  console.log(
    "Demo " +
      m.lastVerification +
      " passed: real password/TOTP, two-app SSO, data, role boundaries and isolated spaces. Private credentials retained; no recovery codes consumed.",
  );
}
async function reset(m) {
  if (!m) {
    console.log("No managed demo dataset exists.");
    return;
  }
  phase = "guarded-demo-only-cleanup";
  if (m.accounts.length)
    d.execFileSync(
      "python3",
      [
        "tests/api/cleanup.py",
        "--demo-reset",
        m.runId,
        ...m.accounts.map((a) => a.email),
      ],
      { cwd: d.root, stdio: "pipe" },
    );
  for (const filename of [
    "manifest.json",
    "presenter.md",
    "dataset.md",
    "verification.json",
  ])
    await d.fs.rm(d.path.join(d.directory, filename), { force: true });
  console.log(
    "Only the managed demo identities and their closed set of spaces were removed. Other development data preserved.",
  );
}
(async () => {
  try {
    d.assert(["seed", "check", "rehearse", "reset"].includes(operation));
    await lock();
    const m = await d.load(false);
    if (operation === "seed") await seed(m);
    else if (operation === "reset") await reset(m);
    else await verify(m, operation === "rehearse");
  } finally {
    clearInterval(progress);
    if (browser) await browser.close();
    if (releaseLock) await releaseLock();
  }
})().catch((e) => {
  console.error(
    "Demo command failed: phase=" +
      phase +
      ", error=" +
      e.name +
      " (private details withheld). See docs/runbooks/friday-demo.md; partial data is retained for guarded reset.",
  );
  process.exitCode = 1;
});
