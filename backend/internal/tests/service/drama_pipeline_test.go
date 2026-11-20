package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"video-canvas/internal/drama"
	"video-canvas/internal/llmgateway"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/provider"
	"video-canvas/internal/provider/modelcfg"
	"video-canvas/internal/repository"
	"video-canvas/internal/service"
)

type dramaMemory struct {
	repository.DramaTx
	drafts     map[string]model.DramaSourceDraft
	units      []model.DramaUnit
	prompts    map[uint64]model.DramaPrompt
	project    model.DramaProject
	episodes   []model.DramaEpisode
	jobs       []model.DramaJob
	credit     model.UserCredit
	ledger     []model.CreditLedger
	nextID     uint64
	segments   []model.DramaSegment
	hasTakes   bool
	replaceErr error
	durations  map[string][]int // 按步骤给模型 9 的历史耗时（秒）
	mu         *sync.Mutex      // 并发拆解批次时由 lockedDramaMemory 和假画布共用
}

// lockedDramaMemory 让并发的拆解批次安全地读写内存仓储：事务和事务外的单次读取共用一把锁，事务内的调用直接落到内层。
type lockedDramaMemory struct{ *dramaMemory }

func (l lockedDramaMemory) WithTx(ctx context.Context, fn func(repository.DramaTx) error) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.dramaMemory.WithTx(ctx, fn)
}
func (l lockedDramaMemory) Job(ctx context.Context, userID, id, jobID uint64, forUpdate bool) (*model.DramaJob, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.dramaMemory.Job(ctx, userID, id, jobID, forUpdate)
}
func (l lockedDramaMemory) Jobs(ctx context.Context, userID, id uint64, active bool) ([]model.DramaJob, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.dramaMemory.Jobs(ctx, userID, id, active)
}
func (l lockedDramaMemory) Project(ctx context.Context, userID, id uint64, forUpdate bool) (*model.DramaProject, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.dramaMemory.Project(ctx, userID, id, forUpdate)
}
func (l lockedDramaMemory) Episodes(ctx context.Context, userID, id uint64) ([]model.DramaEpisode, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.dramaMemory.Episodes(ctx, userID, id)
}

