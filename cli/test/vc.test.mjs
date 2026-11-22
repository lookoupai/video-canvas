import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { once } from "node:events";
import { mkdtemp, readFile, rm, stat, writeFile } from "node:fs/promises";
import { createServer } from "node:http";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { test } from "node:test";
import { fileURLToPath } from "node:url";

const entry = fileURLToPath(new URL("../vc.mjs", import.meta.url));
const token = "private-test-token";

async function fixture(t, respond) {
  const directory = await mkdtemp(join(tmpdir(), "vc-cli-test-"));
  const requestLog = [];
  const server = createServer(async (req, res) => {
    const chunks = [];
    for await (const chunk of req) chunks.push(chunk);
    const body = Buffer.concat(chunks).toString();
    requestLog.push({ url: req.url, headers: req.headers, body, method: req.method });
    const result =
      req.url === "/api/v1/auth/login"
        ? { data: { token, expire_at: Math.floor(Date.now() / 1000) + 3600 } }
        : respond(requestLog.at(-1), res);
    if (result === null) return;
    res.writeHead(result.status ?? 200, { "content-type": "application/json" });
    res.end(JSON.stringify({ code: 0, ...result }));
  });
  server.listen(0, "127.0.0.1");
  await once(server, "listening");
  t.after(async () => {
    await new Promise((resolve) => server.close(resolve));
    await rm(directory, { force: true, recursive: true });
  });
  return {
    base: `http://127.0.0.1:${server.address().port}`,
    directory,
    requestLog,
    run: async (args, input = "") => {
      const child = spawn(process.execPath, [entry, ...args, "--json"], {
        env: { ...process.env, VC_CONFIG_DIR: directory },
        stdio: ["pipe", "pipe", "pipe"],
      });
      let stdout = "";
      let stderr = "";
      child.stdin.on("error", () => {});
      child.stdin.end(input);
      child.stdout.on("data", (data) => {
        stdout += data;
      });
      child.stderr.on("data", (data) => {
        stderr += data;
      });
      const [code] = await once(child, "close");
      assert.equal((stdout + stderr).includes(token), false, "must never print the token");
      return { code, stdout, stderr };
    },
  };
}

async function login(f) {
  return f.run(["auth", "login", "--site", f.base, "--username", "video"], "test-password\n");
}

test("login keeps the bearer private and uses it for asset and reference requests", async (t) => {
  const f = await fixture(t, (req) => {
    if (req.url === "/api/v1/assets") return { data: { id: 7, kind: "video" } };
    return { data: { id: "b", revision: 4 } };
  });
  assert.equal((await login(f)).code, 0);
  assert.deepEqual(JSON.parse(f.requestLog[0].body), {
    username: "video",
    password: "test-password",
  });
  assert.equal((await stat(join(f.directory, "session.json"))).mode & 0o777, 0o600);
  const media = join(f.directory, "clip.mp4");
  await writeFile(media, "local clip content");
  const upload = await f.run(["asset", "upload", "--file", media]);
  assert.equal(upload.code, 0);
  assert.deepEqual(JSON.parse(upload.stdout), { id: 7, kind: "video" });
  assert.match(f.requestLog[1].headers["content-type"], /^multipart\/form-data; boundary=/);
  assert.match(f.requestLog[1].body, /name="file"; filename="clip.mp4"/);
  assert.match(f.requestLog[1].body, /local clip content/);
  const read = await f.run(["reference", "get", "--project", "a", "--reference", "b"]);
  assert.equal(read.code, 0);
  assert.equal(f.requestLog[2].url, "/api/v1/dramas/a/references/b");
  assert.equal(f.requestLog[2].headers.authorization, `Bearer ${token}`);
});

test("an interrupted mutation preserves its request key and refuses different input", async (t) => {
  let attempts = 0;
  const f = await fixture(t, (req, res) => {
    if (++attempts === 1) {
      res.destroy();
      return null;
    }
    return { data: { id: "c", revision: 1 } };
  });
  assert.equal((await login(f)).code, 0);
  const command = [
    "draft",
    "create",
    "--project",
    "a",
    "--reference",
    "b",
    "--request-key",
    "analysis-1",
    "--input",
    "-",
  ];
  assert.equal((await f.run(command, '{"reference_revision":1}')).code, 4);
  assert.equal(attempts, 1);
  const retry = await f.run(command, '{"reference_revision":1}');
  assert.equal(retry.code, 0);
  assert.deepEqual(JSON.parse(f.requestLog[1].body), {
    reference_revision: 1,
    request_key: "analysis-1",
  });
  assert.equal(f.requestLog[1].body, f.requestLog[2].body);
  assert.equal((await f.run(command, '{"reference_revision":2}')).code, 2);
  assert.equal(attempts, 2);
});

test("task waiting has a deadline and unauthenticated responses give a distinct exit code", async (t) => {
  const f = await fixture(t, (req) => {
    if (req.url.endsWith("/999999")) {
      return { status: 401, code: 10002, msg: token };
    }
    return { data: { id: "69", status: "running" } };
  });
  assert.equal((await login(f)).code, 0);
  const result = await f.run(["task", "wait", "--task", "69", "--timeout", "1"]);
  assert.equal(result.code, 6);
  assert.equal(JSON.parse(result.stderr).resource_id, "69");
  assert.equal(f.requestLog[1].url, "/api/v1/generation-tasks/69");
  assert.equal((await f.run(["task", "get", "--task", "999999"])).code, 3);
  assert.equal((await f.run(["task", "get", "--task", "abc"])).code, 2);
  assert.ok(JSON.parse(await readFile(join(f.directory, "session.json"), "utf8")).token);
});
