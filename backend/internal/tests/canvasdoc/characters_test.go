package canvasdoc_test

import (
	"slices"
	"testing"

	"video-canvas/internal/canvasdoc"
)

// 本文件测试 CharacterImageUses：只认角色卡节点，主图 + 补充图去重，音色和其他节点的素材不算。

func TestCharacterImageUses(t *testing.T) {
	raw := []byte(`{"nodes":[
		{"id":"c1","data":{"kind":"character","assetId":"11","character":{"name":"迅哥儿","extras":[{"assetId":"12"},{"assetId":"11"},{"src":"x"}],"voice":{"mode":"audio","assetId":"90"}}}},
		{"id":"c2","data":{"kind":"character","character":{"name":"闰土","extras":[{"assetId":13}]}}},
		{"id":"i1","data":{"kind":"image","assetId":"11"}},
		{"id":"bad","data":"oops"}
	]}`)
	uses, err := canvasdoc.CharacterImageUses(raw)
	if err != nil {
		t.Fatalf("CharacterImageUses 失败：%v", err)
	}
	if len(uses) != 2 {
		t.Fatalf("应只有 2 张角色卡，得到 %d", len(uses))
	}
	if uses[0].NodeID != "c1" || uses[0].Name != "迅哥儿" || !slices.Equal(uses[0].AssetIDs, []string{"11", "12"}) {
		t.Errorf("c1 = %+v", uses[0])
	}
	// 没有主图也照常返回；JSON 数字形式的素材 ID 规整成字符串
	if uses[1].NodeID != "c2" || !slices.Equal(uses[1].AssetIDs, []string{"13"}) {
		t.Errorf("c2 = %+v", uses[1])
	}

	if _, err := canvasdoc.CharacterImageUses([]byte(`not json`)); err == nil {
		t.Error("内容损坏应返回错误")
	}
}
