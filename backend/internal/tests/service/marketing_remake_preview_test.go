package service_test

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"video-canvas/internal/marketing"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/repository"
	"video-canvas/internal/service"
)

type previewRepo struct {
	service.MarketingRepo
	row repository.MarketingPreviewRow
}

func TestMarketing_RemakePreviewPersisted(t *testing.T) {
	env := newMktEnv(t, 300)
	env.caller.on(marketing.StepFollow, func(int) (string, error) {
		out, err := marketing.DecodeFollow([]byte(followReply(10, "一抹就润")))
		if err != nil {
			return "", err
		}
		out.FollowMap[2].Shot = 0
		raw, err := json.Marshal(out)
		return string(raw), err
	})
	p, round := env.refRound()
	id := uint64(round.Scripts[0].ID)
	current := *round.Reverse
	current.Shots[2].Start = 5.5
	if err := env.repo.UpdateRound(t.Context(), uint64(round.ID), map[string]any{"reverse": []byte(mustJSONText(current))}); err != nil {
		t.Fatal(err)
	}
	first, err := env.svc.RemakePreview(t.Context(), env.user, id)
	if err != nil {
		t.Fatal(err)
	}
	refs := first.Shots[0].Sources
	if !first.BasisChanged || len(refs) != 2 || refs[0].EndMs != 2500 || refs[1].StartMs != 5000 || refs[1].EndMs != 7500 {
		t.Fatalf("持久化的快照区间: %+v", first)
	}
	again, err := env.svc.RemakePreview(t.Context(), env.user, id)
	if err != nil || !reflect.DeepEqual(first, again) {
		t.Fatalf("再次查询: %+v, %v", again, err)
	}
	_, err = env.svc.RemakePreview(t.Context(), env.user+1, id)
	wantMktCode(t, err, errcode.ErrMarketingNotFound.Code)
	if err := env.svc.DeleteProject(t.Context(), env.user, uint64(p.ID)); err != nil {
		t.Fatal(err)
	}
	_, err = env.svc.RemakePreview(t.Context(), env.user, id)
	wantMktCode(t, err, errcode.ErrMarketingNotFound.Code)
}

func (r *previewRepo) RemakePreviewRow(_ context.Context, userID, id uint64) (*repository.MarketingPreviewRow, error) {
	if userID != 7 || id != 9 {
		return nil, repository.ErrNotFound
	}
	return &r.row, nil
}

func TestMarketing_RemakePreview(t *testing.T) {
	assetID := uint64(22)
	basis := []byte(`{"shots":[{"id":1,"start":5,"end":7.5}]}`)
	edited := []byte(`{"shots":[{"id":1,"start":3,"end":4}]}`)
	for _, tc := range []struct {
		name                    string
		basis, current, mapping []byte
		reverseRevision         int
		wantStart, wantEnd      int
		partial, changed        bool
	}{
		{"原拆解", basis, basis, []byte(`[{"shot":0,"ref_shot":1}]`), 1, 5000, 7500, false, false},
		{"当前拆解已换", basis, edited, []byte(`[{"shot":0,"ref_shot":1}]`), 2, 5000, 7500, false, true},
		{"历史单镜记录", basis, edited, nil, 2, 5000, 7500, true, true},
		{"只有初版拆解", nil, basis, nil, 1, 5000, 7500, true, false},
		{"缺快照且拆解已改", nil, edited, nil, 2, 0, 0, true, false},
		{"找不到原镜", basis, basis, []byte(`[{"shot":0,"ref_shot":8}]`), 1, 0, 0, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &previewRepo{row: repository.MarketingPreviewRow{Kind: marketing.KindFollow, Revision: 4, RefVideoAssetID: &assetID,
				Shots: []byte(`[{"start":0,"end":2,"ref_shot":1}]`), FollowMap: tc.mapping, FollowBasis: tc.basis, Reverse: tc.current, ReverseRevision: tc.reverseRevision}}
			svc := service.NewMarketingService(service.MarketingDeps{Repo: repo})
			got, err := svc.RemakePreview(t.Context(), 7, 9)
			if err != nil {
				t.Fatal(err)
			}
			if got.Revision != 4 || got.RefVideoAssetID == nil || *got.RefVideoAssetID != 22 || got.PartialMapping != tc.partial || got.BasisChanged != tc.changed || len(got.Shots) != 1 || got.Shots[0].DurationMs != 2000 {
				t.Fatalf("预览: %+v", got)
			}
			refs := got.Shots[0].Sources
			if tc.wantEnd == 0 {
				if refs == nil || len(refs) != 0 {
					t.Fatalf("应待关联: %+v", refs)
				}
			} else if len(refs) != 1 || refs[0].StartMs != tc.wantStart || refs[0].EndMs != tc.wantEnd {
				t.Fatalf("来源: %+v", refs)
			}
		})
	}
}

func TestMarketing_RemakePreviewErrors(t *testing.T) {
	svc := service.NewMarketingService(service.MarketingDeps{Repo: &previewRepo{row: repository.MarketingPreviewRow{Kind: marketing.KindCreate}}})
	for _, tc := range []struct {
		user, id uint64
		code     int
	}{{7, 9, errcode.ErrInvalidParams.Code}, {8, 9, errcode.ErrMarketingNotFound.Code}, {7, 10, errcode.ErrMarketingNotFound.Code}} {
		_, err := svc.RemakePreview(t.Context(), tc.user, tc.id)
		wantMktCode(t, err, tc.code)
	}
}
