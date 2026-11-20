package marketing_test

import (
	"encoding/json"
	"os"
	"testing"

	"video-canvas/internal/marketing"
)

func TestRemakeShots_CreamReference(t *testing.T) {
	raw, err := os.ReadFile("../../marketing/samples/cream_reverse.json")
	if err != nil {
		t.Fatal(err)
	}
	var rev marketing.Reverse
	if err := json.Unmarshal(raw, &rev); err != nil {
		t.Fatal(err)
	}
	follow := goodFollow()
	shots := goodShots(4, 8, "护手霜")
	one := marketing.RemakeShots(shots, follow.FollowMap, rev)
	if len(one) != 4 {
		t.Fatalf("成片镜头数 %d", len(one))
	}
	for i, shot := range one {
		if shot.DurationMs != 2000 || len(shot.Sources) != 1 {
			t.Fatalf("镜头 %d: %+v", i+1, shot)
		}
		ref := shot.Sources[0]
		if ref.RefShot != i+1 || ref.StartMs != i*2500 || ref.EndMs != (i+1)*2500 {
			t.Fatalf("原镜 %d: %+v", i+1, ref)
		}
	}
	follow.FollowMap[2].Shot = 0
	merged := marketing.RemakeShots(shots, follow.FollowMap, rev)
	refs := merged[0].Sources
	if len(refs) != 2 || refs[0] != one[0].Sources[0] || refs[1] != one[2].Sources[0] || len(merged[2].Sources) != 0 {
		t.Fatalf("镜 1 和镜 3 应保留为独立区间: %+v", merged)
	}
}
