package canvasgraph

import "math"

// 故事板（宫格分镜图）是短剧画布上的图片节点：data.drama.board 记着它属于哪个片段。
// 片场里点生成时由后端写节点并绑定生成任务，任务结束后后端把结果写回节点；
// 节点数据的形状要和前端编辑器一致（web/src/utils/drama/board-input.ts、utils/tasks/submit.ts、utils/tasks/backfill.ts），
// 这样编辑器打开时读到的就是一个普通的图片节点，再对账也不会改出差异。

// 故事板节点的排布：放在所有非故事板节点的右边，每集一行、每段一列；尺寸同前端 16:9 节点框。
const (
	boardAreaGap = 400.0
	boardColGap  = 48.0
	boardRowGap  = 96.0
	boardWidth   = 576.0
	boardHeight  = 324.0
	// maxBoardOutputs 同前端 MAX_NODE_OUTPUTS：每个节点最多留几版
	maxBoardOutputs = 50
)

// BoardSpec 是片场交来的一张故事板：前端按片段原稿和项目设置拼好的提示词、参数、模型和要连进来的资产节点。
type BoardSpec struct {
	SegmentID string         // 片段 id（对外编码）
	EpisodeNo int            // 集号，从 1 起
	Seq       int            // 片段序号，从 0 起
	Label     string         // 新建节点的标题，如「故事板 · 第 3 集 · 01」
	Prompt    string         // 宫格图提示词（@ 引用原样保留）
	Params    map[string]any // 生成参数（含 prompt、比例）
	Model     string         // 图片模型 key
	Board     map[string]any // data.drama.board：segmentId / sourceHash / layout / shotCount
	Refs      []string       // 要连线进来的资产节点 id，画布上不存在的跳过
	NewID     string         // 片段还没有故事板节点时用的新节点 id
}

// BoardOutput 是生成任务的一份产物。
type BoardOutput struct {
	AssetID   string // 素材 id（十进制字符串）
	URL       string // 访问地址
	MediaType string // image / video / audio
}

// BoardOutcome 是生成任务的终态，用来写回故事板节点。
type BoardOutcome struct {
	TaskID  string        // 任务 id（十进制字符串），要和节点上的 taskId 一致才写
	Status  string        // succeeded / failed / expired / canceled
	Outputs []BoardOutput // 成功时的产物
	Error   string        // 失败原因
	TaskRef string        // 任务编号，失败时给用户报障用
	Now     int64         // 写入时间（毫秒），记在新版本的 createdAt 上
}

// boardMeta 返回节点的 data.drama.board，不是故事板节点返回 nil。
func boardMeta(n Node) map[string]any {
	d, _ := n.Data()["drama"].(map[string]any)
	b, _ := d["board"].(map[string]any)
	if str(b["segmentId"]) == "" {
		return nil
	}
	return b
}

// IsBoardNode 判断是不是故事板节点（组节点不算）。
func IsBoardNode(n Node) bool { return !n.IsGroup() && boardMeta(n) != nil }

// FindBoard 按片段 id 找故事板节点；同一片段有多个时取后面的（同前端 boardsFromGraph），没有返回 nil。
func FindBoard(g *Graph, segmentID string) Node {
	var hit Node
	for _, n := range g.Nodes {
		if IsBoardNode(n) && str(boardMeta(n)["segmentId"]) == segmentID {
			hit = n
		}
	}
	return hit
}

// BoardSegment 是一个片段在画布上的故事板要用的信息：分镜出来时每个片段先建一个空节点。
type BoardSegment struct {
	ID         string // 片段 id（对外编码）
	Seq        int    // 片段序号，从 0 起
	NewID      string // 片段还没有节点时用的新节点 id
	Label      string // 新节点的标题，如「故事板 · 第 3 集 · 01」
	SourceHash string // 当前原稿的 SHA-256（空节点没出过图，记着它用来在画布里看是按哪版原稿建的）
	Layout     string // 2x2 / 3x2 / 3x3
	ShotCount  int    // 数出的镜头数
}

