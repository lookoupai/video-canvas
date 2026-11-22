#!/usr/bin/env node

import { openAsBlob } from "node:fs";
import { readFile } from "node:fs/promises";
import { basename } from "node:path";
import { setTimeout as wait } from "node:timers/promises";
import { parseArgs } from "node:util";
import {
  call,
  clearSession,
  CommandError,
  loadSession,
  recordRequest,
  siteUrl,
  storeSession,
} from "./client.mjs";

const terminalStatuses = new Set(["succeeded", "failed", "canceled", "expired"]);
const apiOptions = {
  project: { type: "string" },
  reference: { type: "string" },
  preparation: { type: "string" },
  draft: { type: "string" },
  segment: { type: "string" },
  task: { type: "string" },
  episode: { type: "string" },
  revision: { type: "string" },
  input: { type: "string" },
  file: { type: "string" },
  cursor: { type: "string" },
  limit: { type: "string" },
  "shot-id": { type: "string" },
  "request-key": { type: "string" },
  site: { type: "string" },
  username: { type: "string" },
  timeout: { type: "string" },
  json: { type: "boolean" },
  help: { type: "boolean", short: "h" },
};

function required(flags, name) {
  const value = flags[name]?.trim();
  if (!value) throw new CommandError(2, `缺少 --${name}`);
  return value;
}

function id(flags, name) {
  const value = required(flags, name);
  if (!/^[a-z\d]+$/i.test(value)) throw new CommandError(2, `--${name} 格式无效`);
  return value;
}

function taskId(flags) {
  const value = required(flags, "task");
  if (!/^(?:[1-9]\d{0,19}|[a-f0-9]{32})$/.test(value)) {
    throw new CommandError(2, "--task 须为有效的平台任务编号");
  }
  return value;
}

function number(flags, name, fallback, max = Number.MAX_SAFE_INTEGER) {
  const value = flags[name] ?? fallback;
  const integer = Number(value);
  if (
    !/^\d+$/.test(String(value)) ||
    !Number.isSafeInteger(integer) ||
    integer < 1 ||
    integer > max
  ) {
    throw new CommandError(2, `--${name} 须为 1 到 ${max} 的整数`);
  }
  return integer;
}

const project = (flags) => `/dramas/${id(flags, "project")}`;
const references = (flags) => `${project(flags)}/references`;
const reference = (flags) => `${references(flags)}/${id(flags, "reference")}`;
const drafts = (flags) => `${reference(flags)}/drafts`;
const draft = (flags) => `${drafts(flags)}/${id(flags, "draft")}`;
const preparations = (flags) => `${reference(flags)}/preparations`;
const preparation = (flags) => `${preparations(flags)}/${id(flags, "preparation")}`;

const routes = {
  "project list": { path: () => "/dramas" },
  "project get": { path: project, flags: "project" },
  "project create": {
    path: () => "/drama-reference-projects",
    method: "POST",
    input: true,
    key: "body",
  },
  "asset upload": { path: () => "/assets", method: "POST", flags: "file", form: "file" },
  "reference list": { path: references, flags: "project" },
  "reference add": { path: references, flags: "project", method: "POST", input: true, key: "body" },
  "reference get": { path: reference, flags: "project reference revision", query: ["revision"] },
  "reference import": {
    path: (flags) => `${reference(flags)}/import`,
    flags: "project reference file revision",
    method: "POST",
    form: "package",
  },
  "reference prepare": {
    path: preparations,
    flags: "project reference",
    method: "POST",
    input: true,
    key: "body",
  },
  "reference status": { path: preparation, flags: "project reference preparation" },
  "reference frames": {
    path: (flags) => `${preparation(flags)}/frames`,
    flags: "project reference preparation cursor limit shot-id",
    query: ["cursor", "limit", "shot-id"],
  },
  "draft list": {
    path: drafts,
    flags: "project reference limit cursor",
    query: ["limit", "cursor"],
  },
  "draft create": {
    path: drafts,
    flags: "project reference",
    method: "POST",
    input: true,
    key: "body",
  },
  "draft get": { path: draft, flags: "project reference draft" },
  "draft update": { path: draft, flags: "project reference draft", method: "PATCH", input: true },
  "draft quote": {
    path: (flags) => `${draft(flags)}/quote`,
    flags: "project reference draft",
    method: "POST",
    input: true,
  },
  "draft apply": {
    path: (flags) => `${draft(flags)}/apply`,
    flags: "project reference draft",
    method: "POST",
    input: true,
    key: "body",
  },
  "draft generate": {
    path: (flags) => `${draft(flags)}/generate`,
    flags: "project reference draft",
    method: "POST",
    input: true,
    key: "body",
  },
  "segment generate": {
    path: (flags) => `/drama-segments/${id(flags, "segment")}/generate`,
    flags: "segment",
    method: "POST",
    input: true,
    key: "header",
  },
  "episode compose": {
    path: (flags) => `${project(flags)}/jobs`,
    flags: "project episode",
    method: "POST",
    json: (flags) => ({ step: "compose", episodes: [number(flags, "episode", undefined, 100000)] }),
  },
  "episode jobs": { path: (flags) => `${project(flags)}/jobs`, flags: "project" },
};

