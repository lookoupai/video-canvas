package videoframe

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// ProbeInfo 保存已探测时长和真实音轨存在性，不表示做过任何声音语义分析。
type ProbeInfo struct {
	DurationMs int64
	HasAudio   bool
	FrameRate  float64
}

// Probe 同时检查有效视频轨、时长和音轨；探测失败返回错误而不是无音轨。
func (e *Extractor) Probe(ctx context.Context, src string) (ProbeInfo, error) {
	var info ProbeInfo
	out, err := e.run(ctx, e.ffprobe, "-v", "error", "-protocol_whitelist", e.protocols,
		"-show_entries", "format=duration:stream=codec_type,avg_frame_rate", "-of", "json", src)
	if err != nil {
		return info, err
	}
	var result struct {
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
		Streams []struct {
			CodecType    string `json:"codec_type"`
			AvgFrameRate string `json:"avg_frame_rate"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(out, &result); err != nil {
		return info, fmt.Errorf("%w: 视频探测结果无效", ErrDecode)
	}
	duration, err := strconv.ParseFloat(result.Format.Duration, 64)
	if err != nil || math.IsNaN(duration) || math.IsInf(duration, 0) || duration <= 0 || duration > 24*60*60 {
		return info, fmt.Errorf("%w: 视频时长无效", ErrDecode)
	}
	hasVideo := false
	for _, stream := range result.Streams {
		if stream.CodecType == "video" && !hasVideo {
			hasVideo = true
			info.FrameRate = frameRate(stream.AvgFrameRate)
		}
		info.HasAudio = info.HasAudio || stream.CodecType == "audio"
	}
	if !hasVideo {
		return info, fmt.Errorf("%w: 没有视频轨", ErrDecode)
	}
	info.DurationMs = int64(math.Round(duration * 1000))
	return info, nil
}

// frameRate parses ffprobe's average rate, which may be unavailable for variable-rate inputs.
func frameRate(value string) float64 {
	parts := strings.Split(value, "/")
	if len(parts) != 2 {
		return 0
	}
	numerator, numeratorErr := strconv.ParseFloat(parts[0], 64)
	denominator, denominatorErr := strconv.ParseFloat(parts[1], 64)
	if numeratorErr != nil || denominatorErr != nil || denominator <= 0 {
		return 0
	}
	return numerator / denominator
}
