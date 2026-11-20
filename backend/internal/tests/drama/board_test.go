package drama_test

import (
	"testing"

	"video-canvas/internal/drama"
)

func TestBoardHashes(t *testing.T) {
	payload := []byte(`{"nodes":[
		{"id":"b1","data":{"kind":"image","src":"/files/1.png","drama":{"episodes":[1],"board":{"segmentId":"5d2a","sourceHash":"h1"}}}},
		{"id":"b2","data":{"kind":"image","src":"","drama":{"episodes":[1],"board":{"segmentId":"5d2b","sourceHash":"h2"}}}},
		{"id":"b3","data":{"kind":"image","src":"blob:local","drama":{"episodes":[1],"board":{"segmentId":"5d2c","sourceHash":"h3"}}}},
		{"id":"a1","data":{"kind":"image","src":"/files/2.png","drama":{"type":"scene","name":"村口","episodes":[]}}},
		{"id":"g1","type":"group","data":{}}
	]}`)
	got, err := drama.BoardHashes(payload)
	if err != nil {
		t.Fatal(err)
	}
	// 只认有图的故事板节点：还没出图、本地上传中（blob）的、普通资产都不算
	if len(got) != 1 || got["5d2a"] != "h1" {
		t.Fatalf("故事板哈希：%v", got)
	}
	if _, err := drama.BoardHashes([]byte(`{`)); err == nil {
		t.Fatal("坏的画布 JSON 要报错")
	}
	if got, err := drama.BoardHashes(nil); err != nil || len(got) != 0 {
		t.Fatalf("空画布：%v %v", got, err)
	}
}

func TestBoardShotCount(t *testing.T) {
	prompt := "【画幅风格】9:16\n【时间轴】\n0-3秒：全景。\n3-6秒：中景。台词：『你好』\n6-9.5秒：近景。\n【站位】左"
	for _, tc := range []struct {
		name, prompt string
		want         int
	}{
		{"数【时间轴】里的镜头行", prompt, 3},
		{"全角冒号、小数秒、波浪线都认", "【时间轴】\n0~3秒：A\n3-6.5秒：B", 2},
		{"没有时间轴", "一段没有分栏的原稿", 0},
		{"时间轴后遇到下一栏就停", "【时间轴】\n0-3秒：A\n【站位】\n3-6秒：不算", 1},
		{"空", "", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := drama.BoardShotCount(tc.prompt); got != tc.want {
				t.Fatalf("got %d want %d", got, tc.want)
			}
		})
	}
}

func TestBoardLayout(t *testing.T) {
	for shots, want := range map[int]string{0: "2x2", 1: "2x2", 4: "2x2", 5: "3x2", 6: "3x2", 7: "3x3", 9: "3x3", 20: "3x3"} {
		if got := drama.BoardLayout(shots); got != want {
			t.Fatalf("%d 镜：got %s want %s", shots, got, want)
		}
	}
}
