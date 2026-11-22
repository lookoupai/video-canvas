package service_test

import (
	"bytes"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/idcodec"
)

func TestReferencePreparationFramesMatchOriginalShotPixels(t *testing.T) {
	r := newPreparationRig(t)
	file := filepath.Join(t.TempDir(), "four-colors.mp4")
	args := []string{"-v", "error"}
	for _, color := range []string{"red", "green", "blue", "yellow"} {
		args = append(args, "-f", "lavfi", "-i", "color=c="+color+":s=32x32:d=2.5:r=20")
	}
	args = append(args, "-filter_complex", "[0:v][1:v][2:v][3:v]concat=n=4:v=1:a=0[v]", "-map", "[v]", "-c:v", "libx264", "-pix_fmt", "yuv420p", file)
	if out, err := exec.CommandContext(t.Context(), "ffmpeg", args...).CombinedOutput(); err != nil {
		t.Fatalf("四镜样片：%v %s", err, out)
	}
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	r.assets.assets.items[1].data = data
	created, err := r.svc.Create(t.Context(), 7, r.project, r.reference, &model.CreateDramaReferencePreparationReq{RequestKey: "pixels", ReferenceRevision: r.revision})
	if err != nil {
		t.Fatal(err)
	}
	id, _ := idcodec.Decode(created.ID)
	if err := r.svc.HandlePreparation(t.Context(), 7, r.project, r.reference, id, 1); err != nil {
		t.Fatal(err)
	}
	page, err := r.svc.Frames(t.Context(), 7, r.project, r.reference, id, &model.ListDramaReferenceFramesReq{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 12 {
		t.Fatalf("帧数 %d", len(page.Items))
	}
	for _, frame := range page.Items {
		if frame.Asset == nil {
			t.Fatalf("未持久化：%+v", frame)
		}
		r.assets.mu.Lock()
		data := append([]byte(nil), r.assets.assets.items[frame.Asset.ID].data...)
		r.assets.mu.Unlock()
		img, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		red, green, blue, _ := img.At(16, 16).RGBA()
		correct := map[string]bool{"1": red > 50000 && green < 5000 && blue < 5000, "2": green > 20000 && red < 5000 && blue < 5000, "3": blue > 50000 && red < 5000 && green < 5000, "4": red > 50000 && green > 50000 && blue < 5000}
		if !correct[frame.ShotID] {
			t.Fatalf("采样越过原镜：shot=%s at=%d rgb=%d,%d,%d", frame.ShotID, frame.AtMs, red, green, blue)
		}
	}
}
