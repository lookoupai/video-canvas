package videoframe

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
)

// ErrWindow 表示所选区间为空、越界或短于一个输出视频帧。
var ErrWindow = errors.New("素材视频不包含完整的选定区间")

// Window 从本地视频取左闭右开的毫秒区间。
type Window struct {
	Path           string
	StartMs, EndMs int
}

// ComposeRanges 按顺序裁取多个视频区间，复用合成的探测、尺寸和帧率规则。
// 成片保留选版的声音，无声音轨补静音；mute 用于给模型的无声参考片。
func (e *Extractor) ComposeRanges(ctx context.Context, windows []Window, destination string, mute bool) error {
	if len(windows) == 0 {
		return ErrWindow
	}
	ctx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()
	args := []string{"-nostdin", "-v", "error", "-filter_complex_threads", "1"}
	var filters, inputs strings.Builder
	var first composeInfo
	infos := map[string]composeInfo{}
	for i, w := range windows {
		info, ok := infos[w.Path]
		if !ok {
			var err error
			if info, err = e.composeProbe(ctx, w.Path); err != nil {
				return err
			}
			infos[w.Path] = info
		}
		if w.StartMs < 0 || w.EndMs <= w.StartMs || w.EndMs-w.StartMs < 33 || w.EndMs > int(math.Round(info.duration*1000)) {
			return ErrWindow
		}
		if i == 0 {
			first = info
		}
		args = append(args, "-protocol_whitelist", "file", "-threads", "1", "-ss", rangeSeconds(w.StartMs), "-t", rangeSeconds(w.EndMs-w.StartMs), "-i", w.Path)
		writeWindowFilter(&filters, &inputs, i, w, first, info.audio, mute)
	}
	filters.WriteString(inputs.String())
	fmt.Fprintf(&filters, "concat=n=%d:v=1", len(windows))
	if mute {
		filters.WriteString(":a=0[v]")
	} else {
		filters.WriteString(":a=1[v][a]")
	}
	args = append(args, "-filter_complex", filters.String(), "-map", "[v]", "-c:v", "libx264", "-preset", "veryfast", "-crf", "20", "-pix_fmt", "yuv420p", "-video_track_timescale", "90000")
	if !mute {
		args = append(args, "-map", "[a]", "-c:a", "aac")
	}
	args = append(args, "-movflags", "+faststart", "-y", destination)
	if _, err := e.run(ctx, e.ffmpeg, args...); err != nil {
		_ = os.Remove(destination) // FFmpeg 启动后可能留下半成品。
		return err
	}
	return nil
}

// writeWindowFilter 把同一输入的画面和声音裁到同一个时间窗，再放进有序拼接列表。
func writeWindowFilter(filters, inputs *strings.Builder, i int, w Window, first composeInfo, audio, mute bool) {
	duration := rangeSeconds(w.EndMs - w.StartMs)
	fmt.Fprintf(filters, "[%d:v:0]%s,tpad=stop_mode=clone:stop_duration=%s,trim=duration=%s,setpts=PTS-STARTPTS[v%d];", i, composeVideoFilter(first), duration, duration, i)
	fmt.Fprintf(inputs, "[v%d]", i)
	if mute {
		return
	}
	if audio {
		fmt.Fprintf(filters, "[%d:a:0]aresample=48000:async=1:first_pts=0,aformat=channel_layouts=stereo,apad", i)
	} else {
		filters.WriteString("anullsrc=channel_layout=stereo:sample_rate=48000")
	}
	fmt.Fprintf(filters, ",atrim=duration=%s,asetpts=PTS-STARTPTS[a%d];", duration, i)
	fmt.Fprintf(inputs, "[a%d]", i)
}

// rangeSeconds 按源毫秒精度为 ffmpeg 生成秒数。
func rangeSeconds(ms int) string {
	return strconv.FormatFloat(float64(ms)/1000, 'f', 3, 64)
}
