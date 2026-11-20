package service

import (
	"encoding/json"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
)

// editRemakeWindow 仅更新目标素材的成片窗口，固定的原片依据与已选任务保持不变。
func editRemakeWindow(row *model.DramaSegment, req *model.UpdateDramaSegmentReq) error {
	if req.TargetDurationMs == nil && req.TrimStartMs == nil {
		return nil
	}
	remake, err := segmentRemakeOf(row)
	if err != nil {
		return err
	}
	if remake == nil {
		return errcode.ErrInvalidParams.WithMsg("只有仿拍片段支持精确成片裁剪")
	}
	if req.TargetDurationMs != nil {
		remake.TargetDurationMs = *req.TargetDurationMs
	}
	if req.TrimStartMs != nil {
		remake.TrimStartMs = *req.TrimStartMs
	}
	if err := validateRemakeWindow(remake.TrimStartMs, remake.TargetDurationMs); err != nil {
		return err
	}
	row.Remake, err = json.Marshal(remake)
	return err
}
