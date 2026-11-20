package service_test

import (
	"encoding/json"
	"strings"
	"testing"

	"video-canvas/internal/model"
)

func TestDramaReferenceRewriteUsesAdoptedSnapshot(t *testing.T) {
	repo := newDramaMemory()
	dramaAddEpisode(repo, 1, "")
	basis := model.DramaReferenceBasis{AdoptionID: "a", ReferenceID: "r", ReferenceRevision: 4, Mode: "rewrite", Instruction: "保留钥匙引发的冲突，改写为职场悬疑", Document: referenceDocument(), Roles: []model.DramaRemakeRole{{CharacterID: "a", TargetNodeID: "target-a", TargetName: "新阿澄"}, {CharacterID: "b", TargetNodeID: "target-b", TargetName: "新小林"}}}
	raw, err := json.Marshal(basis)
	if err != nil {
		t.Fatal(err)
	}
	repo.episodes[0].ReferenceBasis = raw
	stream := &dramaTextStreamer{outputs: []string{"第1集\n新阿澄：这把钥匙是谁的？\n【本集完】"}}
	svc, _ := dramaTestService(repo, stream, nil)
	job, err := svc.CreateJob(t.Context(), 7, 1, &model.DramaJobReq{Step: "scripts", Episodes: []int{1}})
	if err != nil {
		t.Fatal(err)
	}
	repo.episodes[0].ReferenceBasis = []byte("{}")
	if err := svc.HandleDrama(t.Context(), 7, 1, uint64(job.ID)); err != nil {
		t.Fatal(err)
	}
	if len(stream.prompts) != 1 {
		t.Fatalf("scripts调用次数 %d", len(stream.prompts))
	}
	for _, want := range []string{"职场悬疑", "新阿澄", "钥匙交给你"} {
		if !strings.Contains(stream.prompts[0], want) {
			t.Fatalf("已采用快照缺少 %s", want)
		}
	}
}
