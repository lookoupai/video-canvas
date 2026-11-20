import type { JSONContent } from "@tiptap/core";

/**
 * 故事板提示词模板里的占位符：生成时由前端换成这个片段的真实内容。
 * 名字要和后端 boardPlaceholders（backend/internal/service/drama_settings.go）保持一致。
 */
export const BOARD_PLACEHOLDERS = ["格数", "列数", "行数", "比例", "画面"] as const;

/** 占位符名 */
export type BoardPlaceholder = (typeof BOARD_PLACEHOLDERS)[number];

/** 把名字写成文本里的占位符：双花括号 */
export const formatPlaceholder = (name: string) => `{{${name}}}`;

/** 放逐镜画面的占位符，自定义模板必须保留 */
export const BOARD_SHOTS_PLACEHOLDER = formatPlaceholder("画面");

/** 一个写完整的占位符；括号里不允许再有花括号 */
const PLACEHOLDER_RE = /\{\{([^{}\n]*)\}\}/g;

const KNOWN = new Set<string>(BOARD_PLACEHOLDERS);

/**
 * 检查故事板模板：只能用认识的占位符、花括号要成对、必须保留 {{画面}}。
 * @param body 模板正文
 * @returns 第一个问题的文案；没问题返回 null
 */
export function checkBoardTemplate(body: string): string | null {
  for (const match of body.matchAll(PLACEHOLDER_RE)) {
    if (!KNOWN.has(match[1])) {
      return `不认识的占位符 ${match[0]}，只能用 ${BOARD_PLACEHOLDERS.map(formatPlaceholder).join("、")}`;
    }
  }
  const rest = body.replace(PLACEHOLDER_RE, "");
  if (rest.includes("{{") || rest.includes("}}")) {
    return "占位符没写完整，花括号要成对写成 {{名字}}";
  }
  if (!body.includes(BOARD_SHOTS_PLACEHOLDER)) {
    return `正文必须保留 ${BOARD_SHOTS_PLACEHOLDER}，镜头内容会放在这里`;
  }
  return null;
}

/** 编辑器里占位符节点的类型名 */
export const PLACEHOLDER_NODE = "placeholder";

/** 一行文字转成段落内容：认识的占位符是整体的节点，其他（含不认识的 {{xx}}）都是文字 */
function lineContent(line: string): JSONContent[] {
  const out: JSONContent[] = [];
  let last = 0;
  const pushText = (text: string) => {
    if (!text) return;
    const prev = out[out.length - 1];
    if (prev?.type === "text") prev.text += text;
    else out.push({ type: "text", text });
  };
  for (const match of line.matchAll(PLACEHOLDER_RE)) {
    if (!KNOWN.has(match[1])) continue;
    pushText(line.slice(last, match.index));
    out.push({ type: PLACEHOLDER_NODE, attrs: { name: match[1] } });
    last = match.index + match[0].length;
  }
  pushText(line.slice(last));
  return out;
}

/** 模板文本转成编辑器文档：一行一段，空行是空段 */
export function textToDoc(text: string): JSONContent {
  return {
    type: "doc",
    content: text.split("\n").map((line) => {
      const content = lineContent(line);
      return content.length > 0 ? { type: "paragraph", content } : { type: "paragraph" };
    }),
  };
}

/** 编辑器文档转回模板文本：段落之间换行，占位符写回双花括号 */
export function docToText(doc: JSONContent): string {
  const inline = (node: JSONContent): string => {
    if (node.type === "text") return node.text ?? "";
    if (node.type === "hardBreak") return "\n";
    if (node.type === PLACEHOLDER_NODE) return formatPlaceholder(String(node.attrs?.name ?? ""));
    return (node.content ?? []).map(inline).join("");
  };
  return (doc.content ?? []).map(inline).join("\n");
}