// SyncBoards 让一集的故事板节点和片段对上：每个片段有一个节点（没有就建一个空的：没有图、没有任务，
// 用户可以在画布里上传或生成），这一集里片段已经不在了的节点连同连线清掉。
// 已有的节点原样保留（图、历史、标题、drama.board 里出图时的原稿哈希都不动）；别的集的故事板不碰。
// 没有要改的返回 nil；入参不变。
func SyncBoards(g *Graph, episodeNo int, segments []BoardSegment) *Result {
	work := g.Clone()
	want := map[string]bool{}
	for _, seg := range segments {
		want[seg.ID] = true
	}
	// 1. 这一集里片段已经没了的故事板节点：清掉节点和它的连线
	var gone map[string]bool
	kept := work.Nodes[:0:0]
	for _, n := range work.Nodes {
		meta := boardMeta(n)
		if n.IsGroup() || meta == nil || !boardInEpisode(n, episodeNo) || want[str(meta["segmentId"])] {
			kept = append(kept, n)
			continue
		}
		if gone == nil {
			gone = map[string]bool{}
		}
		gone[n.ID()] = true
	}
	if gone != nil {
		work.Nodes = kept
		edges := work.Edges[:0:0]
		for _, e := range work.Edges {
			if !gone[e.Source()] && !gone[e.Target()] {
				edges = append(edges, e)
			}
		}
		work.Edges = edges
	}
	// 2. 没有节点的片段建一个空节点
	for _, seg := range segments {
		if FindBoard(work, seg.ID) != nil {
			continue
		}
		n := Node{"id": seg.NewID, "type": "canvas", "data": map[string]any{
			"kind": "image", "label": seg.Label,
			"drama": map[string]any{"episodes": []any{float64(episodeNo)}, "board": map[string]any{
				"segmentId": seg.ID, "sourceHash": seg.SourceHash, "layout": seg.Layout, "shotCount": float64(seg.ShotCount),
			}},
		}}
		n.SetPosition(boardPosition(work, episodeNo, seg.Seq))
		work.Nodes = append(work.Nodes, n)
	}
	changes := Diff(g, work)
	if len(changes) == 0 {
		return nil
	}
	return &Result{Graph: work, Changes: changes}
}

// boardInEpisode 故事板节点属于第几集（data.drama.episodes 里的集号）。
func boardInEpisode(n Node, episodeNo int) bool {
	d, _ := n.Data()["drama"].(map[string]any)
	list, _ := d["episodes"].([]any)
	for _, item := range list {
		if int(num(item)) == episodeNo {
			return true
		}
	}
	return false
}

// UpsertBoard 建好或更新片段的故事板节点、连上资产，并把生成任务绑定上去（同前端提交后的 buildSubmittedPatch）：
// 已有节点就用新的提示词、参数、模型和 drama.board 重写，旧结果落进历史版本；没有就在故事板区按集 / 段排开新建。
// 返回新图（入参不变）和节点 id。
func UpsertBoard(g *Graph, spec BoardSpec, taskID string) (*Result, string) {
	work := g.Clone()
	n := FindBoard(work, spec.SegmentID)
	if n == nil {
		n = Node{"id": spec.NewID, "type": "canvas", "data": map[string]any{"kind": "image", "label": spec.Label}}
		n.SetPosition(boardPosition(work, spec.EpisodeNo, spec.Seq))
		work.Nodes = append(work.Nodes, n)
	}
	d := n.Data()
	// 提交后的节点：旧结果先落进历史（记着它当时的模型和任务），再清掉当前结果和上一次的残留
	if outs := readOutputs(d); len(outs) > 0 {
		d["outputs"] = outs
	}
	params, _ := d["params"].(map[string]any)
	merged := map[string]any{}
	for k, v := range params {
		merged[k] = v
	}
	for k, v := range spec.Params {
		merged[k] = v
	}
	d["prompt"] = spec.Prompt
	d["params"] = merged
	if spec.Model != "" {
		d["model"] = spec.Model
	}
	d["drama"] = map[string]any{"episodes": []any{float64(spec.EpisodeNo)}, "board": spec.Board}
	d["taskId"] = taskID
	d["status"] = "running"
	d["src"] = nil
	d["uploaded"] = false
	d["error"] = nil
	for _, key := range []string{"assetId", "mediaType", "fileName", "pendingTaskIds"} {
		delete(d, key)
	}
	for _, ref := range spec.Refs {
		if ref == n.ID() || work.Node(ref) == nil || work.hasEdge(ref, n.ID()) {
			continue
		}
		work.Edges = append(work.Edges, Edge{
			"id": "xy-edge__" + ref + "-" + n.ID(), "source": ref, "target": n.ID(), "type": "animatedSvgEdge",
			"data": map[string]any{"duration": float64(2), "direction": "forward", "repeat": "indefinite", "path": "bezier", "shape": "sweep"},
		})
	}
	return &Result{Graph: work, Changes: Diff(g, work)}, n.ID()
}