func newDramaMemory() *dramaMemory {
	return &dramaMemory{project: model.DramaProject{BaseModel: model.BaseModel{ID: 1}, UserID: 7, CanvasID: 2, Title: "测试", SourceText: "第一章\n小说正文", SourceKind: "novel", SourceSummary: "原文摘要", StyleKey: "custom", StyleCustom: "写实", Revision: 1}, credit: model.UserCredit{UserID: 7, Balance: 1000}, nextID: 10, mu: &sync.Mutex{}}
}
func (r *dramaMemory) WithTx(ctx context.Context, fn func(repository.DramaTx) error) error {
	before := *r
	before.episodes = append([]model.DramaEpisode{}, r.episodes...)
	before.jobs = append([]model.DramaJob{}, r.jobs...)
	before.ledger = append([]model.CreditLedger{}, r.ledger...)
	before.segments = append([]model.DramaSegment{}, r.segments...)
	if err := fn(r); err != nil {
		*r = before
		return err
	}
	return nil
}
func (r *dramaMemory) List(_ context.Context, userID uint64, _ string, _, _ int) ([]model.DramaProject, int64, error) {
	if userID != r.project.UserID {
		return []model.DramaProject{}, 0, nil
	}
	return []model.DramaProject{r.project}, 1, nil
}
func (r *dramaMemory) Project(_ context.Context, userID, id uint64, _ bool) (*model.DramaProject, error) {
	if r.project.UserID != userID || r.project.ID != id || r.project.DeletedAt.Valid {
		return nil, repository.ErrNotFound
	}
	p := r.project
	return &p, nil
}
func (r *dramaMemory) InsertProject(_ context.Context, p *model.DramaProject) error {
	p.ID = r.nextID
	r.nextID++
	p.CanvasID = r.nextID
	r.nextID++
	p.CreatedAt = time.Now()
	p.UpdatedAt = p.CreatedAt
	r.project = *p
	return nil
}
func (r *dramaMemory) UpdateProject(_ context.Context, userID, id uint64, revision int64, fields map[string]any) error {
	if userID != r.project.UserID || id != r.project.ID {
		return repository.ErrNotFound
	}
	if revision != r.project.Revision {
		return repository.ErrStateConflict
	}
	for k, v := range fields {
		switch k {
		case "phase":
			r.project.Phase = v.(string)
		case "plan_cursor":
			r.project.PlanCursor = v.(int)
		case "range_end":
			r.project.RangeEnd = v.(int)
		case "pause_reason":
			r.project.PauseReason = v.(string)
		case "pause_code":
			r.project.PauseCode = v.(string)
		case "prompt_settings":
			r.project.PromptSettings = v.([]byte)
		case "source_kind":
			r.project.SourceKind = v.(string)
		case "source_summary":
			r.project.SourceSummary = v.(string)
		case "characters_md":
			r.project.CharactersMD = v.(string)
		case "outline_md":
			r.project.OutlineMD = v.(string)
		case "status":
			r.project.Status = v.(string)
		case "ratio":
			r.project.Ratio = v.(string)
		case "title":
			r.project.Title = v.(string)
		case "text_model":
			r.project.TextModel = v.(string)
		case "video_model":
			r.project.VideoModel = v.(string)
		case "image_models":
			r.project.ImageModels = v.([]byte)
		case "image_params":
			r.project.ImageParams = v.([]byte)
		}
	}
	r.project.Revision++
	return nil
}
func (r *dramaMemory) DeleteProject(_ context.Context, _, _ uint64) error {
	r.project.DeletedAt.Valid = true
	return nil
}
func (r *dramaMemory) Episodes(_ context.Context, userID, id uint64) ([]model.DramaEpisode, error) {
	if userID != r.project.UserID || id != r.project.ID {
		return nil, repository.ErrNotFound
	}
	return append([]model.DramaEpisode{}, r.episodes...), nil
}
func (r *dramaMemory) SaveEpisode(_ context.Context, e *model.DramaEpisode, revision int64) error {
	for i := range r.episodes {
		if r.episodes[i].ID == e.ID && e.ID != 0 {
			if r.episodes[i].Revision != revision {
				return repository.ErrStateConflict
			}
			e.Revision++
			r.episodes[i] = *e
			return nil
		}
	}
	r.nextID++
	e.ID = r.nextID
	r.episodes = append(r.episodes, *e)
	return nil
}
func (r *dramaMemory) Segments(_ context.Context, userID, episodeID uint64) ([]model.DramaSegment, error) {
	out := []model.DramaSegment{}
	for _, segment := range r.segments {
		if segment.UserID == userID && segment.EpisodeID == episodeID {
			out = append(out, segment)
		}
	}
	return out, nil
}
func (r *dramaMemory) ReplaceSegments(_ context.Context, userID, episodeID uint64, segments []model.DramaSegment) error {
	if r.replaceErr != nil {
		return r.replaceErr
	}
	out := []model.DramaSegment{}
	for _, segment := range r.segments {
		if segment.UserID != userID || segment.EpisodeID != episodeID {
			out = append(out, segment)
		}
	}
	for _, segment := range segments {
		r.nextID++
		segment.ID = r.nextID
		out = append(out, segment)
	}
	r.segments = out
	return nil
}
func (r *dramaMemory) HasSegmentTakes(context.Context, uint64, uint64) (bool, error) {
	return r.hasTakes, nil
}
func (r *dramaMemory) Summary(ctx context.Context, userID, episodeID uint64, _ map[string]string) (model.DramaSegmentStats, error) {
	var stats model.DramaSegmentStats
	segments, err := r.Segments(ctx, userID, episodeID)
	for _, segment := range segments {
		stats.Count++
		stats.Seconds += segment.Seconds
		if segment.ChosenTaskID != nil {
			stats.Done++
		}
	}
	return stats, err
}
func (r *dramaMemory) GetByID(_ context.Context, userID, canvasID uint64) (*model.CanvasProject, error) {
	if userID != r.project.UserID || canvasID != r.project.CanvasID {
		return nil, repository.ErrNotFound
	}
	return &model.CanvasProject{PayloadJSON: []byte(`{"nodes":[{"id":"n_scene","data":{"drama":{"type":"scene","name":"村口","look":"夜","episodes":[]},"params":{"prompt":"夜间村口灯光"}}},{"id":"n_other","data":{"drama":{"type":"prop","name":"其他集","look":"","episodes":[2]},"params":{"prompt":"不应输入"}}},{"id":"n_orphan","data":{"drama":{"type":"prop","name":"未分类道具","look":"","episodes":null},"params":{"prompt":"未分类不应输入"}}}]}`)}, nil
}
func (r *dramaMemory) Jobs(_ context.Context, userID, id uint64, active bool) ([]model.DramaJob, error) {
	if userID != r.project.UserID || id != r.project.ID {
		return nil, repository.ErrNotFound
	}
	out := []model.DramaJob{}
	for _, j := range r.jobs {
		if !active || j.Status == "queued" || j.Status == "running" {
			out = append(out, j)
		}
	}
	return out, nil
}
func (r *dramaMemory) Job(_ context.Context, userID, id, jobID uint64, _ bool) (*model.DramaJob, error) {
	if userID != r.project.UserID || id != r.project.ID {
		return nil, repository.ErrNotFound
	}
	for _, j := range r.jobs {
		if j.ID == jobID {
			return &j, nil
		}
	}
	return nil, repository.ErrNotFound
}
func (r *dramaMemory) InsertJob(_ context.Context, j *model.DramaJob) error {
	r.nextID++
	j.ID = r.nextID
	j.CreatedAt = time.Now()
	r.jobs = append(r.jobs, *j)
	return nil
}
func (r *dramaMemory) UpdateJob(_ context.Context, userID, id uint64, fields map[string]any) error {
	for i := range r.jobs {
		j := &r.jobs[i]
		if j.ID != id || j.UserID != userID {
			continue
		}
		for k, v := range fields {
			switch k {
			case "status":
				j.Status = v.(string)
			case "progress":
				j.Progress = v.(int)
			case "credits":
				j.Credits = v.(int)
			case "error_code":
				j.ErrorCode = v.(int)
			case "error_message":
				j.ErrorMessage = v.(string)
			case "finished_at":
				now := v.(time.Time)
				j.FinishedAt = &now
			case "started_at":
				now := v.(time.Time)
				j.StartedAt = &now
			case "output":
				j.Output = v.([]byte)
			case "input_json":
				j.InputJSON = v.([]byte)
			}
		}
		return nil
	}
	return repository.ErrNotFound
}
func (r *dramaMemory) EnsureCredit(context.Context, uint64, int) error { return nil }
func (r *dramaMemory) LockCredit(context.Context, uint64) (*model.UserCredit, error) {
	c := r.credit
	return &c, nil
}
func (r *dramaMemory) AddCredit(_ context.Context, _ uint64, balance, frozen int) error {
	r.credit.Balance += balance
	r.credit.Frozen += frozen
	return nil
}
func (r *dramaMemory) InsertLedger(_ context.Context, e *model.CreditLedger) (bool, error) {
	for _, old := range r.ledger {
		if old.DramaJobID != nil && e.DramaJobID != nil && *old.DramaJobID == *e.DramaJobID && old.Type == e.Type {
			return false, nil
		}
	}
	r.ledger = append(r.ledger, *e)
	return true, nil
}

type dramaRegistry struct {
	snap        *provider.Snapshot
	models      []provider.ModelInfo
	unavailable map[string]bool
	requested   []string
	// snaps 按模型 key 返回专属快照（视频 / 图片模型）；没登记的 key 回落到 snap
	snaps map[string]*provider.Snapshot
}

