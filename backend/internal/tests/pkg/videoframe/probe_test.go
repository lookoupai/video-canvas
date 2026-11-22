package videoframe_test

import (
	"os/exec"
	"path/filepath"
	"testing"

	"video-canvas/internal/pkg/videoframe"
)

func TestProbeDistinguishesMissingAudio(t *testing.T) {
	e := videoframe.New(videoframe.Options{})
	if !e.Available() {
		t.Skip("需要 ffmpeg 和 ffprobe")
	}
	for _, sound := range []bool{false, true} {
		name := "silent"
		if sound {
			name = "audio"
		}
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "sample.mp4")
			args := []string{"-v", "error", "-f", "lavfi", "-i", "color=c=red:s=32x32:d=1:r=10"}
			if sound {
				args = append(args, "-f", "lavfi", "-i", "sine=frequency=440:duration=1", "-c:a", "aac")
			}
			args = append(args, "-c:v", "libx264", "-pix_fmt", "yuv420p", "-shortest", path)
			if out, err := exec.CommandContext(t.Context(), "ffmpeg", args...).CombinedOutput(); err != nil {
				t.Fatalf("生成样片：%v %s", err, out)
			}
			info, err := e.Probe(t.Context(), path)
			if err != nil || info.HasAudio != sound || info.DurationMs < 950 || info.DurationMs > 1100 || info.FrameRate != 10 {
				t.Fatalf("音轨探测：%+v %v", info, err)
			}
		})
	}
	if _, err := e.Probe(t.Context(), filepath.Join(t.TempDir(), "missing.mp4")); err == nil {
		t.Fatal("探测失败不能冒充无音轨")
	}
}
