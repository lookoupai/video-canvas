package service_test

import (
	"encoding/json"
	"testing"

	"video-canvas/internal/model"
	"video-canvas/internal/provider"
	"video-canvas/internal/provider/modelcfg"
	"video-canvas/internal/service"
	"video-canvas/internal/service/stepfake"
)

// 在计划读取入口验证模型时长档位、计价、引用数量和单镜编辑后的软问题。
func TestMarketing_RemakeQuote(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		field                modelcfg.ParamField
		duration, trim, want int
		scene                string
		ref                  bool
	}{
		{"向上取整并符合枚举", modelcfg.ParamField{Type: "enum", Open: true, Options: []any{"10", "5"}}, 2500, 0, 5, "举起商品", false},
		{"裁剪终点超过最短时长", modelcfg.ParamField{Type: "enum", Open: true, Options: []any{5, 10}}, 2500, 3000, 10, "举起商品", false},
		{"超过最长档", modelcfg.ParamField{Type: "enum", Open: true, Options: []any{5, 10}}, 11000, 0, 0, "举起商品", false},
		{"模型固定时长", modelcfg.ParamField{Type: "number", Default: 8}, 2000, 0, 8, "举起商品", false},
		{"未配置参考视频", modelcfg.ParamField{Type: "number", Open: true}, 2000, 0, 0, "举起商品", true},
		{"本镜提及原片品牌", modelcfg.ParamField{Type: "number", Open: true}, 2000, 0, 0, "展示 SOURCEBRAND 商标", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			snap := mktVideoSnapshot()
			for i, param := range snap.Model.Capabilities.Params {
				if param.Name == "duration" {
					snap.Model.Capabilities.Params[i].ParamField = tc.field
				}
			}
			snap.Model.Capabilities.Refs.Video.On = false
			models := &stepfake.Models{}
			models.Snapshots = map[string]*provider.Snapshot{mktVideoKey: snap}
			data := model.RemakePlanData{Settings: model.MarketingSettings{VideoModel: mktVideoKey, Platform: "douyin"}, Brands: []string{"SOURCEBRAND"}, ProductAssets: []uint64{1}, Shots: []model.RemakePlanShot{{ID: 3, Revision: 1, Scene: tc.scene, DurationMs: tc.duration, TrimStartMs: tc.trim}}}
			if tc.ref {
				data.RefVideoAssetID = idp(4)
				data.Shots[0].Sources = []model.MarketingRemakeSource{{RefShot: 3, StartMs: 5000, EndMs: 7500}}
			}
			raw, err := json.Marshal(data)
			if err != nil {
				t.Fatal(err)
			}
			repo := &planEditRepo{row: model.MarketingRemakePlan{ID: 5, UserID: 4, Data: raw}}
			svc := service.NewMarketingService(service.MarketingDeps{Repo: repo, Models: models})
			view, err := svc.GetRemakePlan(t.Context(), 4, 5)
			if err != nil {
				t.Fatal(err)
			}
			shot := view.Shots[0]
			if shot.GenerateSeconds != tc.want || (tc.want == 0) != (shot.GenerateError != "") || (tc.want > 0 && shot.RenderCredits != 20) {
				t.Fatalf("报价: %+v", shot)
			}
		})
	}
}
