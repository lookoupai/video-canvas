package reference_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"video-canvas/internal/reference"
)

func TestAnalyzer_OfflineCommandAndParsing(t *testing.T) {
	if os.PathSeparator != '/' {
		t.Skip("测试可执行脚本需要 Unix shell")
	}
	dir := t.TempDir()
	command := filepath.Join(dir, "rea-video")
	script := `#!/bin/sh
if [ "$1" != "decompose" ] || [ "$3" != "-o" ]; then exit 2; fi
case " $* " in *" --no-vlm "*" --no-asr "*" --no-stems "*) ;; *) exit 3 ;; esac
out=$4
mkdir -p "$out/video"
cat > "$out/VIDEO.md" <<'CARD'
---
videomd: 1
source: {duration: 10}
---
CARD
cat > "$out/video/timeline.md" <<'TIMELINE'
---
videomd: 1
part: timeline
---
` + "```videomd-timeline" + `
#01 0.0-10.0 | seen 测试画面
` + "```" + `
TIMELINE
printf '%s\n' "$@" > "$out/args.txt"
`
	if err := os.WriteFile(command, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	analyzer := reference.NewAnalyzer()
	if !analyzer.Available() {
		t.Fatal("应检测到工具")
	}
	doc, err := analyzer.Analyze(t.Context(), filepath.Join(dir, "input video.mp4"), 10000)
	if err != nil || len(doc.Shots) != 1 || doc.Shots[0].EndMs != 10000 {
		t.Fatalf("%+v %v", doc, err)
	}
}

func TestAnalyzer_MissingOrCanceled(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	analyzer := reference.NewAnalyzer()
	if analyzer.Available() {
		t.Fatal("不应检测到工具")
	}
	_, err := analyzer.Analyze(t.Context(), "/input", 10000)
	if !errors.Is(err, reference.ErrAnalyzerUnavailable) {
		t.Fatalf("%v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = analyzer.Analyze(ctx, "/input", 10000)
	if err == nil || !strings.Contains(err.Error(), "canceled") {
		t.Fatalf("%v", err)
	}
}
