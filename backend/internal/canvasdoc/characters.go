package canvasdoc

// CharacterImages 是一张角色卡节点用到的图片：主图（data.assetId）和补充图（data.character.extras[].assetId）。
type CharacterImages struct {
	NodeID   string   // 节点 id
	Name     string   // 角色名（data.character.name）
	AssetIDs []string // 素材 ID（十进制字符串），主图在前，去重
}

// CharacterImageUses 找出画布内容里所有角色卡节点（data.kind == "character"）及其用到的图片。
// 真人库移除图片前用它判断图片是否还被角色卡使用；音色不是图片，不算在内。
func CharacterImageUses(raw []byte) ([]CharacterImages, error) {
	p, err := decodePayload(raw)
	if err != nil {
		return nil, err
	}
	var out []CharacterImages
	for _, node := range p.Nodes {
		data, _ := node["data"].(map[string]any)
		if data == nil || data["kind"] != "character" {
			continue
		}
		use := CharacterImages{}
		use.NodeID, _ = node["id"].(string)
		character, _ := data["character"].(map[string]any)
		use.Name, _ = character["name"].(string)
		ids := appendAssetID(nil, data["assetId"])
		extras, _ := character["extras"].([]any)
		for _, e := range extras {
			if m, ok := e.(map[string]any); ok {
				ids = appendAssetID(ids, m["assetId"])
			}
		}
		seen := map[string]bool{}
		for _, id := range ids {
			if !seen[id] {
				seen[id] = true
				use.AssetIDs = append(use.AssetIDs, id)
			}
		}
		out = append(out, use)
	}
	return out, nil
}
