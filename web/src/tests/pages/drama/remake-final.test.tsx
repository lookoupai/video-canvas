import { afterAll, beforeAll, expect, spyOn, test } from "bun:test";
import type { FinalArea as Area } from "@/pages/drama/workspace/final-area";
import type { DramaWorkspaceContext as Context } from "@/pages/drama/workspace/context";
import type { DramaWorkspace } from "@/pages/drama/workspace/use-workspace";
import service from "@/utils/requests/service";
import { useDramaSegmentsStore } from "@/store/drama-segments";
import { installDom, type DomEnv } from "../../helpers/dom";

let env: DomEnv;
let FinalArea: typeof Area;
let Workspace: typeof Context;
beforeAll(async () => {
  env = await installDom();
  FinalArea = (await import("@/pages/drama/workspace/final-area")).FinalArea;
  Workspace = (await import("@/pages/drama/workspace/context")).DramaWorkspaceContext;
});
afterAll(() => env.restore());

test("成片页按两个2.5秒目标窗口显示5秒，不把生成4秒加进预计时长", async () => {
  const get = spyOn(service, "get").mockResolvedValue({
    items: [0, 1].map((seq) => ({
      id: "s" + seq,
      seq,
      episode_no: 1,
      seconds: 4,
      prompt: "",
      revision: 1,
      status: "done",
      chosen_task_id: "t" + seq,
      updated_at: "",
      takes: [
        {
          task_id: "t" + seq,
          status: "succeeded",
          asset: { id: 10 + seq, kind: "video", url: "/files/result.mp4", duration_ms: 4000 },
        },
      ],
      remake: {
        reference_id: "r",
        adoption_id: "a",
        reference_revision: 3,
        asset_id: 1,
        sources: [],
        roles: [],
        target_duration_ms: 2500,
        trim_start_ms: 500,
      },
    })),
  });
  useDramaSegmentsStore.setState({ byEpisode: {}, watching: {} });
  const value = {
    drama: {
      id: "d",
      title: "仿拍",
      ratio: "9:16",
      episodes: [
        { no: 1, title: "第一集", segmentCount: 2, doneSegmentCount: 2, final: null, locked: true },
      ],
    },
    jobs: [],
  } as unknown as DramaWorkspace;
  const { unmount } = await env.mount(
    <Workspace.Provider value={value}>
      <FinalArea ep={1} onGo={() => {}} />
    </Workspace.Provider>,
  );
  try {
    expect(document.querySelector('[aria-label="合成整集"]')?.textContent).toContain("00:05");
    expect(document.querySelector('[aria-label="合成整集"]')?.textContent).toContain("2.5s");
  } finally {
    await unmount();
    get.mockRestore();
    useDramaSegmentsStore.setState({ byEpisode: {}, watching: {} });
  }
});