// BackfillBoard 把任务终态写回故事板节点（规则同前端 planBackfill）；节点不在、不是故事板、或 taskId 对不上时不改，返回 nil。
// 成功：追加新版本并设为当前版本，status=done；没有可用产物按失败处理。
// 失败 / 超时：节点已经有可用结果（done）时只保留现状，否则 status=error 并写原因。
// 取消：有历史版本就退回当前版本（done），没有就回到 idle，并清掉 taskId。
func BackfillBoard(g *Graph, nodeID string, o BoardOutcome) *Result {
	before := g.Node(nodeID)
	if before == nil || !IsBoardNode(before) || str(before.Data()["taskId"]) != o.TaskID || o.TaskID == "" {
		return nil
	}
	work := g.Clone()
	d := work.Node(nodeID).Data()
	var changed bool
	switch o.Status {
	case "succeeded":
		changed = backfillSucceeded(d, o)
	case "failed", "expired":
		changed = backfillFailed(d, o)
	case "canceled":
		backfillCanceled(d)
		changed = true
	}
	if !changed {
		return nil
	}
	changes := Diff(g, work)
	if len(changes) == 0 {
		return nil
	}
	return &Result{Graph: work, Changes: changes}
}

// backfillSucceeded 成功：追加新版本并设为当前版本；没有可用产物按失败处理；产物都已在历史里（重复回写）时不改。
func backfillSucceeded(d map[string]any, o BoardOutcome) bool {
	var outs []map[string]any
	for _, out := range o.Outputs {
		if out.URL == "" || out.AssetID == "" {
			continue
		}
		media := out.MediaType
		if media == "" {
			media = "image"
		}
		outs = append(outs, map[string]any{
			"id": out.AssetID, "src": out.URL, "mediaType": media, "assetId": out.AssetID,
			"taskId": o.TaskID, "model": d["model"], "createdAt": float64(o.Now),
		})
	}
	if len(outs) == 0 {
		failBoard(d, "生成结果为空，请重试", o.TaskRef)
		return true
	}
	if str(d["status"]) == "done" && knowsAll(readOutputs(d), outs) {
		return false
	}
	appendBoardOutputs(d, outs)
	d["status"] = "done"
	d["uploaded"] = false
	d["error"] = nil
	delete(d, "fileName")
	return true
}

// backfillFailed 失败 / 超时：节点已经有可用结果（done）时保留现状，否则写失败原因。
func backfillFailed(d map[string]any, o BoardOutcome) bool {
	if str(d["status"]) == "done" {
		return false
	}
	msg := o.Error
	if msg == "" {
		msg = "生成失败"
		if o.Status == "expired" {
			msg = "生成超时"
		}
	}
	failBoard(d, msg, o.TaskRef)
	return true
}

// backfillCanceled 取消：清掉 taskId，有历史版本就退回当前版本（done），没有回到 idle。
func backfillCanceled(d map[string]any) {
	delete(d, "taskId")
	outs := readOutputs(d)
	if len(outs) == 0 {
		d["status"] = "idle"
		d["src"] = nil
		delete(d, "assetId")
		return
	}
	active := outs[len(outs)-1]
	for _, out := range outs {
		if str(out["id"]) == str(d["activeOutputId"]) {
			active = out
		}
	}
	selectOutput(d, active)
	d["status"] = "done"
}

// HasBoardOutput 故事板节点的历史版本里有没有 outputID 这一版。
func HasBoardOutput(n Node, outputID string) bool {
	for _, out := range readOutputs(n.Data()) {
		if str(out["id"]) == outputID {
			return true
		}
	}
	return false
}

