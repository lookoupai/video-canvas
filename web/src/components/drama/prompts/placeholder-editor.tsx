import { useEffect } from "react";
import { Plus } from "lucide-react";
import { Node as TiptapNode } from "@tiptap/core";
import Document from "@tiptap/extension-document";
import HardBreak from "@tiptap/extension-hard-break";
import Paragraph from "@tiptap/extension-paragraph";
import Text from "@tiptap/extension-text";
import { UndoRedo } from "@tiptap/extensions";
import { EditorContent, useEditor } from "@tiptap/react";

import { cn } from "@/lib/utils";
import {
  BOARD_PLACEHOLDERS,
  PLACEHOLDER_NODE,
  docToText,
  textToDoc,
} from "@/utils/drama/placeholders";

/**
 * 占位符在编辑器里是整体的一块（像 @ 引用的 chip）：光标进不去、字改不了，退格一下整块删掉；
 * 存回文本时写成 `{{名字}}`。
 */
const PlaceholderNode = TiptapNode.create({
  name: PLACEHOLDER_NODE,
  group: "inline",
  inline: true,
  atom: true,
  selectable: true,
  addAttributes() {
    return { name: { default: "" } };
  },
  parseHTML() {
    return [
      {
        tag: "span[data-placeholder]",
        getAttrs: (el) => ({ name: (el as HTMLElement).getAttribute("data-placeholder") ?? "" }),
      },
    ];
  },
  renderHTML({ node }) {
    return [
      "span",
      {
        "data-placeholder": String(node.attrs.name),
        contenteditable: "false",
        class:
          "bg-chrome ring-chrome-border mx-0.5 inline-flex items-center rounded-full px-2 py-px align-baseline font-sans text-[12.5px] font-medium ring-1 select-none [&.ProseMirror-selectednode]:ring-2 [&.ProseMirror-selectednode]:ring-ring",
      },
      String(node.attrs.name),
    ];
  },
});

const EXTENSIONS = [Document, Paragraph, Text, HardBreak, UndoRedo, PlaceholderNode];

/**
 * 带占位符的提示词编辑器（故事板模板用）：上面一排按钮往光标处插占位符，占位符整体不可编辑。
 * 值始终是纯文本，占位符写成 `{{名字}}`；不认识的 `{{xx}}` 在编辑器里就是普通文字，由保存前的检查拦下。
 */
export function PlaceholderEditor({
  value,
  onChange,
  ariaLabel,
  className,
}: {
  /** 模板正文 */
  value: string;
  onChange: (text: string) => void;
  /** 编辑区的无障碍名称 */
  ariaLabel: string;
  /** 编辑区外框的样式（高度、背景等） */
  className?: string;
}) {
  const editor = useEditor({
    extensions: EXTENSIONS,
    content: textToDoc(value),
    shouldRerenderOnTransaction: false,
    editorProps: {
      attributes: {
        role: "textbox",
        "aria-multiline": "true",
        "aria-label": ariaLabel,
        class: "min-h-full whitespace-pre-wrap break-words outline-none",
      },
    },
    onUpdate: ({ editor: self }) => onChange(docToText(self.getJSON())),
  });

  // 外部改了正文（恢复内置等）：内容对不上才重灌，自己打字回传的不动，免得光标乱跳
  useEffect(() => {
    if (!editor || docToText(editor.getJSON()) === value) return;
    editor.commands.setContent(textToDoc(value), { emitUpdate: false });
  }, [editor, value]);

  return (
    <div className="grid gap-2">
      <div className="flex flex-wrap items-center gap-1.5" data-slot="placeholder-toolbar">
        {BOARD_PLACEHOLDERS.map((name) => (
          <button
            key={name}
            type="button"
            aria-label={`插入占位符 ${name}`}
            // 按下时不抢走编辑区的焦点和光标，插入才落在光标处
            onMouseDown={(event) => event.preventDefault()}
            onClick={() =>
              editor
                ?.chain()
                .focus()
                .insertContent({ type: PLACEHOLDER_NODE, attrs: { name } })
                .run()
            }
            className="bg-chrome-hover ring-chrome-border hover:bg-chrome focus-visible:ring-ring/50 inline-flex h-7 items-center gap-1 rounded-full px-3 text-[12.5px] ring-1 outline-none focus-visible:ring-2"
          >
            <Plus className="size-3" />
            {name}
          </button>
        ))}
      </div>
      <EditorContent
        editor={editor}
        className={cn(
          "bg-chrome-hover border-chrome-border cursor-text overflow-y-auto rounded-2xl border px-4 py-3 font-mono text-[13px] leading-relaxed",
          className,
        )}
        onClick={() => editor?.commands.focus()}
      />
    </div>
  );
}
