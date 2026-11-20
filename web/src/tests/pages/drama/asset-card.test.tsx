import { describe, expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";

import type { CanvasNodeData } from "@/types/canvas";
import { AddAssetCard, AssetCard } from "@/pages/drama/workspace/asset-card";
import type { AssetItem } from "@/utils/drama/assets";
import type { VirtualHumanDto } from "@/api/virtual-human/type";

const item = (overrides: Partial<AssetItem> = {}): AssetItem => ({
  id: "n1",
  type: "character",
  name: "林薇",
  look: "",
  label: "林薇",
  episodes: [1],
  status: "idle",
  data: { kind: "character" } as CanvasNodeData,
  ...overrides,
});

const noop = () => {};
const card = (
  value: AssetItem,
  multi = false,
  enroll?: { human: VirtualHumanDto | null; onEnroll: () => void },
) =>
  renderToStaticMarkup(
    <AssetCard
      item={value}
      index={0}
      multi={multi}
      selected={false}
      onToggle={noop}
      onLocate={noop}
      onUpload={noop}
      onPickLibrary={noop}
      enroll={enroll}
    />,
  );

describe("AssetCard：短剧资产卡", () => {
  test("整张卡就是进画布的入口，没有悬停的「生成」「画布」按钮", () => {
    const out = card(item());
    expect(out).toContain('aria-label="在画布中打开 林薇"');
    expect(out).not.toContain(">生成<");
    expect(out).not.toContain(">画布<");
  });

  test("生成失败也不出「重试」按钮", () => {
    expect(card(item({ status: "error" }))).not.toContain(">重试<");
  });

  test("封面图完整显示不裁切：16:9 的三视图放进 3:4 的卡片里也能看全", () => {
    const out = card(
      item({
        status: "done",
        data: { kind: "character", src: "https://cdn/sheet.png" } as CanvasNodeData,
      }),
    );
    expect(out).toContain("object-contain");
    expect(out).not.toContain("object-cover");
  });

  test.each(["character", "image"] as const)("%s 资产封面只加载缩略图", (kind) => {
    const src = "/files/assets/sheet.png";
    const out = card(item({ status: "done", data: { kind, src } as CanvasNodeData }));
    expect(out).toContain(`src="${src}?v=thumb"`);
    expect(out).not.toContain(`src="${src}"`);
    expect(out).toContain('data-slot="media-skeleton"');
  });

  test("多选模式下点卡片是勾选，不进画布", () => {
    const out = card(item(), true);
    expect(out).toContain('aria-label="选择"');
    expect(out).not.toContain("在画布中打开");
  });
});

describe("AssetCard：往空卡里上传", () => {
  test("空的角色卡直接给「上传」「素材库」两个按钮", () => {
    const out = card(item());
    expect(out).toContain("待生成形象");
    expect(out).toContain(">上传<");
    expect(out).toContain(">素材库<");
  });

  test.each(["scene", "prop", "image"] as const)("空的 %s 卡也能上传", (type) => {
    const out = card(item({ type, data: { kind: "image" } as CanvasNodeData }));
    expect(out).toContain(">上传<");
  });

  test("已经生成过的卡不给上传，也不给替换", () => {
    const out = card(
      item({
        status: "done",
        data: { kind: "character", src: "https://cdn/a.png" } as CanvasNodeData,
      }),
    );
    expect(out).not.toContain(">上传<");
    expect(out).not.toContain("替换");
  });

  test("多选模式下不出上传按钮，免得和勾选抢点击", () => {
    expect(card(item(), true)).not.toContain(">上传<");
  });

  test("文本卡没有上传", () => {
    const out = card(item({ type: "text", data: { kind: "script" } as CanvasNodeData }));
    expect(out).not.toContain(">上传<");
  });

  test("上传中显示进度，不说「生成中」", () => {
    const out = card(
      item({
        status: "uploading",
        data: { kind: "image", status: "running", uploadProgress: 40 } as CanvasNodeData,
      }),
    );
    expect(out).toContain("上传中");
    expect(out).toContain("40%");
    expect(out).not.toContain("生成中");
  });

  test("上传的场景卡没有「提示词与生成设置」铅笔，左上角标「上传」", () => {
    const out = card(
      item({
        type: "scene",
        status: "done",
        data: { kind: "image", uploaded: true, src: "https://cdn/a.png" } as CanvasNodeData,
      }),
    );
    expect(out).not.toContain("的提示词");
    expect(out).toContain(">上传</span>");
  });

  test("上传失败写「上传失败」，不写生成失败", () => {
    const out = card(
      item({
        type: "image",
        status: "error",
        data: { kind: "image", uploaded: true, status: "error" } as CanvasNodeData,
      }),
    );
    expect(out).toContain("上传失败");
    expect(out).not.toContain("生成失败");
  });
});

describe("AssetCard：上传形象的角色卡", () => {
  test("不出「提示词与生成设置」铅笔，只留角色详情", () => {
    const out = card(
      item({
        status: "done",
        data: {
          kind: "character",
          src: "https://cdn/a1.png",
          outputs: [
            {
              id: "a1",
              src: "https://cdn/a1.png",
              mediaType: "image",
              assetId: "a1",
              createdAt: 1,
            },
          ],
          activeOutputId: "a1",
        } as CanvasNodeData,
      }),
    );
    expect(out).not.toContain("的提示词");
    expect(out).toContain("角色详情与音色");
  });
});

describe("AddAssetCard：新建卡", () => {
  const add = (type: Parameters<typeof AddAssetCard>[0]["type"]) =>
    renderToStaticMarkup(<AddAssetCard type={type} scope="出现在第 1 集" onCreate={noop} />);

  test("只有新建，没有上传入口", () => {
    const out = add("character");
    expect(out).toContain("引入角色卡");
    expect(out).toContain("出现在第 1 集");
    expect(out).not.toContain("上传");
  });

  test("其他类型写「新建某类」", () => {
    expect(add("scene")).toContain("新建场景");
    expect(add("image")).toContain("新建图片");
  });
});

describe("AssetCard：加入真人库", () => {
  const done = item({
    status: "done",
    data: { kind: "character", assetId: "11", src: "/files/a.png" } as CanvasNodeData,
  });
  const human = (status: VirtualHumanDto["status"]): VirtualHumanDto => ({
    id: "h1",
    name: "迅哥儿",
    status,
    images: [],
    createdAt: "",
  });

  test("没传 enroll（没配置虚拟人像库、还没生成形象）时没有按钮", () => {
    expect(card(done)).not.toContain("真人库");
  });

  test("还没加入：显示「加入真人库」按钮", () => {
    expect(card(done, false, { human: null, onEnroll: noop })).toContain('title="加入真人库"');
  });

  test("已加入：只显示所属真人和备案状态，不能重复加入", () => {
    const out = card(done, false, { human: human("pending"), onEnroll: noop });
    expect(out).toContain("已在真人库「迅哥儿」· 备案中");
    expect(out).not.toContain('title="加入真人库"');
  });
});