// SelectBoardOutput 把片段故事板的当前版本切到 outputID 这一版（刷新 src / mediaType / assetId 镜像，同前端切换历史版本）。
// 没有这个故事板节点、没有这一版、或已经是当前版本时不改，返回 nil。
func SelectBoardOutput(g *Graph, segmentID, outputID string) *Result {
	before := FindBoard(g, segmentID)
	if before == nil || outputID == "" {
		return nil
	}
	var target map[string]any
	for _, out := range readOutputs(before.Data()) {
		if str(out["id"]) == outputID {
			target = out
		}
	}
	if target == nil || str(before.Data()["activeOutputId"]) == outputID && str(before.Data()["assetId"]) == str(target["assetId"]) {
		return nil
	}
	work := g.Clone()
	d := FindBoard(work, segmentID).Data()
	selectOutput(d, target)
	d["status"] = "done"
	d["error"] = nil
	changes := Diff(g, work)
	if len(changes) == 0 {
		return nil
	}
	return &Result{Graph: work, Changes: changes}
}

// failBoard 写失败态：保留 taskId 方便报障，清掉当前结果。
func failBoard(d map[string]any, msg, taskRef string) {
	d["status"] = "error"
	d["error"] = msg
	if taskRef != "" {
		d["errorTaskRef"] = taskRef
	} else {
		d["errorTaskRef"] = nil
	}
	d["src"] = nil
	delete(d, "assetId")
}

// readOutputs 读节点的历史版本（同前端 readOutputs）：没有 outputs 但有存下来的素材时把它当唯一一版。
func readOutputs(d map[string]any) []map[string]any {
	if list, ok := d["outputs"].([]any); ok && len(list) > 0 {
		out := make([]map[string]any, 0, len(list))
		for _, item := range list {
			if m, ok := item.(map[string]any); ok {
				out = append(out, m)
			}
		}
		return out
	}
	if list, ok := d["outputs"].([]map[string]any); ok && len(list) > 0 {
		return list
	}
	src, assetID := str(d["src"]), str(d["assetId"])
	if src == "" || assetID == "" || len(src) >= 5 && src[:5] == "blob:" {
		return nil
	}
	media := str(d["mediaType"])
	if media == "" {
		media = "image"
	}
	first := map[string]any{"id": assetID, "src": src, "mediaType": media, "assetId": assetID, "createdAt": float64(0)}
	if t := str(d["taskId"]); t != "" {
		first["taskId"] = t
	}
	if m, ok := d["model"]; ok {
		first["model"] = m
	}
	return []map[string]any{first}
}

// knowsAll 判断这些产物是不是都已经在历史里（重复回写时不改）。
func knowsAll(current, outs []map[string]any) bool {
	known := map[string]bool{}
	for _, item := range current {
		known[str(item["id"])] = true
	}
	for _, out := range outs {
		if !known[str(out["id"])] {
			return false
		}
	}
	return true
}

// appendBoardOutputs 追加新版本（同一素材不重复），第一份设为当前版本（同前端 appendOutputs activeIsFirst）。
func appendBoardOutputs(d map[string]any, outs []map[string]any) {
	current := readOutputs(d)
	known := map[string]bool{}
	for _, item := range current {
		known[str(item["id"])] = true
	}
	list := make([]any, 0, len(current)+len(outs))
	for _, item := range current {
		list = append(list, item)
	}
	for _, out := range outs {
		if !known[str(out["id"])] {
			known[str(out["id"])] = true
			list = append(list, out)
		}
	}
	if len(list) > maxBoardOutputs {
		list = list[len(list)-maxBoardOutputs:]
	}
	d["outputs"] = list
	selectOutput(d, outs[0])
}

// selectOutput 把某一版设为当前版本，并刷新 src / mediaType / assetId 镜像。
func selectOutput(d map[string]any, out map[string]any) {
	d["activeOutputId"] = out["id"]
	d["src"] = out["src"]
	d["mediaType"] = out["mediaType"]
	d["assetId"] = out["assetId"]
}

// boardPosition 新故事板节点的位置（同前端 boardNodePosition）：已有的故事板节点不参与算基准。
func boardPosition(g *Graph, episodeNo, seq int) Point {
	right := -boardAreaGap
	top := math.Inf(1)
	for _, n := range g.Nodes {
		if IsBoardNode(n) {
			continue
		}
		width := num(n["width"])
		if m, ok := n["measured"].(map[string]any); ok && num(m["width"]) > 0 {
			width = num(m["width"])
		}
		p := n.Position()
		right = math.Max(right, p.X+width)
		top = math.Min(top, p.Y)
	}
	if math.IsInf(top, 1) {
		top = 0
	}
	return Point{
		X: right + boardAreaGap + float64(seq)*(boardWidth+boardColGap),
		Y: top + float64(episodeNo-1)*(boardHeight+boardRowGap),
	}
}