func (r *dramaRegistry) ListModels(_ context.Context, kind string) ([]provider.ModelInfo, error) {
	var out []provider.ModelInfo
	for _, m := range r.models {
		if m.Kind == kind {
			out = append(out, m)
		}
	}
	return out, nil
}
func (r *dramaRegistry) Snapshot(_ context.Context, key string) (*provider.Snapshot, error) {
	r.requested = append(r.requested, key)
	if r.unavailable[key] {
		return nil, provider.ErrModelUnavailable
	}
	if snap, ok := r.snaps[key]; ok {
		return snap, nil
	}
	return r.snap, nil
}

// dramaTextStreamer 冒充 LLM 网关：按顺序吐出预置文本，并记下每次请求的目标渠道与提示词。
type dramaTextStreamer struct {
	mu      sync.Mutex
	outputs []string
	prompts []string
	targets []llmgateway.Target
	usage   llmgateway.Usage
	calls   int
	failAt  int
	failErr error // failAt 那次返回的错误，空时返回一个普通错误
	onCall  func()
	delay   time.Duration // 每次调用先等这么久再出结果，期间 ctx 结束就返回 ctx 的错误
	// respond 设置后按提示词作答（并发调用时顺序不定），不再使用 outputs / failAt
	respond func(ctx context.Context, prompt string) (*llmgateway.Result, error)
}

func (e *dramaTextStreamer) Stream(ctx context.Context, tg llmgateway.Target, req llmgateway.Request, _ func(llmgateway.Event)) (*llmgateway.Result, error) {
	e.mu.Lock()
	e.calls++
	call := e.calls
	e.targets = append(e.targets, tg)
	e.prompts = append(e.prompts, req.Messages[0].Text)
	e.mu.Unlock()
	if e.onCall != nil {
		e.onCall()
	}
	if e.delay > 0 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(e.delay):
		}
	}
	if e.respond != nil {
		return e.respond(ctx, req.Messages[0].Text)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if call == e.failAt {
		if e.failErr != nil {
			return nil, e.failErr
		}
		return nil, errors.New("上游失败")
	}
	if len(e.outputs) == 0 {
		return nil, errors.New("测试未提供输出")
	}
	text := e.outputs[0]
	e.outputs = e.outputs[1:]
	return &llmgateway.Result{Text: text, Usage: e.usage, UsageMissing: e.usage == llmgateway.Usage{}}, nil
}

type dramaSecrets map[string]string

func (s dramaSecrets) Get(_ context.Context, name string) (string, error) {
	if v, ok := s[name]; ok {
		return v, nil
	}
	return "", errors.New("没有这个密钥")
}

type dramaPipelineCanvas struct {
	mem      *dramaMemory // 设置后像真实画布服务一样，在同一把锁里把进度累加进任务检查点
	applied  int
	prompted int
	created  []service.DramaCanvasAsset // 非空时每批都返回它；为空时按资产名生成节点 node-<名字>
	models   map[string]string
	params   map[string]map[string]string
	assets   []drama.Asset     // 各批收到的资产，按提交顺序
	prompts  map[string]string // 已写入的节点提示词
	filtered []service.DramaCanvasAsset
	// deleted 记录删集调用：短剧 id、画布 id、集号
	deleted [][3]uint64
	// synced 记录故事板同步调用；syncErr 非空时同步返回它
	synced  []boardSync
	syncErr error
}

// boardSync 是一次故事板同步调用的参数。
type boardSync struct {
	canvasID  uint64
	episodeNo int
	segments  []service.DramaBoardSegment
}

func (c *dramaPipelineCanvas) SyncBoards(_ context.Context, _, canvasID uint64, episodeNo int, segments []service.DramaBoardSegment) error {
	if c == nil { // 很多测试给服务传 nil 画布（装进接口后不是 nil 接口），不关心故事板
		return nil
	}
	c.synced = append(c.synced, boardSync{canvasID: canvasID, episodeNo: episodeNo, segments: segments})
	return c.syncErr
}

func (c *dramaPipelineCanvas) DeleteEpisode(_ context.Context, _, dramaID, canvasID uint64, no int) error {
	c.deleted = append(c.deleted, [3]uint64{dramaID, canvasID, uint64(no)})
	return nil
}

func (c *dramaPipelineCanvas) ApplyBreakdown(_ context.Context, _, _, _, jobID uint64, batch service.DramaBreakdownBatch) ([]service.DramaCanvasAsset, error) {
	unlock := c.lock()
	defer unlock()
	c.applied++
	c.assets = append(c.assets, batch.Assets...)
	c.models = batch.Models
	c.params = batch.Params
	created := c.created
	if created == nil {
		for _, a := range batch.Assets {
			created = append(created, service.DramaCanvasAsset{NodeID: "node-" + a.Name, Asset: a})
		}
	}
	c.addProgress(jobID, func(cp map[string]any) {
		cp["done"] = append(jsonInts(cp["done"]), batch.Episodes...)
		cp["new_assets"] = append(jsonList(cp["new_assets"]), toJSONList(created)...)
		cp["cost"] = jsonInt(cp["cost"]) + batch.Cost
	})
	return created, nil
}
func (c *dramaPipelineCanvas) ApplyPrompts(_ context.Context, _, _, jobID uint64, _ string, prompts []drama.AssetPrompt, filtered []service.DramaCanvasAsset, cost int) error {
	unlock := c.lock()
	defer unlock()
	c.prompted++
	if c.prompts == nil {
		c.prompts = map[string]string{}
	}
	ids := []any{}
	for _, p := range prompts {
		c.prompts[p.NodeID] = p.Prompt
		ids = append(ids, p.NodeID)
	}
	c.filtered = append(c.filtered, filtered...)
	c.addProgress(jobID, func(cp map[string]any) {
		cp["prompted"] = append(jsonList(cp["prompted"]), ids...)
		cp["filtered"] = append(jsonList(cp["filtered"]), toJSONList(filtered)...)
		cp["cost"] = jsonInt(cp["cost"]) + cost
	})
	return nil
}

