import { afterAll, beforeAll, expect, spyOn, test } from "bun:test";
import { MemoryRouter, useLocation } from "react-router";
import service from "@/utils/requests/service";
import type { DramaReference } from "@/api/drama/reference-type";
import type { ReferenceAdopt } from "@/pages/drama/references/adopt";
import { installDom, type DomEnv } from "../../helpers/dom";

let env: DomEnv;
let Adopt: typeof ReferenceAdopt;
beforeAll(async () => {
  env = await installDom();
  Adopt = (await import("@/pages/drama/references/adopt")).ReferenceAdopt;
});
afterAll(() => env.restore());

const episode: DramaReference = {
  id: "ref",
  seq: 1,
  title: "参考集",
  asset: null,
  durationMs: 5000,
  revision: 4,
  status: "ready",
  error: "",
  method: "manual",
  bindings: {},
  versions: [],
  document: {
    scenes: [],
    characters: [],
    dialogues: [],
    shots: [
      {
        id: "one",
        sceneId: "",
        startMs: 0,
        endMs: 2500,
        action: "递钥匙",
        camera: "",
        characters: [],
      },
      {
        id: "two",
        sceneId: "",
        startMs: 2500,
        endMs: 5000,
        action: "接钥匙",
        camera: "",
        characters: [],
      },
    ],
  },
};
const existing = {
  id: "old",
  reference_id: "ref",
  reference_revision: 3,
  mode: "follow",
  episode_id: "ep",
  episode_no: 2,
  episode_revision: 7,
  segment_ids: ["s1", "s2"],
  deleted: false,
};
function Location() {
  const loc = useLocation();
  return <output aria-label="位置">{loc.pathname + loc.search}</output>;
}
const button = (text: string) =>
  Array.from(document.querySelectorAll("button")).find((el) => el.textContent === text)!;

test("更新已有集只采用勾选原镜，响应丢失后重试复用同一键", async () => {
  const get = spyOn(service, "get").mockResolvedValue([existing]);
  const post = spyOn(service, "post")
    .mockRejectedValueOnce(new Error("响应丢失"))
    .mockResolvedValue({ ...existing, id: "new", reference_revision: 4, episode_revision: 8 });
  const { unmount } = await env.mount(
    <MemoryRouter>
      <Adopt id="drama" episode={episode} disabled={false} />
      <Location />
    </MemoryRouter>,
  );
  try {
    await env.click(button("采用到片场"));
    const target = document.querySelector<HTMLSelectElement>('[aria-label="采用目标"]')!;
    await env.act(async () => {
      target.value = "ep";
      target.dispatchEvent(new env.win.Event("change", { bubbles: true }));
    });
    await env.click(document.querySelector('[aria-label="更新原镜 2"]'));
    await env.click(button("采用并进入片场"));
    expect(post.mock.calls[0][1]).toMatchObject({
      revision: 4,
      target_episode_no: 2,
      episode_revision: 7,
      shot_ids: ["two"],
    });
    const key = (post.mock.calls[0][1] as { request_key: string }).request_key;
    expect(key).toBeTruthy();
    await env.click(button("采用并进入片场"));
    expect((post.mock.calls[1][1] as { request_key: string }).request_key).toBe(key);
    expect(document.querySelector('[aria-label="位置"]')?.textContent).toBe(
      "/drama/drama?area=studio&ep=2",
    );
  } finally {
    await unmount();
    get.mockRestore();
    post.mockRestore();
  }
});

test("未保存的校订不能被当作采用依据，原创改编进入分集剧本", async () => {
  const get = spyOn(service, "get").mockResolvedValue([]);
  const post = spyOn(service, "post").mockResolvedValue({
    ...existing,
    mode: "rewrite",
    episode_no: 3,
  });
  const mounted = await env.mount(
    <MemoryRouter>
      <Adopt id="drama" episode={episode} disabled />
      <Location />
    </MemoryRouter>,
  );
  try {
    expect(button("采用到片场").disabled).toBe(true);
  } finally {
    await mounted.unmount();
  }
  const { unmount } = await env.mount(
    <MemoryRouter>
      <Adopt id="drama" episode={episode} disabled={false} />
      <Location />
    </MemoryRouter>,
  );
  try {
    await env.click(button("采用到片场"));
    await env.click(document.querySelector('[aria-label="原创改编"]'));
    await env.type(document.querySelector('[aria-label="改编要求"]'), "改成误会引发的喜剧");
    await env.click(button("采用并编辑剧本"));
    expect(post.mock.calls[0][1]).toMatchObject({
      mode: "rewrite",
      instruction: "改成误会引发的喜剧",
    });
    expect(document.querySelector('[aria-label="位置"]')?.textContent).toContain(
      "area=script&ep=3",
    );
  } finally {
    await unmount();
    get.mockRestore();
    post.mockRestore();
  }
});
