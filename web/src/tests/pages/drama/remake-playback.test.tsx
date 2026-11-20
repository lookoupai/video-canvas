import { afterAll, beforeAll, expect, spyOn, test } from "bun:test";
import type { Segment } from "@/api/drama/type";
import type { usePlayback as PlaybackHook } from "@/pages/drama/studio/use-playback";
import { installDom, type DomEnv } from "../../helpers/dom";

let env: DomEnv;
let usePlayback: typeof PlaybackHook;
const mediaMocks: { mockRestore(): void }[] = [];
beforeAll(async () => {
  env = await installDom();
  mediaMocks.push(spyOn(env.win.HTMLMediaElement.prototype, "load").mockImplementation(() => {}));
  mediaMocks.push(
    spyOn(env.win.HTMLMediaElement.prototype, "play").mockImplementation(async () => {}),
  );
  mediaMocks.push(spyOn(env.win.HTMLMediaElement.prototype, "pause").mockImplementation(() => {}));
  usePlayback = (await import("@/pages/drama/studio/use-playback")).usePlayback;
});
afterAll(() => {
  mediaMocks.forEach((mock) => mock.mockRestore());
  env.restore();
});

function segment(id: string, seq: number): Segment {
  return {
    id,
    seq,
    episodeNo: 1,
    seconds: 5,
    prompt: "",
    takes: [
      {
        taskId: id,
        status: "succeeded",
        asset: {
          id,
          url: `/files/${id}.mp4`,
          kind: "video",
          durationMs: 5000,
          mimeType: "video/mp4",
          byteSize: 1,
          width: null,
          height: null,
          fileName: null,
        },
        errorMessage: "",
        createdAt: "",
      },
    ],
    chosenTaskId: id,
    status: "done",
    revision: 1,
    updatedAt: "",
    adaptations: {},
  };
}
const first = segment("a", 0);
first.remake = {
  adoptionId: "adopt",
  referenceId: "ref",
  referenceRevision: 1,
  assetId: "1",
  sources: [],
  roles: [],
  targetDurationMs: 2500,
  trimStartMs: 500,
};
const segments = [first, segment("b", 1)];
function Harness() {
  const playback = usePlayback(segments);
  return (
    <>
      <button onClick={playback.toggle}>连播</button>
      <output aria-label="当前片段">{playback.currentId}</output>
      {([0, 1] as const).map((which) => {
        const slot = playback.slot(which);
        return (
          <video
            key={which}
            data-slot={which}
            ref={slot.ref}
            onLoadedMetadata={slot.onLoadedMetadata}
            onTimeUpdate={slot.onTimeUpdate}
            onEnded={slot.onEnded}
          />
        );
      })}
    </>
  );
}

test("连播从采用素材的裁剪起点开始，到目标终点立即切下一镜", async () => {
  const { unmount } = await env.mount(<Harness />);
  try {
    await env.click(document.querySelector("button"));
    const current = document.querySelector<HTMLVideoElement>('video[data-slot="0"]')!;
    await env.act(async () => current.dispatchEvent(new env.win.Event("loadedmetadata")));
    expect(current.currentTime).toBe(0.5);
    expect(document.querySelector("output")?.textContent).toBe("a");
    await env.act(async () => {
      current.currentTime = 3;
      current.dispatchEvent(new env.win.Event("timeupdate"));
    });
    expect(document.querySelector("output")?.textContent).toBe("b");
    expect(document.querySelector<HTMLVideoElement>('video[data-slot="1"]')?.currentTime).toBe(0);
  } finally {
    await unmount();
  }
});