const customFlags = {
  "auth login": "site username",
  "auth status": "",
  "auth logout": "",
  "task get": "task",
  "task wait": "task timeout",
};

async function readStdin(maxBytes) {
  const chunks = [];
  let length = 0;
  for await (const chunk of process.stdin) {
    length += chunk.length;
    if (length > maxBytes) throw new CommandError(2, "标准输入超过大小上限");
    chunks.push(chunk);
  }
  return Buffer.concat(chunks).toString("utf8");
}

async function readInput(path) {
  let input;
  try {
    const raw = path === "-" ? await readStdin(2 << 20) : await readFile(path, "utf8");
    input = JSON.parse(raw);
  } catch (error) {
    if (error instanceof CommandError) throw error;
    throw new CommandError(2, "--input 需要可读取的 JSON 对象文件，或用 - 从标准输入读取");
  }
  if (input === null || typeof input !== "object" || Array.isArray(input)) {
    throw new CommandError(2, "--input 必须是 JSON 对象");
  }
  return input;
}

async function login(flags) {
  const site = siteUrl(required(flags, "site"));
  const username = required(flags, "username");
  if (process.stdin.isTTY) throw new CommandError(2, "请通过标准输入传入密码");
  const password = (await readStdin(1024)).replace(/\r?\n$/, "");
  if (!password) throw new CommandError(2, "标准输入缺少密码");
  const result = await call({ site }, "POST", "/auth/login", { json: { username, password } });
  if (
    typeof result?.token !== "string" ||
    !result.token ||
    !(result.expire_at > Date.now() / 1000)
  ) {
    throw new CommandError(4, "平台未返回有效的登录凭据");
  }
  await storeSession({ site, username, token: result.token, expiresAt: result.expire_at });
  return { site, username, expire_at: result.expire_at };
}

async function getTask(session, taskId, signal) {
  if (taskId.length < 32) {
    return call(session, "GET", `/generation-tasks/${taskId}`, { signal });
  }
  const rows = await call(session, "GET", `/generation-tasks?ids=${taskId}`, { signal });
  if (!rows?.length) throw new CommandError(5, "未找到任务", { resource_id: taskId });
  return rows[0];
}

async function waitForTask(session, flags) {
  const timeout = number(flags, "timeout", 300, 86400);
  const deadline = AbortSignal.timeout(timeout * 1000);
  const resourceID = taskId(flags);
  try {
    while (!deadline.aborted) {
      const row = await getTask(session, resourceID, deadline);
      if (terminalStatuses.has(row?.status)) return row;
      await wait(1000, undefined, { signal: deadline });
    }
  } catch (error) {
    if (!deadline.aborted) throw error;
  }
  throw new CommandError(6, "等待超时，可使用原任务 ID 继续查询", { resource_id: resourceID });
}

async function fileForm(flags, field) {
  const path = required(flags, "file");
  const form = new FormData();
  try {
    form.set(field, await openAsBlob(path), basename(path));
  } catch {
    throw new CommandError(2, "无法读取待上传文件");
  }
  if (field === "package") form.set("revision", String(number(flags, "revision")));
  return form;
}

