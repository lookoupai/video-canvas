package videoframe_test

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image/png"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"video-canvas/internal/pkg/videoframe"
)

// sampleLevel 解码一小段单声道 PCM，量测采用的时间窗中是否有声。
func sampleLevel(t *testing.T, path, at string) float64 {
	t.Helper()
	raw, err := exec.CommandContext(t.Context(), "ffmpeg", "-v", "error", "-ss", at, "-i", path, "-t", "0.08", "-vn", "-f", "s16le", "-ac", "1", "-ar", "16000", "-").Output()
	if err != nil || len(raw) < 8 {
		t.Fatalf("读取音轨: %v，%d 字节", err, len(raw))
	}
	var sum float64
	for i := 0; i+1 < len(raw); i += 2 {
		sample := float64(int16(binary.LittleEndian.Uint16(raw[i:])))
		sum += sample * sample
	}
	return math.Sqrt(sum / float64(len(raw)/2))
}

func TestComposeRanges_SelectsIndependentWindowsAndAudio(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	red := composeColor(t, dir, "red", "160x120", true)
	scenes := makeThreeScenes(t)
	dst := filepath.Join(dir, "final.mp4")
	ranges := []videoframe.Window{
		{Path: scenes, StartMs: 2050, EndMs: 2550},
		{Path: red, StartMs: 300, EndMs: 600},
		{Path: scenes, StartMs: 1000, EndMs: 1500},
	}
	e := newExtractor()
	if err := e.ComposeRanges(ctx, ranges, dst, false); err != nil {
		t.Fatal(err)
	}
	duration, err := e.Duration(ctx, dst)
	if err != nil || math.Abs(duration-1.3) > 0.034 {
		t.Fatalf("成片时长 = %f，错误 %v", duration, err)
	}
	for _, point := range []struct {
		at      float64
		channel int
	}{{.25, 1}, {.65, 0}, {1.05, 2}} {
		raw, err := e.Frame(ctx, dst, point.at, videoframe.FrameOpts{})
		if err != nil {
			t.Fatal(err)
		}
		img, err := png.Decode(bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		r, g, b, _ := img.At(1, 1).RGBA()
		channels := []uint32{r, g, b}
		if channels[point.channel] < 3*channels[(point.channel+1)%3]+1000 || channels[point.channel] < 3*channels[(point.channel+2)%3]+1000 {
			t.Fatalf("%.3f 秒取错画面: RGB %v", point.at, channels)
		}
	}
	if sampleLevel(t, dst, "0.15") > 8 || sampleLevel(t, dst, "0.55") < 500 || sampleLevel(t, dst, "0.95") > 8 {
		t.Fatal("合成音轨接点不符")
	}
	if err := e.ComposeRanges(ctx, ranges, dst, true); err != nil {
		t.Fatal(err)
	}
	audio, err := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-select_streams", "a", "-show_entries", "stream=index", "-of", "csv=p=0", dst).Output()
	if err != nil || len(bytes.TrimSpace(audio)) != 0 {
		t.Fatalf("裁好的参考片应无原片音轨：%q, %v", audio, err)
	}
}

func TestComposeRanges_RejectsOutsideWindow(t *testing.T) {
	src := composeColor(t, t.TempDir(), "red", "160x120", false)
	for _, w := range []videoframe.Window{{Path: src, StartMs: -1, EndMs: 20}, {Path: src, StartMs: 100, EndMs: 100}, {Path: src, StartMs: 200, EndMs: 1001}} {
		dst := filepath.Join(t.TempDir(), "bad.mp4")
		if err := newExtractor().ComposeRanges(t.Context(), []videoframe.Window{w}, dst, false); !errors.Is(err, videoframe.ErrWindow) {
			t.Fatalf("应拒绝区间 %+v: %v", w, err)
		}
		if _, err := os.Stat(dst); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("错误留下文件: %v", err)
		}
	}
}
