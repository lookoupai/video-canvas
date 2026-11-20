/**
 * Agnes AI 视频插件（内置，契约 v1）。
 *
 * 对接 Agnes AI 的 OpenAI Videos 兼容接口（国际站渠道 base_url 填 https://apihub.agnes-ai.com，不带路径；
 *   国内站填 https://api.agnes-ai.cn）：
 *   - 提交：POST /v1/videos，JSON 体 { model, prompt, seconds, mode, size, aspect_ratio, ... }
 *   - 轮询：GET  /agnesapi?video_id=<id>&model_name=<model>（注意：轮询端点不在 /v1 下，在站点根路径）
 *   - 完成：轮询响应 status=completed 时顶层 url 直接给出 mp4 地址，宿主下载落库（不走 /content 端点）
 *
 * 模型（upstreamModel）：
 *   agnes-video-2.5-flash  免费；size 固定 "720P"；images ≤5、audios ≤3、不支持 videos
 *   agnes-video-2.5        付费；size 可为 "720P"/"1080P" 等；支持图片/音频/视频参考
 *
 * 鉴权：bearer，渠道 Key（sk-...）由宿主按 Authorization: Bearer 注入，无需插件构造。
 *
 * 三种生成模式（mode）：
 *   text      纯文生视频：无参考素材
 *   keyframe  首尾帧控制：first_frame / last_frame 至少一个（图片引用）
 *   reference 参考生成：images 或 audios 非空（2.5 还可带 videos）
 *
 * 用户输入（宿主按模型 capabilities 校验后传入）：
 *   prompt、duration（秒，agnes 要字符串 "4"-"12"，默认 "5"）、aspect_ratio（默认 16:9）、
 *   resolution / size（flash 固定 720P）、images / audios / videos（文件引用，宿主按渠道设置转换）、seed、n
 *
 * 写法说明：goja 只保证 ES5.1 + 部分 ES6，所以用 var / function，不用可选链与空值合并。
 */

module.exports = {
  meta: {
    apiVersion: 1,
    key: "agnes-video",
    name: "Agnes AI · 视频",
    version: "1.0.1",
    vendor: "agnes",
    description: "Agnes AI 视频生成：文生视频、首尾帧、图片/音频/视频参考（异步轮询，完成后按 url 取片）",
    auth: { type: "bearer" },
    channelSettings: {
      as: {
        type: "enum",
        label: "素材传输格式",
        description: "选择图片、视频和音频引用的传输格式，默认使用存储 URL。",
        options: ["url", "dataUrl", "base64"],
        required: true,
        default: "url",
      },
    },
    endpoints: {
      video: { mode: "async", method: "POST", path: "/v1/videos" },
    },
    poll: { firstDelay: 5, interval: 3, maxInterval: 10, jitter: 0.2 },
  },

  buildSubmitRequest: function (ctx) {
    var referenceFormat = (ctx.channel && ctx.channel.settings && ctx.channel.settings.as) || "url";
    var input = ctx.input || {};
    var model = ctx.model.upstreamModel;
    var images = isArray(input.images) ? input.images : [];
    var audios = isArray(input.audios) ? input.audios : [];
    var videos = isArray(input.videos) ? input.videos : [];
    var mode = resolveMode(input, images, audios, videos);

    var body = { model: model };
    if (input.prompt !== undefined && input.prompt !== "") {
      body.prompt = String(input.prompt);
    }
    body.mode = mode;

    // seconds 必须是字符串 "4"-"12"，默认 "5"
    var seconds = pick(input.duration, input.seconds);
    body.seconds = seconds === undefined || seconds === null || seconds === "" ? "5" : String(seconds);

    // size：flash 固定 720P；其它模型用用户给的 resolution / size，缺省 720P
    body.size = resolveSize(model, input);

    var ratio = firstString(input.aspect_ratio, input.ratio);
    if (ratio) {
      body.aspect_ratio = ratio;
    }
    if (typeof input.seed === "number") {
      body.seed = input.seed;
    }
    if (typeof input.n === "number" && input.n > 0) {
      body.n = input.n;
    }

    if (mode === "keyframe") {
      if (images.length > 0) {
        body.first_frame = { __fileRef: images[0], as: referenceFormat };
      }
      if (images.length > 1) {
        body.last_frame = { __fileRef: images[1], as: referenceFormat };
      }
    } else if (mode === "reference") {
      if (images.length > 0) {
        body.images = fileRefs(images, referenceFormat);
      }
      if (audios.length > 0) {
        body.audios = fileRefs(audios, referenceFormat);
      }
      if (videos.length > 0) {
        body.videos = fileRefs(videos, referenceFormat);
      }
    }

    return { method: "POST", path: "/v1/videos", json: body, timeout: 60 };
  },

  parseSubmitResponse: function (ctx, resp) {
    var body = resp.body || {};
    var id = pick(body.video_id, body.id, body.task_id);
    if (id === undefined) {
      var msg = errorMessageOf(body) || detailOf(body) || (resp.text || "").slice(0, 300);
      if (msg) {
        return { immediate: failed(msg) };
      }
      throw new Error("Agnes 上游没有返回 video_id");
    }
    return { providerTaskId: String(id), state: { model: ctx.model.upstreamModel } };
  },

  buildQueryRequest: function (ctx) {
    var model = (ctx.task && ctx.task.state && ctx.task.state.model) || ctx.model.upstreamModel;
    return {
      method: "GET",
      path: "/agnesapi",
      query: { video_id: ctx.task.providerTaskId, model_name: model },
      timeout: 30,
    };
  },

  parseQueryResponse: function (ctx, resp) {
    var body = resp.body || {};
    var status = normalizeStatus(body.status);
    if (status === "failed") {
      return failed(errorMessageOf(body) || detailOf(body) || "Agnes 任务失败");
    }
    if (status === "succeeded") {
      var url = typeof body.url === "string" ? body.url : "";
      if (!url) {
        return failed("Agnes 返回成功，但没有视频地址");
      }
      return {
        status: "succeeded",
        progress: 100,
        outputs: [{ type: "url", url: url, media_type: "video", mime: "video/mp4" }],
      };
    }
    var out = { status: status };
    var progress = parseProgress(body.progress);
    if (progress !== undefined) {
      out.progress = Math.min(progress, 99);
    }
    return out;
  },

  // 非 2xx：agnes 错误体常见 {error:{message}} / {detail} + 文案。
  classifyError: function (ctx, resp) {
    var body = resp.body && typeof resp.body === "object" ? resp.body : {};
    var msg = errorMessageOf(body) || detailOf(body) || (resp.text || "").slice(0, 300);
    if (!msg) {
      return null;
    }
    var cls = classifyOf(msg);
    if (cls === "terminal" && resp.status !== 401 && resp.status !== 403) {
      return null; // 429 / 5xx / 其它交给宿主默认规则
    }
    return { class: cls, message: msg };
  },

  buildCheckRequest: function (ctx) {
    return { method: "GET", path: "/v1/models", timeout: 15 };
  },
};

