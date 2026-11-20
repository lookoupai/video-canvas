package reference

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"video-canvas/internal/model"
)

// ErrAnalyzerUnavailable 表示本机未安装 rea-video，仍可上传拆片包。
var ErrAnalyzerUnavailable = errors.New("未安装 rea-video，请先导入拆片包")

// Analyzer 使用 rea-video 本地工具提取分镜，角色与对白由导入包或人工校订补齐。
type Analyzer struct{}

// NewAnalyzer 检测部署环境中的工具路径，不接受用户自定义执行命令。
func NewAnalyzer() *Analyzer {
	return &Analyzer{}
}

// Available 返回本机是否有可执行的分析工具。
func (a *Analyzer) Available() bool {
	_, err := exec.LookPath("rea-video")
	return err == nil
}

// Analyze 在独立临时目录运行离线拆片，进程受取消和时限约束，完成后清理产生的媒体文件。
func (a *Analyzer) Analyze(ctx context.Context, video string, durationMs int64) (model.DramaReferenceDocument, error) {
	empty := emptyDocument()
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	if !a.Available() {
		return empty, ErrAnalyzerUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	dir, err := os.MkdirTemp("", "vc-reference-analysis-")
	if err != nil {
		return empty, err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	cmd := exec.CommandContext(ctx, "rea-video", "decompose", video, "-o", dir, "--no-vlm", "--no-whole", "--no-asr", "--no-speakers", "--no-stems", "--no-ocr", "--no-packaging", "--workers", "1") //nolint:gosec // 固定可执行文件和开关，video 与 dir 均由服务器生成；直接传参而不经 shell。
	cmd.Env = append(os.Environ(), "OMP_NUM_THREADS=1", "OPENBLAS_NUM_THREADS=1", "MKL_NUM_THREADS=1", "OPENCV_FOR_THREADS_NUM=1")
	configureCommand(cmd)
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return empty, ctx.Err()
		}
		return empty, fmt.Errorf("rea-video 分析失败：%w", err)
	}
	files, err := analysisFiles(dir)
	if err != nil {
		return empty, err
	}
	doc, err := decodeVideoMD(files, durationMs)
	if err != nil {
		return empty, err
	}
	if err := ValidateDocument(doc, durationMs); err != nil {
		return empty, err
	}
	return doc, nil
}

// analysisFiles 仅读取生成的文本元数据，拒绝符号链接和过大文件。
func analysisFiles(dir string) (map[string][]byte, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	files := map[string][]byte{}
	total := 0
	err = filepath.WalkDir(dir, func(p string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		text, err := analysisText(root, rel, entry)
		if err != nil {
			return err
		}
		if text == nil {
			return nil
		}
		total += len(text)
		if total > 32<<20 || len(files) >= 4096 {
			return ErrPackage
		}
		files[filepath.ToSlash(rel)] = text
		return nil
	})
	return files, err
}

// analysisText 拒绝链接与过大的元数据，媒体文件留在临时目录中等待统一回收。
func analysisText(root *os.Root, rel string, entry fs.DirEntry) ([]byte, error) {
	if entry.Type()&fs.ModeSymlink != 0 {
		return nil, ErrPackage
	}
	if !metadataExt(strings.ToLower(filepath.Ext(rel))) {
		return nil, nil
	}
	info, err := entry.Info()
	if err != nil {
		return nil, err
	}
	if info.Size() > MaxPackageBytes {
		return nil, ErrPackage
	}
	return root.ReadFile(rel)
}
