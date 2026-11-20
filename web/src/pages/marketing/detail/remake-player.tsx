import { useEffect, useRef } from "react";
import type { RemakeSource } from "@/api/marketing/type";

/** 原生片段定位配合播放时逐帧检查，浏览器自身暂停时也将播放头收回到区间终点。 */
export function IntervalPlayer({
  url,
  source,
  autoPlay,
  onError,
}: {
  url: string;
  source: RemakeSource;
  autoPlay: boolean;
  onError: () => void;
}) {
  const frame = useRef(0);
  useEffect(() => () => cancelAnimationFrame(frame.current), []);
  const stopAtEnd = (video: HTMLVideoElement) => {
    if (video.currentTime < source.end) return;
    if (!video.paused) video.pause();
    if (video.currentTime > source.end) video.currentTime = source.end;
  };
  const monitor = (video: HTMLVideoElement) => {
    stopAtEnd(video);
    if (!video.paused) frame.current = requestAnimationFrame(() => monitor(video));
  };
  return (
    <video
      src={`${url.split("#")[0]}#t=${source.start},${source.end}`}
      aria-label={`原镜 ${source.refShot} 区间回放`}
      className="h-[min(32svh,260px)] w-full shrink-0 rounded-lg bg-black object-contain"
      controls
      playsInline
      preload="metadata"
      autoPlay={autoPlay}
      onError={onError}
      onPlay={({ currentTarget: video }) => {
        if (video.currentTime < source.start || video.currentTime >= source.end)
          video.currentTime = source.start;
        cancelAnimationFrame(frame.current);
        monitor(video);
      }}
      onSeeked={({ currentTarget: video }) => {
        if (video.currentTime < source.start) video.currentTime = source.start;
        stopAtEnd(video);
      }}
      onTimeUpdate={({ currentTarget: video }) => stopAtEnd(video)}
      onPause={({ currentTarget: video }) => stopAtEnd(video)}
    />
  );
}
