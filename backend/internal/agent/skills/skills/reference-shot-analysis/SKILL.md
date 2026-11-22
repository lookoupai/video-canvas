---
name: reference-shot-analysis
title: 参考原片逐镜分析
description: 使用带时间码的系统帧图分析参考剧集，保存有证据、可核对的镜头与提示词草稿。
tags: [短剧,参考剧集,原片,镜头,动作,关键帧,提示词,仿拍]
---

# 参考原片逐镜分析

把用户指定的原片镜头整理成可编辑草稿。分析结论以平台准备任务保存的帧图、已有分镜边界和用户明确提出的改动要求为依据。

## 流程

1. 调用 `drama_reference_read` 的 `collection` 范围，按项目中的标题、集号确认目标参考集。随后读取该集当前修订，不根据列表摘要补写内容。
2. 调用 `drama_reference_prepare` 冻结当前修订。记录返回的 `preparation_id`，通过 `drama_reference_status` 查询任务；恢复或重复工具调用时先读取原 ID。
3. 按镜头读取冻结的镜头边界，再调用 `drama_reference_inspect` 查看该镜头的全部可用帧图。保持图片顺序和时间码，确实看不清时指出缺少的证据。
4. 逐镜记录可直接观察到的场景、画面构图、主体、动作和运镜。区分画面事实、合理推断和未知信息。单帧只能说明画面状态，不能确认运动轨迹、人物身份、商品属性或前后因果。
5. 按用户的保留／替换要求写出可独立执行的目标提示词。没有证据的商品、人物、对白、文字或品牌信息标为未知，不凭常识补齐。
6. 用 `drama_reference_draft` 创建或更新草稿。优先保留冻结文档已有的场景、角色、镜头边界和对白，不把草稿直接写回正式参考稿。

## 证据和状态

- 每张系统帧在 `evidence` 中保留 `preparation_frame_id`、`asset_id`、`shot_id` 和 `at_ms` 原值，并为草稿内证据生成稳定的 `id`。
- `observations` 的 `field` 只能是 `action`、`camera`、`characters` 或 `prompt`。`source` 用 `observation` 表示画面观察，已有导入内容用 `imported`，用户明确核对的值用 `manual`。
- 直接由图像支持且引用同镜证据时可标记 `review_state: observed`。部分可见或需要解释时标记 `inferred`，没有足够证据时标记 `unknown`。不要用模型置信度冒充用户确认。
- 仅当用户明确核对某项内容时标记 `confirmed`。后续复述模型观察不等于确认。
- `coverage.shots` 描述镜头区间是否完整，`coverage.visual` 描述逐镜视觉分析范围。没有音轨用 `unavailable`，有音轨但未分析用 `not_analyzed`，不能虚构对白或说话人。
- 工具返回的 `preparation_frame_id` 只能关联同一准备、同一镜号和镜内时间窗中的系统证据。引用不匹配时读取最新准备与草稿并修复数据，不要改时间码伪造匹配。

## 修订冲突

创建草稿时使用准备任务冻结的 `reference_revision` 和 `preparation_id`。更新时使用最近一次返回的草稿 `revision`。遇到修订冲突后先读取最新草稿，比较用户已经保存的字段，再合并有新证据的部分。

## 从草稿到制作

1. 用 `model_list` 查看可用模型，按用户选定的镜头和目标素材选择视频参考、图片参考或关键帧路线。只使用已核实的模型能力和资料。
2. 需要换商品或人物且现有素材不足时，用 `canvas_apply_ops` 准备图像生成节点，通过 `generate_media` 打开图片生成审批。图片积分单独确认。用 `task_get` 读取已成功产出的图片 `asset_id`，请用户核对商品或人物，再将它放进对应镜头的 `image_asset_ids`。原片证据仍引用原始系统帧。
3. 用 `drama_reference_read` 的 `quote` 范围提交实际图片、输出时长、成片裁剪和预算。记录原请求与 `plan_digest`。使用关键帧路线时，按首帧、尾帧顺序提交素材。模型、素材、时长或报价变更后重新报价。
4. 通过 `drama_reference_apply` 申请采用。拿到 `application_id` 后，以同一报价输入和摘要调用 `drama_generate_segments` 申请视频生成。收到任务后用 `task_get` 查询状态。额度不足时保留已保存的证据和草稿，等待用户决定是否调整模型或预算。