// lock 和内存仓储共用一把锁，模拟画布事务与任务行锁。
func (c *dramaPipelineCanvas) lock() func() {
	if c.mem == nil {
		return func() {}
	}
	c.mem.mu.Lock()
	return c.mem.mu.Unlock
}

// addProgress 把一次画布提交的进度合并进任务 output，字段与真实检查点一致。
func (c *dramaPipelineCanvas) addProgress(jobID uint64, edit func(map[string]any)) {
	if c.mem == nil {
		return
	}
	for i := range c.mem.jobs {
		if c.mem.jobs[i].ID != jobID {
			continue
		}
		cp := map[string]any{}
		_ = json.Unmarshal(c.mem.jobs[i].Output, &cp)
		edit(cp)
		c.mem.jobs[i].Output, _ = json.Marshal(cp)
	}
}

func jsonList(v any) []any {
	list, _ := v.([]any)
	return list
}
func jsonInt(v any) int {
	f, _ := v.(float64)
	return int(f)
}
func jsonInts(v any) []int {
	out := []int{}
	for _, x := range jsonList(v) {
		out = append(out, jsonInt(x))
	}
	return out
}
func toJSONList(v any) []any {
	raw, _ := json.Marshal(v)
	out := []any{}
	_ = json.Unmarshal(raw, &out)
	return out
}
func dramaTestService(r *dramaMemory, e *dramaTextStreamer, c *dramaPipelineCanvas) (*service.DramaService, *dramaRegistry) {
	snap := &provider.Snapshot{ModelRevisionID: 9, Channel: provider.ChannelSnapshot{Key: "chan", BaseURL: "https://llm.example"}, Model: provider.ModelSnapshot{Key: "text", Kind: "agent", UpstreamModel: "upstream-chat", APIFormat: "openai", Pricing: modelcfg.Pricing{Billing: modelcfg.BillingPerCall, Unit: 5}, Capabilities: modelcfg.Capabilities{Context: &modelcfg.ContextSpec{Window: 128000, Output: 5000}}}}
	registry := &dramaRegistry{snap: snap, models: []provider.ModelInfo{{Key: "text", Kind: "agent"}}}
	secrets := dramaSecrets{model.ChannelSecretName("chan"): "sk-test"}
	return service.NewDramaService(service.DramaDeps{Repo: lockedDramaMemory{r}, Settings: r, Registry: registry, Streamer: e, Secrets: secrets, Canvas: c, CanvasAssets: r, Segments: r}), registry
}
func dramaCheckCode(t *testing.T, err error, code int) {
	t.Helper()
	var business *errcode.Error
	if !errors.As(err, &business) || business.Code != code {
		t.Fatalf("want code %d, got %v", code, err)
	}
}
func dramaAddEpisode(r *dramaMemory, no int, script string) {
	r.nextID++
	status := "empty"
	if script != "" {
		status = "ready"
	}
	r.episodes = append(r.episodes, model.DramaEpisode{BaseModel: model.BaseModel{ID: r.nextID}, UserID: 7, DramaID: 1, No: no, Title: fmt.Sprintf("第%d集", no), ScriptMD: script, ScriptStatus: status, Revision: 1})
}

