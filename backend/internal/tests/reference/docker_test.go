package reference_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// TestAnalyzer_DockerImage 验证真实镜像在断网、只读根目录和默认用户下可完成四镜样片分析。
// 默认跳过；构建镜像后设置 TEST_REFERENCE_IMAGE 运行，不启动数据库或应用服务。
func TestAnalyzer_DockerImage(t *testing.T) {
	image := os.Getenv("TEST_REFERENCE_IMAGE")
	if image == "" {
		t.Skip("未设置 TEST_REFERENCE_IMAGE，跳过镜像内分析验收")
	}
	sample, err := filepath.Abs("../../marketing/samples/cream_ref.mp4")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "docker", "run", "--rm", "--network=none", "--read-only",
		"--tmpfs", "/tmp:rw,size=512m", "--mount", "type=bind,source="+sample+",target=/input.mp4,readonly",
		"--entrypoint", "/bin/sh", image, "-ec", dockerAnalyzeScript)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("镜像内离线分析失败：%v\n%s", err, out)
	}
}

const dockerAnalyzeScript = `test "$(id -u)" = 100
test "$(id -g)" = 101
command -v ffmpeg
command -v ffprobe
command -v wget
command -v rea-video
command -v videomd
rea-video decompose /input.mp4 -o /tmp/reference --no-vlm --no-whole --no-asr --no-speakers --no-stems --no-ocr --no-packaging --workers 1
python - <<'PY'
import importlib.metadata as metadata
import hashlib
import json
import os
from pathlib import Path
import yaml
from rea_video.faces import MODEL_NAME, MODEL_SHA256

model_dir = Path(os.environ["REA_VIDEO_MODELS"])
assert hashlib.sha256((model_dir / MODEL_NAME).read_bytes()).hexdigest() == MODEL_SHA256
assert (model_dir / "LICENSE.yunet").is_file(), "缺少模型许可"
root = Path("/tmp/reference")
assert (root / "VIDEO.md").is_file(), "未生成 VIDEO.md"
shots = sorted((root / "video" / "shots").glob("*.md"))
assert len(shots) == 4, f"四镜样片应拆为 4 镜：{len(shots)}"
times = [yaml.safe_load(path.read_text().split("---", 2)[1])["time"] for path in shots]
for actual, expected in zip(times, [[0, 2.5], [2.5, 5], [5, 7.5], [7.5, 10]]):
    assert all(abs(a - b) < .1 for a, b in zip(actual, expected)), (actual, expected)
for package, revision in [("rea-video", "538eb3a757db94826cb90c4297279e50c5dd0f83"), ("videomd", "083722191572ec00710c6eb69a9351809ed443ce")]:
    dist = metadata.distribution(package)
    assert revision in json.loads(dist.read_text("direct_url.json"))["url"], package
    assert any("LICENSE" in str(file) for file in dist.files), package + " 缺少许可"
print("镜像内离线四镜分析通过，原片区间完整")
PY
`
