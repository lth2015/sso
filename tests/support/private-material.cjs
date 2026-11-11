// Check only caller-owned ephemeral material. Never print bytes or raw failures.
const { execFileSync } = require("node:child_process"),
  fs = require("node:fs"),
  path = require("node:path");
module.exports = function privateMaterialAbsent(root, values, since) {
  const privateValues = values
    .filter((v) => typeof v === "string" && v.length >= 32)
    .map((v) => Buffer.from(v));
  const scan = (data) => {
    if (privateValues.some((value) => data.includes(value)))
      throw new Error("private material detected (details withheld)");
  };
  const files = execFileSync(
    "git",
    ["ls-files", "--cached", "--others", "--exclude-standard", "-z"],
    { cwd: root, stdio: ["ignore", "pipe", "pipe"] },
  )
    .toString()
    .split("\0")
    .filter(Boolean);
  let checked = 0;
  for (const file of files) {
    const location = path.join(root, file);
    if (fs.existsSync(location) && fs.statSync(location).isFile()) {
      scan(fs.readFileSync(location));
      checked++;
    }
  }
  const logs = execFileSync(
    "docker",
    [
      "compose",
      "--env-file",
      ".local/m0.env",
      "-f",
      "deploy/local/compose.yml",
      "logs",
      "--no-color",
      "--since",
      since,
      "identity-api",
      "platform-api",
      "hydra-login",
      "console",
    ],
    {
      cwd: root,
      stdio: ["ignore", "pipe", "pipe"],
      maxBuffer: 16 * 1024 * 1024,
    },
  );
  scan(logs);
  return { files: checked, log_bytes: logs.length };
};
