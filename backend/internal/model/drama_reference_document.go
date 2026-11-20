package model

// DramaReferenceDocument 保存一集原片的独立镜头、场次、人物和跨镜对白。
type DramaReferenceDocument struct {
	Scenes     []DramaReferenceScene     `json:"scenes"`
	Characters []DramaReferenceCharacter `json:"characters"`
	Shots      []DramaReferenceShot      `json:"shots"`
	Dialogues  []DramaReferenceDialogue  `json:"dialogues"`
}

// DramaReferenceScene 是拆片包内稳定的场次标识。
type DramaReferenceScene struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// DramaReferenceCharacter 是当前原集的角色身份，跨集身份通过绑定表人工确认。
type DramaReferenceCharacter struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// DramaReferenceShot 的时间窗使用原片毫秒，结束点不包含在镜头内。
type DramaReferenceShot struct {
	ID         string   `json:"id"`
	SceneID    string   `json:"scene_id"`
	StartMs    int64    `json:"start_ms"`
	EndMs      int64    `json:"end_ms"`
	Action     string   `json:"action"`
	Camera     string   `json:"camera"`
	Characters []string `json:"characters"`
}

// DramaReferenceDialogue 独立于镜头保存，允许对白跨镜及一个镜头多人说话；空 Speaker 表示待确认。
type DramaReferenceDialogue struct {
	ID      string `json:"id"`
	StartMs int64  `json:"start_ms"`
	EndMs   int64  `json:"end_ms"`
	Text    string `json:"text"`
	Speaker string `json:"speaker"`
}