// 剧本只有创作段：创作稿原样就是本集剧本；空白输出算不合格，带校验反馈重试一次，不合格的调用不收费。
func TestDramaPipelineJSONRetryAndNoCharge(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		outputs              []string
		status               string
		code, calls, balance int
	}{{"成功", []string{"△ 剧本正文"}, "succeeded", 0, 1, 995}, {"空白重试成功", []string{" ", "△ 修正后的正文"}, "succeeded", 0, 2, 995}, {"两次空白失败", []string{" ", "\n"}, "failed", 67008, 2, 1000}} {
		t.Run(tc.name, func(t *testing.T) {
			r := newDramaMemory()
			dramaAddEpisode(r, 1, "")
			e := &dramaTextStreamer{outputs: tc.outputs}
			s, _ := dramaTestService(r, e, nil)
			j, err := s.CreateJob(context.Background(), 7, 1, &model.DramaJobReq{Step: "scripts", Episodes: []int{1}})
			if err != nil {
				t.Fatal(err)
			}
			if err := s.HandleDrama(context.Background(), 7, 1, uint64(j.ID)); err != nil {
				t.Fatal(err)
			}
			got := r.jobs[0]
			if got.Status != tc.status || got.ErrorCode != tc.code || e.calls != tc.calls {
				t.Fatalf("job=%+v calls=%d", got, e.calls)
			}
			if r.credit.Balance != tc.balance || r.credit.Frozen != 0 {
				t.Fatalf("account=%+v", r.credit)
			}
			if tc.calls == 2 && !strings.Contains(e.prompts[1], "validation_error") {
				t.Fatal("重试缺少校验反馈")
			}
			// 不再有格式段改写：保存的剧本就是创作稿原文
			if tc.status == "succeeded" && r.episodes[0].ScriptMD != tc.outputs[len(tc.outputs)-1] {
				t.Fatalf("剧本应原样保存创作稿，got %q", r.episodes[0].ScriptMD)
			}
		})
	}
}
func TestDramaJobPrerequisitesBusyAndIsolation(t *testing.T) {
	r := newDramaMemory()
	s, registry := dramaTestService(r, &dramaTextStreamer{}, nil)
	_, err := s.CreateJob(context.Background(), 7, 1, &model.DramaJobReq{Step: "scripts"})
	dramaCheckCode(t, err, 67006)
	_, err = s.Get(context.Background(), 8, 1)
	dramaCheckCode(t, err, 67001)
	dramaAddEpisode(r, 1, "")
	_, err = s.CreateJob(context.Background(), 7, 1, &model.DramaJobReq{Step: "breakdown", Episodes: []int{1}})
	dramaCheckCode(t, err, 67006)
	j, err := s.CreateJob(context.Background(), 7, 1, &model.DramaJobReq{Step: "scripts"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.CreateJob(context.Background(), 7, 1, &model.DramaJobReq{Step: "scripts"})
	dramaCheckCode(t, err, 67004)
	_, err = s.CancelJob(context.Background(), 8, 1, uint64(j.ID))
	dramaCheckCode(t, err, 67001)
	if _, err = s.CancelJob(context.Background(), 7, 1, uint64(j.ID)); err != nil {
		t.Fatal(err)
	}
	if r.credit.Frozen != 0 || r.credit.Balance != 1000 {
		t.Fatalf("cancel account=%+v", r.credit)
	}
	registry.models = nil
	_, err = s.Estimate(context.Background(), 7, 1, &model.DramaJobReq{Step: "scripts"})
	dramaCheckCode(t, err, 67007)
}
func TestDramaEpisodeLockedAndRevision(t *testing.T) {
	r := newDramaMemory()
	dramaAddEpisode(r, 1, "正文")
	now := time.Now()
	r.episodes[0].LockedAt = &now
	s, _ := dramaTestService(r, &dramaTextStreamer{}, nil)
	text := "修改"
	for _, req := range []*model.UpdateDramaEpisodeReq{{Revision: 1, ScriptMD: &text}, {Revision: 1, Synopsis: &text}} {
		_, err := s.UpdateEpisode(context.Background(), 7, 1, 1, req)
		dramaCheckCode(t, err, 67005)
	}
	_, err := s.UpdateEpisode(context.Background(), 7, 1, 1, &model.UpdateDramaEpisodeReq{Revision: 2, Title: &text})
	dramaCheckCode(t, err, 30002)
	view, err := s.UpdateEpisode(context.Background(), 7, 1, 1, &model.UpdateDramaEpisodeReq{Revision: 1, Title: &text})
	if err != nil {
		t.Fatal(err)
	}
	if view.Title != text || view.Revision != 2 || !view.Locked {
		t.Fatalf("view=%+v", view)
	}
	_, err = s.UpdateEpisode(context.Background(), 8, 1, 1, &model.UpdateDramaEpisodeReq{Revision: 2, Title: &text})
	dramaCheckCode(t, err, 67001)
}
func TestDramaScriptsSerialContinuationAndPartialSuccess(t *testing.T) {
	r := newDramaMemory()
	dramaAddEpisode(r, 1, "")
	dramaAddEpisode(r, 2, "")
	dramaAddEpisode(r, 3, "")
	ending := strings.Repeat("甲", 900) + "最后一句"
	// 第一集一次调用；第二集（第 2 次调用）失败
	e := &dramaTextStreamer{outputs: []string{ending}, failAt: 2}
	s, _ := dramaTestService(r, e, nil)
	j, err := s.CreateJob(context.Background(), 7, 1, &model.DramaJobReq{Step: "scripts"})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.HandleDrama(context.Background(), 7, 1, uint64(j.ID)); err != nil {
		t.Fatal(err)
	}
	if r.episodes[0].ScriptStatus != "ready" || r.episodes[0].ScriptMD != ending || r.episodes[1].ScriptStatus != "failed" || r.episodes[2].ScriptStatus != "empty" {
		t.Fatalf("episodes=%+v", r.episodes)
	}
	if !strings.Contains(e.prompts[1], "最后一句") || strings.Contains(e.prompts[1], strings.Repeat("甲", 900)) {
		t.Fatal("衔接没有限定为上一集结尾约800字")
	}
	if r.credit.Balance != 1000 || r.credit.Frozen != 0 {
		t.Fatalf("failed job charged %+v", r.credit)
	}
}

// 文本模型返回错误时，任务的失败原因是上游原文，不是“服务器内部错误”。
func TestDramaJobFailureShowsUpstreamError(t *testing.T) {
	r := newDramaMemory()
	dramaAddEpisode(r, 1, "")
	e := &dramaTextStreamer{failAt: 1, failErr: &llmgateway.UpstreamError{Status: 400, Message: "The model gpt-up does not exist"}}
	s, _ := dramaTestService(r, e, nil)
	j, err := s.CreateJob(context.Background(), 7, 1, &model.DramaJobReq{Step: "scripts"})
	if err != nil {
		t.Fatal(err)
	}
	_ = s.HandleDrama(context.Background(), 7, 1, uint64(j.ID))
	got := r.jobs[len(r.jobs)-1]
	if got.Status != "failed" || got.ErrorCode != errcode.ErrDramaModelCall.Code || got.ErrorMessage != "The model gpt-up does not exist" {
		t.Fatalf("任务应带上游原文失败：%s %d %q", got.Status, got.ErrorCode, got.ErrorMessage)
	}
	if r.credit.Balance != 1000 || r.credit.Frozen != 0 {
		t.Fatalf("失败不扣费：%+v", r.credit)
	}
}
func TestDramaRejectsUserAnalyzeAndOutline(t *testing.T) {
	r := newDramaMemory()
	s, _ := dramaTestService(r, &dramaTextStreamer{}, nil)
	for _, step := range []string{"analyze", "outline", "plan"} {
		_, err := s.CreateJob(context.Background(), 7, 1, &model.DramaJobReq{Step: step})
		dramaCheckCode(t, err, 10001)
	}
}
func TestDramaModelFallbackAndEstimate(t *testing.T) {
	r := newDramaMemory()
	r.project.TextModel = "disabled"
	dramaAddEpisode(r, 1, "")
	dramaAddEpisode(r, 2, "")
	s, registry := dramaTestService(r, &dramaTextStreamer{}, nil)
	registry.models = []provider.ModelInfo{{Key: "unavailable", Kind: "agent"}, {Key: "text", Kind: "agent"}}
	registry.unavailable = map[string]bool{"unavailable": true}
	credits, err := s.Estimate(context.Background(), 7, 1, &model.DramaJobReq{Step: "scripts"})
	if err != nil {
		t.Fatal(err)
	}
	// 两集，每集两次调用（创作段、格式段）各 5 点
	if credits != 20 || len(registry.requested) != 2 || registry.requested[1] != "text" {
		t.Fatalf("estimate=%d requested=%v", credits, registry.requested)
	}
	r.credit.Balance = 0
	_, err = s.CreateJob(context.Background(), 7, 1, &model.DramaJobReq{Step: "scripts"})
	dramaCheckCode(t, err, 40001)
	if len(r.jobs) != 0 {
		t.Fatal("余额不足不得入队")
	}
}

// dramaFakeModel 按提示词作答：拆解调用给这一批的第一集出资产（默认一个「角色<集号>」），提示词调用给每个输入节点写一条；
// 提示词调用里出现 filtered 中的资产时，整次调用按安全过滤截断返回。并发调用安全，inFlight / peak 记录同时在途的拆解调用数。
type dramaFakeModel struct {
	mu       sync.Mutex
	names    func(first int) []string
	filtered map[string]bool
	before   func(ctx context.Context, first int) error // 拆解调用作答前执行，返回错误就以它失败
	hold     time.Duration                              // 拆解调用在途时停留这么久，用来观察并行
	inFlight int
	peak     int
}

var (
	dramaNodeRe    = regexp.MustCompile(`"node_id":\s*"(node-[^"]+)"`)
	dramaEpisodeRe = regexp.MustCompile(`第(\d+)集正文`)
	// 资产拆解格式段只带集号范围，没有剧本正文
	dramaEpisodeNosRe = regexp.MustCompile(`"episode_numbers":\[(\d+)`)
)

func (m *dramaFakeModel) filter(names ...string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.filtered = map[string]bool{}
	for _, n := range names {
		m.filtered["node-"+n] = true
	}
}

func (m *dramaFakeModel) respond(ctx context.Context, prompt string) (*llmgateway.Result, error) {
	if ids := dramaNodeRe.FindAllStringSubmatch(prompt, -1); len(ids) > 0 {
		m.mu.Lock()
		defer m.mu.Unlock()
		out := drama.AssetPromptsOutput{}
		seen := map[string]bool{}
		for _, id := range ids {
			if seen[id[1]] {
				continue
			}
			seen[id[1]] = true
			if m.filtered[id[1]] {
				return &llmgateway.Result{Text: `{"prompts":[{"node_id":"`, FinishReason: "content_filter"}, nil
			}
			out.Prompts = append(out.Prompts, drama.AssetPrompt{NodeID: id[1], Prompt: "提示词：" + id[1]})
		}
		raw, _ := json.Marshal(out)
		return &llmgateway.Result{Text: string(raw)}, nil
	}
	first := 0
	if m := dramaEpisodeRe.FindStringSubmatch(prompt); m != nil {
		first, _ = strconv.Atoi(m[1])
	} else if m := dramaEpisodeNosRe.FindStringSubmatch(prompt); m != nil {
		first, _ = strconv.Atoi(m[1])
	}
	m.mu.Lock()
	m.inFlight++
	m.peak = max(m.peak, m.inFlight)
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		m.inFlight--
		m.mu.Unlock()
	}()
	time.Sleep(m.hold)
	if m.before != nil {
		if err := m.before(ctx, first); err != nil {
			return nil, err
		}
	}
	names := []string{fmt.Sprintf("角色%d", first)}
	if m.names != nil {
		names = m.names(first)
	}
	out := drama.BreakdownOutput{Assets: []drama.Asset{}}
	for _, n := range names {
		out.Assets = append(out.Assets, drama.Asset{Type: "character", Name: n, Aliases: []string{}, Episodes: []int{first}, Description: "人物"})
	}
	raw, _ := json.Marshal(out)
	return &llmgateway.Result{Text: string(raw)}, nil
}

// dramaBreakdownJob 建 n 集带剧本的短剧并创建全集拆解任务；超过 12 集会分成多批调用模型。
func dramaBreakdownJob(t *testing.T, n int, e *dramaTextStreamer, deadline time.Duration) (*service.DramaService, *dramaMemory, *dramaPipelineCanvas, uint64) {
	t.Helper()
	r := newDramaMemory()
	r.project.Ratio = "9:16"
	episodes := []int{}
	for no := 1; no <= n; no++ {
		dramaAddEpisode(r, no, fmt.Sprintf("第%d集正文", no))
		episodes = append(episodes, no)
	}
	c := &dramaPipelineCanvas{mem: r}
	s, registry := dramaTestService(r, e, c)
	registry.snap.Model.Deadline = modelcfg.Duration(deadline)
	j, err := s.CreateJob(context.Background(), 7, 1, &model.DramaJobReq{Step: "breakdown", Episodes: episodes})
	if err != nil {
		t.Fatal(err)
	}
	return s, r, c, uint64(j.ID)
}

// prompted 读出假画布上已写入的提示词节点。
func (c *dramaPipelineCanvas) hasPrompt(id string) bool {
	unlock := c.lock()
	defer unlock()
	_, ok := c.prompts[id]
	return ok
}

// waitDrama 轮询直到 cond 成立，超时判失败。
func waitDrama(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("等待超时")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// 模型的 deadline 限的是单次调用：多批拆解加起来超过它也要能跑完，不能每次都在半路超时重来。
func TestDramaBreakdownDeadlineLimitsEachCallNotWholeJob(t *testing.T) {
	m := &dramaFakeModel{}
	e := &dramaTextStreamer{respond: m.respond, delay: 150 * time.Millisecond}
	s, r, c, jobID := dramaBreakdownJob(t, 25, e, 250*time.Millisecond)
	if err := s.HandleDrama(context.Background(), 7, 1, jobID); err != nil {
		t.Fatal(err)
	}
	if got := r.jobs[0]; got.Status != "succeeded" || len(c.assets) != 3 {
		t.Fatalf("三批拆解加提示词各 150ms、单次时限 250ms 应跑完：status=%s assets=%d err=%q", got.Status, len(c.assets), got.ErrorMessage)
	}
}

// 单次调用超过 deadline：任务直接失败退款，告诉用户是模型响应超时，不交给队列反复重试。
func TestDramaCallTimeoutFailsJobWithTimeoutMessage(t *testing.T) {
	e := &dramaTextStreamer{respond: (&dramaFakeModel{}).respond, delay: time.Second}
	s, r, _, jobID := dramaBreakdownJob(t, 1, e, 50*time.Millisecond)
	if err := s.HandleDrama(context.Background(), 7, 1, jobID); err != nil {
		t.Fatalf("超时应作为业务失败收尾，不返回错误让队列重试：%v", err)
	}
	got := r.jobs[0]
	if got.Status != "failed" || got.ErrorCode != errcode.ErrDramaModelCall.Code || !strings.Contains(got.ErrorMessage, "超时") {
		t.Fatalf("应以模型超时失败：%s %d %q", got.Status, got.ErrorCode, got.ErrorMessage)
	}
	if r.credit.Balance != 1000 || r.credit.Frozen != 0 {
		t.Fatalf("失败不扣费：%+v", r.credit)
	}
}

// 多批拆解并行调用模型；每批完成就写画布并写好这批资产的提示词，全部完成后不再另起提示词任务，按实际调用扣费。
func TestDramaBreakdownRunsBatchesInParallelAndPromptsEachBatch(t *testing.T) {
	m := &dramaFakeModel{hold: 50 * time.Millisecond}
	e := &dramaTextStreamer{respond: m.respond}
	s, r, c, jobID := dramaBreakdownJob(t, 25, e, 0)
	if err := s.HandleDrama(context.Background(), 7, 1, jobID); err != nil {
		t.Fatal(err)
	}
	got := r.jobs[0]
	if got.Status != "succeeded" || got.Progress != 100 || len(r.jobs) != 1 {
		t.Fatalf("拆解应成功且不另起任务：status=%s progress=%d jobs=%d", got.Status, got.Progress, len(r.jobs))
	}
	if m.peak < 2 {
		t.Fatalf("三批应并行调用模型，同时在途最多 %d 个", m.peak)
	}
	if c.applied != 3 || len(c.prompts) != 3 || c.prompts["node-角色13"] == "" {
		t.Fatalf("三批各写一次画布并写好提示词：applied=%d prompts=%v", c.applied, c.prompts)
	}
	// 每批拆解与提示词各两次调用（创作段、格式段），每次 5 积分
	if got.Credits != 60 || r.credit.Balance != 940 || r.credit.Frozen != 0 {
		t.Fatalf("三次拆解加三次提示词共 60 积分：credits=%d account=%+v", got.Credits, r.credit)
	}
}

// 拆解任务顺带写资产图提示词时，要用项目「资产图提示词」环节的设置，不能沿用「资产拆解」环节的内置正文。
func TestDramaBreakdownUsesAssetPromptsSetting(t *testing.T) {
	m := &dramaFakeModel{}
	e := &dramaTextStreamer{respond: m.respond}
	r := newDramaMemory()
	r.project.Ratio = "9:16"
	r.project.PromptSettings = []byte(`{"asset_prompts":{"source":"mine","id":3,"label":"我的 · 构图","bodies":{"asset_prompts":"自写构图：四画面不对称分栏"}}}`)
	dramaAddEpisode(r, 1, "第1集正文")
	c := &dramaPipelineCanvas{mem: r}
	s, _ := dramaTestService(r, e, c)
	j, err := s.CreateJob(context.Background(), 7, 1, &model.DramaJobReq{Step: "breakdown", Episodes: []int{1}})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.HandleDrama(context.Background(), 7, 1, uint64(j.ID)); err != nil {
		t.Fatal(err)
	}
	if got := r.jobs[0]; got.Status != "succeeded" || len(c.prompts) != 1 {
		t.Fatalf("拆解应成功并写好提示词：status=%s prompts=%v", got.Status, c.prompts)
	}
	var creative string
	for _, p := range e.prompts {
		if strings.Contains(p, "# 资产图提示词创作") || strings.Contains(p, "自写构图") {
			creative = p
		}
		if strings.Contains(p, "# 资产拆解") && strings.Contains(p, "自写构图") {
			t.Fatal("资产图提示词的自定义正文不应混进拆解调用")
		}
	}
	if !strings.Contains(creative, "自写构图：四画面不对称分栏") || strings.Contains(creative, "四视图设定图") {
		t.Fatalf("资产图提示词应使用项目里的自定义正文:\n%s", creative)
	}
}

// 拆解中途被打断（如后端重启）后重跑：已写入画布的批次跳过，只调用剩下的批次，资产不丢也不重复。
func TestDramaBreakdownResumesFromCommittedBatches(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	m := &dramaFakeModel{}
	e := &dramaTextStreamer{respond: m.respond}
	s, r, c, jobID := dramaBreakdownJob(t, 13, e, 0)
	m.before = func(callCtx context.Context, first int) error {
		if first == 13 {
			waitDrama(t, func() bool { return c.hasPrompt("node-角色1") })
			cancel()
			return callCtx.Err()
		}
		return nil
	}
	if err := s.HandleDrama(ctx, 7, 1, jobID); err == nil {
		t.Fatal("第二批被打断，应返回错误交给队列重试")
	}
	if got := r.jobs[0]; got.Status != "running" || c.applied != 1 || got.Progress == 0 {
		t.Fatalf("第一批应已写入画布并推进进度：status=%s applied=%d progress=%d", got.Status, c.applied, got.Progress)
	}
	m.before = nil
	calls := e.calls
	if err := s.HandleDrama(context.Background(), 7, 1, jobID); err != nil {
		t.Fatal(err)
	}
	if e.calls-calls != 4 || c.applied != 2 {
		t.Fatalf("重跑只应调用第二批的拆解和提示词：calls=%d applied=%d", e.calls-calls, c.applied)
	}
	for _, p := range e.prompts[calls:] {
		if strings.Contains(p, "第1集正文") || strings.Contains(p, "node-角色1\"") {
			t.Fatal("重跑不应再处理第一批")
		}
	}
	if got := r.jobs[0]; got.Status != "succeeded" || got.Credits != 40 || len(c.prompts) != 2 {
		t.Fatalf("两批各一次拆解加一次提示词共 40 积分：status=%s credits=%d prompts=%v", got.Status, got.Credits, c.prompts)
	}
}

// 拆解中途取消：已写入画布的批次保留，只扣这些批次的积分，其余退回。
func TestDramaBreakdownCancelKeepsCommittedBatchesAndChargesThem(t *testing.T) {
	m := &dramaFakeModel{}
	e := &dramaTextStreamer{respond: m.respond}
	s, r, c, jobID := dramaBreakdownJob(t, 13, e, 0)
	m.before = func(callCtx context.Context, first int) error {
		if first == 13 {
			waitDrama(t, func() bool { return c.hasPrompt("node-角色1") })
			if _, err := s.CancelJob(context.Background(), 7, 1, jobID); err != nil {
				t.Error(err)
			}
			<-callCtx.Done()
			return callCtx.Err()
		}
		return nil
	}
	if err := s.HandleDrama(context.Background(), 7, 1, jobID); err != nil {
		t.Fatal(err)
	}
	got := r.jobs[0]
	if got.Status != "canceled" || c.applied != 1 || !c.hasPrompt("node-角色1") {
		t.Fatalf("取消后保留第一批：status=%s applied=%d", got.Status, c.applied)
	}
	if got.Credits != 20 || r.credit.Balance != 980 || r.credit.Frozen != 0 {
		t.Fatalf("只扣第一批的拆解和提示词 20 积分：credits=%d account=%+v", got.Credits, r.credit)
	}
}

// 提示词被安全过滤：对半拆开重试，把出问题的资产隔离出来，其余照常写入；拆解成功后为被过滤的资产接一个提示词任务。
func TestDramaPromptsIsolateFilteredAssets(t *testing.T) {
	m := &dramaFakeModel{names: func(int) []string { return []string{"苏晚", "顾北辰", "苏星月", "苏星阳"} }}
	m.filter("苏星月", "苏星阳")
	e := &dramaTextStreamer{respond: m.respond}
	s, r, c, jobID := dramaBreakdownJob(t, 1, e, 0)
	if err := s.HandleDrama(context.Background(), 7, 1, jobID); err != nil {
		t.Fatal(err)
	}
	if !c.hasPrompt("node-苏晚") || !c.hasPrompt("node-顾北辰") || c.hasPrompt("node-苏星月") || len(c.filtered) != 2 {
		t.Fatalf("只有被过滤的两个资产没有提示词：prompts=%v filtered=%v", c.prompts, c.filtered)
	}
	if r.jobs[0].Status != "succeeded" || len(r.jobs) != 2 || r.jobs[1].Step != "asset_prompts" {
		t.Fatalf("拆解成功并接一个提示词任务：jobs=%+v", r.jobs)
	}
	var input []service.DramaCanvasAsset
	if err := json.Unmarshal(r.jobs[1].InputJSON, &input); err != nil || len(input) != 2 {
		t.Fatalf("提示词任务只处理被过滤的资产：%s", r.jobs[1].InputJSON)
	}

	// 接续任务里苏星阳这次通过、苏星月仍被过滤：写入苏星阳，任务以安全过滤失败并只扣成功的那次。
	m.filter("苏星月")
	balance := r.credit.Balance
	if err := s.HandleDrama(context.Background(), 7, 1, r.jobs[1].ID); err != nil {
		t.Fatal(err)
	}
	follow := r.jobs[1]
	if !c.hasPrompt("node-苏星阳") || c.hasPrompt("node-苏星月") {
		t.Fatalf("应写入苏星阳：%v", c.prompts)
	}
	if follow.Status != "failed" || follow.ErrorCode != errcode.ErrDramaContentFiltered.Code || !strings.Contains(follow.ErrorMessage, "苏星月") {
		t.Fatalf("应以安全过滤失败并点名资产：%s %d %q", follow.Status, follow.ErrorCode, follow.ErrorMessage)
	}
	if follow.Credits != 10 || r.credit.Balance != balance-10 || r.credit.Frozen != 0 {
		t.Fatalf("只扣成功写入的那次：credits=%d account=%+v", follow.Credits, r.credit)
	}

	// 重试只处理仍没有提示词的苏星月。
	retry, err := s.CreateJob(context.Background(), 7, 1, &model.DramaJobReq{Step: "asset_prompts", Episodes: []int{1}})
	if err != nil {
		t.Fatal(err)
	}
	input = nil
	if err := json.Unmarshal(r.jobs[2].InputJSON, &input); err != nil || len(input) != 1 || input[0].NodeID != "node-苏星月" || uint64(retry.ID) != r.jobs[2].ID {
		t.Fatalf("重试只处理苏星月：%s", r.jobs[2].InputJSON)
	}
}

// 安全过滤是确定性的：不带着「格式不合格」反馈重试，直接告诉用户内容触发了安全过滤。
func TestDramaContentFilterFailsWithoutRetry(t *testing.T) {
	r := newDramaMemory()
	dramaAddEpisode(r, 1, "")
	e := &dramaTextStreamer{respond: func(context.Context, string) (*llmgateway.Result, error) {
		// 空输出被安全过滤截断：创作段就失败，不再进入格式段
		return &llmgateway.Result{Text: "", FinishReason: "content_filter"}, nil
	}}
	s, _ := dramaTestService(r, e, nil)
	j, err := s.CreateJob(context.Background(), 7, 1, &model.DramaJobReq{Step: "scripts", Episodes: []int{1}})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.HandleDrama(context.Background(), 7, 1, uint64(j.ID)); err != nil {
		t.Fatal(err)
	}
	got := r.jobs[0]
	if got.Status != "failed" || got.ErrorCode != errcode.ErrDramaContentFiltered.Code || e.calls != 1 {
		t.Fatalf("应一次调用后以安全过滤失败：%s %d %q calls=%d", got.Status, got.ErrorCode, got.ErrorMessage, e.calls)
	}
}
