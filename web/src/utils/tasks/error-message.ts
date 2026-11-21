/** 渠道没有可读失败原因时使用的提示，不把协议正文展示给创作者。 */
const UNKNOWN_ERROR = "生成失败，请稍后重试或联系管理员。";

/** 上游额度与站内积分不同，提示用户处理模型渠道。 */
const CHANNEL_QUOTA = "模型渠道余额不足，请补充渠道额度或更换渠道。";

/**
 * 把历史任务中的上游错误转换为可读文案；兼容网关把 JSON 再编码进 message 的情况。
 * 原始错误仍留在任务记录中供排查，解析层数受限，损坏或无文案的 JSON 使用通用提示。
 * @param message 任务保存的失败原因
 * @returns 页面展示的失败原因
 */
export function taskErrorMessage(message: string | null | undefined): string {
  let value: unknown = message;
  for (let depth = 0; depth < 8; depth++) {
    if (typeof value === "string") {
      const text = value.trim();
      if (!text) return "生成失败";
      if (/^[{["]/.test(text)) {
        try {
          value = JSON.parse(text);
          continue;
        } catch {
          return UNKNOWN_ERROR;
        }
      }
      return /insufficient[ _]+(?:user[ _]+)?(?:quota|balance|credits?)/i.test(text)
        ? CHANNEL_QUOTA
        : text;
    }
    if (!value || typeof value !== "object" || Array.isArray(value)) return UNKNOWN_ERROR;
    const error = value as Record<string, unknown>;
    if (error.code === "insufficient_user_quota" || error.code === "insufficient_quota") {
      return CHANNEL_QUOTA;
    }
    value = error.message || error.error || error.detail;
  }
  return UNKNOWN_ERROR;
}
