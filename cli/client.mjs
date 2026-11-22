import { createHash, randomUUID } from "node:crypto";
import { chmod, link, lstat, mkdir, readFile, rename, rm, writeFile } from "node:fs/promises";
import { homedir } from "node:os";
import { dirname, join } from "node:path";

export class CommandError extends Error {
  constructor(exitCode, message, details = {}) {
    super(message);
    this.exitCode = exitCode;
    this.details = details;
  }
}

const configDirectory =
  process.env.VC_CONFIG_DIR ||
  join(process.env.XDG_CONFIG_HOME || join(homedir(), ".config"), "video-canvas");
const sessionPath = join(configDirectory, "session.json");

export function siteUrl(site) {
  let url;
  try {
    url = new URL(site);
  } catch {
    throw new CommandError(2, "需要有效的网站地址");
  }
  const loopback = ["localhost", "127.0.0.1", "[::1]"].includes(url.hostname);
  if (
    (url.protocol !== "https:" && !(url.protocol === "http:" && loopback)) ||
    url.username ||
    url.password ||
    url.search ||
    url.hash ||
    url.pathname !== "/"
  ) {
    throw new CommandError(2, "使用 HTTPS 网站根地址，本机回环地址可用 HTTP");
  }
  return url.origin;
}

async function savePrivate(path, content, publish) {
  await mkdir(dirname(path), { mode: 0o700, recursive: true });
  await chmod(configDirectory, 0o700);
  const temp = `${path}.${randomUUID()}.tmp`;
  await writeFile(temp, content, { mode: 0o600, flag: "wx" });
  try {
    await publish(temp, path);
  } finally {
    await rm(temp, { force: true });
  }
}

export async function storeSession(session) {
  await savePrivate(sessionPath, JSON.stringify(session), rename);
}

export async function clearSession() {
  await rm(sessionPath, { force: true });
}

export async function loadSession() {
  try {
    const file = await lstat(sessionPath);
    if (!file.isFile() || (file.mode & 0o077) !== 0) {
      throw new CommandError(3, "登录文件须为仅当前用户可读写的普通文件，请重新登录");
    }
    const session = JSON.parse(await readFile(sessionPath, "utf8"));
    if (!session.token || !session.username || !(session.expiresAt > Date.now() / 1000)) {
      throw new CommandError(3, "登录已过期，请重新登录");
    }
    return { ...session, site: siteUrl(session.site) };
  } catch (error) {
    if (error.code === "ENOENT" || error instanceof SyntaxError) {
      throw new CommandError(3, "请先运行 vc auth login");
    }
    throw error;
  }
}

function hash(input) {
  return createHash("sha256").update(input).digest("hex");
}

function canonicalJson(value) {
  return JSON.stringify(value, (_, item) =>
    item && typeof item === "object" && !Array.isArray(item)
      ? Object.fromEntries(
          Object.keys(item)
            .sort()
            .map((key) => [key, item[key]]),
        )
      : item,
  );
}

export async function recordRequest(session, method, path, key, body) {
  const namespace = path.startsWith("/drama-segments/") ? "/drama-segments/generate" : path;
  const id = hash(canonicalJson([session.site, session.username, method, namespace, key]));
  const fingerprint = hash(canonicalJson([path, body]));
  const file = join(configDirectory, "requests", id);
  try {
    await savePrivate(file, fingerprint, link);
  } catch (error) {
    if (error.code !== "EEXIST") throw error;
    if ((await readFile(file, "utf8")) !== fingerprint) {
      throw new CommandError(2, "此请求键已用于不同的输入，请为新操作选择新键");
    }
  }
}

export async function call(session, method, path, options = {}) {
  const { json, form, key, signal = AbortSignal.timeout(30_000) } = options;
  const headers = { accept: "application/json" };
  if (session.token) headers.authorization = `Bearer ${session.token}`;
  if (json !== undefined) headers["content-type"] = "application/json";
  if (key) headers["idempotency-key"] = key;
  const body = form ?? (json === undefined ? undefined : JSON.stringify(json));
  let response;
  let payload;
  try {
    response = await fetch(`${session.site}/api/v1${path}`, {
      method,
      headers,
      body,
      signal,
      redirect: "error",
    });
    if (response.status === 204) return null;
    payload = await response.json();
  } catch {
    if (response?.status === 401) throw new CommandError(3, "登录无效或已过期，请重新登录");
    throw new CommandError(4, "未收到有效的平台响应", { http_status: response?.status });
  }
  if (response.status === 401) throw new CommandError(3, "登录无效或已过期，请重新登录");
  if (!response.ok || payload?.code !== 0) {
    const message = session.token
      ? String(payload?.msg || "平台拒绝了请求").replaceAll(session.token, "[已隐藏]")
      : "登录失败";
    throw new CommandError(5, message, {
      http_status: response.status,
      platform_code: payload?.code,
      request_id: payload?.request_id,
    });
  }
  return payload.data ?? null;
}