function queryPath(route, flags) {
  const path = route.path(flags);
  const query = new URLSearchParams();
  for (const key of route.query ?? []) {
    if (flags[key] !== undefined) query.set(key.replaceAll("-", "_"), flags[key]);
  }
  return query.size ? `${path}?${query}` : path;
}

async function runRoute(session, route, flags) {
  const path = queryPath(route, flags);
  const method = route.method ?? "GET";
  const options = {};
  if (route.input) options.json = await readInput(required(flags, "input"));
  if (route.json) options.json = route.json(flags);
  if (route.form) {
    options.form = await fileForm(flags, route.form);
    options.signal = AbortSignal.timeout(300_000);
  }
  let requestKey;
  if (route.key) {
    requestKey = required(flags, "request-key");
    if (!/^[!-~]{1,128}$/.test(requestKey))
      throw new CommandError(2, "--request-key 须为 1 到 128 个可见 ASCII 字符");
    if (route.key === "body") options.json.request_key = requestKey;
    else options.key = requestKey;
    await recordRequest(session, method, path, requestKey, options.json);
  }
  try {
    return await call(session, method, path, options);
  } catch (error) {
    if (requestKey && error instanceof CommandError) error.details.request_key = requestKey;
    throw error;
  }
}

function validateOptions(flags, route, command) {
  const allowed = (route?.flags ?? customFlags[command] ?? "").split(" ");
  if (route?.input) allowed.push("input");
  if (route?.key) allowed.push("request-key");
  for (const name of Object.keys(flags)) {
    if (name !== "json" && !allowed.includes(name)) {
      throw new CommandError(2, `未知参数 --${name}（${command}）`);
    }
  }
}

function showHelp() {
  process.stdout.write(
    [
      "用法 vc <命令> [参数] [--json]",
      "auth login --site URL --username NAME < password",
      "auth status / auth logout",
      "task get --task ID",
      "task wait --task ID [--timeout 秒]",
      ...Object.entries(routes).map(([name, route]) =>
        [
          name,
          ...(route.flags || "")
            .split(" ")
            .filter(Boolean)
            .map((flag) => `--${flag}`),
          ...(route.input ? ["--input FILE|-"] : []),
          ...(route.key ? ["--request-key KEY"] : []),
          ...(name.endsWith(" generate") ? ["（提交可计费任务）"] : []),
        ].join(" "),
      ),
      "",
    ].join("\n"),
  );
}

async function run({ positionals, values: flags }) {
  if (flags.help || positionals.length === 0) {
    showHelp();
    return;
  }
  const command = positionals.join(" ");
  const route = Object.hasOwn(routes, command) ? routes[command] : undefined;
  if (!route && !Object.hasOwn(customFlags, command))
    throw new CommandError(2, "未知命令，运行 vc --help");
  validateOptions(flags, route, command);
  if (command === "auth login") return login(flags);
  if (command === "auth logout") {
    await clearSession();
    return { signed_out: true };
  }
  const session = await loadSession();
  if (command === "auth status") {
    return { site: session.site, username: session.username, expire_at: session.expiresAt };
  }
  if (command === "task get") return getTask(session, taskId(flags));
  if (command === "task wait") {
    const row = await waitForTask(session, flags);
    if (row.status !== "succeeded") process.exitCode = 7;
    return row;
  }
  return runRoute(session, route, flags);
}

try {
  const args = parseArgs({ options: apiOptions, allowPositionals: true });
  const result = await run(args);
  if (result !== undefined)
    process.stdout.write(`${JSON.stringify(result, null, args.values.json ? 0 : 2)}\n`);
} catch (error) {
  const usage = String(error.code).startsWith("ERR_PARSE_ARGS");
  const known = error instanceof CommandError;
  process.exitCode = known ? error.exitCode : usage ? 2 : 1;
  const message = known
    ? error.message
    : usage
      ? "参数格式无效，运行 vc --help"
      : "无法完成本地文件操作";
  if (process.argv.includes("--json")) {
    process.stderr.write(
      `${JSON.stringify({ error: message, ...(known ? error.details : {}) })}\n`,
    );
  } else {
    process.stderr.write(`${message}\n`);
  }
}