// 推断 mode：显式 mode > 宿主 op > 素材。
//   t2v / 无素材       -> text
//   i2v / 首帧或尾帧     -> keyframe（1 图首帧，2 图首尾帧）
//   omni / 图片+音视频参考 -> reference
function resolveMode(input, images, audios, videos) {
  var explicit = firstString(input.mode);
  if (explicit === "text" || explicit === "keyframe" || explicit === "reference") {
    return explicit;
  }
  var op = firstString(input.op);
  if (op === "t2v") {
    return "text";
  }
  if (op === "i2v" || input.first_frame || input.last_frame) {
    return "keyframe";
  }
  if (op === "omni" || images.length > 0 || audios.length > 0 || videos.length > 0) {
    return "reference";
  }
  return "text";
}

// flash 固定 720P；其它模型用 resolution（若已是 720P/1080P 之类）或 size。
function resolveSize(model, input) {
  var isFlash = /-flash$/i.test(String(model || ""));
  if (isFlash) {
    return "720P";
  }
  var size = firstString(input.size, input.resolution);
  return size || "720P";
}

function normalizeStatus(raw) {
  switch (String(raw === undefined || raw === null ? "" : raw).toLowerCase()) {
    case "completed":
    case "succeeded":
    case "success":
      return "succeeded";
    case "failed":
    case "cancelled":
    case "canceled":
    case "expired":
      return "failed";
    case "in_progress":
    case "processing":
    case "running":
      return "running";
    default:
      return "queued"; // pending / queued / 未知：继续等
  }
}

function classifyOf(msg) {
  var s = String(msg || "").toLowerCase();
  if (/moderation|content[ _-]?policy|violat|safety|sensitive|违规|审核|敏感/.test(s)) {
    return "moderation";
  }
  if (/insufficient|billing|balance|quota|credit|arrears|余额|额度|欠费/.test(s)) {
    return "provider_balance";
  }
  if (/rate.?limit|too many|429|limit exceed/.test(s)) {
    return "retryable";
  }
  return "terminal";
}

function failed(message) {
  return { status: "failed", error: { class: classifyOf(message), message: message } };
}

function errorMessageOf(body) {
  if (!body || typeof body !== "object") {
    return "";
  }
  var e = body.error;
  if (e && typeof e === "object" && typeof e.message === "string" && e.message) {
    return e.message;
  }
  if (typeof e === "string" && e) {
    return e;
  }
  return "";
}

function detailOf(body) {
  if (!body || typeof body !== "object") {
    return "";
  }
  return typeof body.detail === "string" ? body.detail : "";
}

function parseProgress(v) {
  if (typeof v === "number") {
    return Math.max(0, v);
  }
  if (typeof v === "string") {
    var n = parseFloat(v);
    return isNaN(n) ? undefined : Math.max(0, n);
  }
  return undefined;
}

function fileRefs(list, referenceFormat) {
  var out = [];
  for (var i = 0; i < list.length; i++) {
    out.push({ __fileRef: list[i], as: referenceFormat });
  }
  return out;
}

function firstString() {
  for (var i = 0; i < arguments.length; i++) {
    if (typeof arguments[i] === "string" && arguments[i] !== "") {
      return arguments[i];
    }
  }
  return "";
}

function pick() {
  for (var i = 0; i < arguments.length; i++) {
    var v = arguments[i];
    if (v !== undefined && v !== null && v !== "") {
      return v;
    }
  }
  return undefined;
}

function isArray(v) {
  return Object.prototype.toString.call(v) === "[object Array]";
}
