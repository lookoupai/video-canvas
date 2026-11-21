# 仿拍项目交接

**2026-11-21 当前状态：v0.7.2 已发布并部署，参考制作的四项基础改造已经实现；Agent 自动理解原片、补全提示词和规划生成路线处于设计阶段。真实仿拍出片质量尚未验收通过。** 产品同时面向商品短片和多集短剧，原片时码与成片裁剪分别保存。

本目录的状态以本页为入口。旧设计和验收记录保留各自的时间与范围，新增方案不能作为已实现功能宣传。

| 文档 | 用途与状态 |
| --- | --- |
| [Agent 驱动仿拍方案](prd-Agent驱动仿拍-2026-11-21.md) | 当前设计稿，说明 Agent、平台工具和制作界面的分工，尚未实现 |
| [Agent 仿拍实施与验收](Agent仿拍实施与验收.md) | 下一阶段任务、依赖和验收矩阵，所有开发任务尚未开始 |
| [营销与短剧仿拍设计](营销与短剧仿拍设计.md) | 第一阶段制作基础的历史设计，四项基础改造已落地 |
| [参考剧集导入](参考剧集导入.md) · [短剧仿拍出片](短剧仿拍出片.md) | 当前可用的导入、校订、采用、生成和合成契约 |

## 最新能力边界

| 范围 | 当前结果 | 下一步 |
| --- | --- | --- |
| 原片导入、本地切镜、历史校订 | 已实现并完成真实媒体／接口验证 | 复用已有数据，增加逐镜持久证据与分析草稿 |
| 角色映射、采用到片场、单镜重做、选版和合成 | 已实现，确定性模型替身和 FFmpeg 验证通过 | 经真实渠道验证人物／商品、动作、声音和费用 |
| 参考集语义理解 | 当前本地命令关闭视觉模型、语音、字幕和说话人分析，缺失字段需导入或人工补齐 | 设计中的 Agent 工具将补全此链路 |
| Agent 运行时和视觉工具 | 已有独立服务、技能、审批、看图／看视频工具及短剧读写能力 | 增加参考草稿、采用及片段生成工具，挂载工作台助手入口 |
| 实际原镜视频生成 | Agnes 引用格式问题已修复，随后调用被渠道额度不足阻断 | 在可用渠道和明确预算下继续效果验收 |

### v0.7.2 发布与线上验证

2026-11-21 发布 `81522f52`，包含容器健康检查、参考素材公开地址、Agnes 视频引用对象格式、片场历史错误与提示词重叠四项修复。后台、前端、Agent 和更新器镜像完成 amd64／arm64 发布，当前应用服务已更新并通过健康检查。数据库和 Agent 数据在更新前已备份。

`bash scripts/check.sh web` 通过格式、类型、lint、5,149 项测试和生产构建。Agnes 插件修复已通过定向插件测试和后端完整检查；该次本机检查的数据库／Redis／FFmpeg 相关集成用例因缺少独立测试配置或工具而跳过，较早的完整集成记录见下文。GitHub 发布质量检查全部通过。线上刷新与三个窗口尺寸验证历史失败说明独立展示、无重叠和横向溢出，输入框可滚动到达并聚焦。

以上验证确认工程行为和部署结果，真实视频生成尚未成功。具体运行原文、令牌、截图和素材只保存在被忽略的 `.traces/`，不写进本目录。[v0.7.2 Release](https://github.com/lookoupai/video-canvas/releases/tag/v0.7.2) · [发布工作流](https://github.com/lookoupai/video-canvas/actions/runs/44896425381)

## 文档同步约定

- 设计或范围改变时更新方案，并在本页注明当前决策。
- 新增字段、工具或接口时，在同一实现提交更新相应契约和实施状态。
- 测试结束后记录实际命令、结果、环境限制及证据，真实模型结果与替身测试分别注明。
- 发布后记录版本、部署结果和剩余缺口，历史验证保留当时事实。
- 状态分别使用“设计完成”“代码完成”“检查通过”“已部署”“真实模型验收通过”，每一项都需要对应证据。

| 位置 | 用途 |
| --- | --- |
| 仓库根目录 | 自有 fork 的工作树，已部署功能基线为 `v0.7.2` / `81522f52`，`master` 跟踪 `origin/master`。最初改造位于 `feature/reference-remake`，经 `release/upstream-v0.7.1` 合并上游 v0.7.0，随后发布修复版本。[fork](https://github.com/lookoupai/video-canvas) · [上游](https://github.com/zaylora/video-canvas) |
| `docs/仿拍二开/` | 随 fork 保存的方案和交接资料。[原始需求](初步需求.md) · [调研、许可与离线验证](短视频仿拍二开准备.md) |
| `docs/仿拍二开/参考源码/` | 已选的拆片包读取器和生成段规划摘录，各自附许可。后续按需集成。[读取器](参考源码/capsule-cinema/scripts/remake_reference.py) · [分组函数](参考源码/daihuo-fanpai/group_shots.py) |

## 第一阶段实施与历史验收

以下记录对应 2026-11-19 至 2026-11-20 的四项制作基础改造。它们验证导入、编辑、任务和合成行为，新增 Agent 自动分析能力按上方方案继续推进。

- **已核对**：fork 和上游远端、工作树提交、Compose 开发配置解析。仓库里的护手霜 MP4 容器标注时长 10 秒，拆解 JSON 有 4 镜。[参考视频](../../backend/internal/marketing/samples/cream_ref.mp4) · [拆解样例](../../backend/internal/marketing/samples/cream_reverse.json)
- **第一项实现**已完成。营销工作台选中跟拍脚本后打开「逐镜仿拍」，可查看该修订每个成片镜的时长、场景及全部已知来源，点击原镜即可回放。旧稿只有 `ref_shot` 时提示映射可能不完整。缺少快照且拆解已经修订时显示「待关联」。[接口契约](../design/营销工作室/接口契约.md)
- **验收记录**覆盖护手霜样例的四个原片区间。Chrome 使用测试接口数据和仓库 MP4，实播分别停在 2.5、5、7.5、10 秒。刷新后将原镜 1、3 归入同一成片镜，两段可独立回放。PostgreSQL 集成测试验证了实际落库的映射，改变当前拆解后再次查询，仍能读到跟拍采用的旧区间。[映射测试](../../backend/internal/tests/marketing/remake_preview_test.go) · [持久化测试](../../backend/internal/tests/service/marketing_remake_preview_test.go) · [工作台测试](../../web/src/tests/pages/marketing/workbench-render.test.tsx)
- **第二项实现**已完成。入口为「逐镜仿拍 → 单镜制作 → 建立制作计划」。每版脚本有独立计划，固定商品图、模型、原片依据，支持编辑镜头、参考裁片、单镜生成和选版、重排及全部或显式部分合成。新增迁移 `0044–0047`，合成经 River 后台队列执行。[接口契约](../design/营销工作室/接口契约.md#31-逐镜制作计划)
- **第二项本地验收**使用真实 PostgreSQL、HTTP 鉴权路由、素材存储和 FFmpeg，模型产物用本地确定性视频替身。第 3 镜 8 次并发重传只建一条任务、冻结一次 20 测试积分；另一次失败退款后保留已选旧版。缺镜会拒绝完整合成，显式部分合成为 2.5 秒。重排和换版的两次完整成片均为 10 秒，画面与音轨顺序一致，首次合成使用提交时的快照。Chrome 从正常工作台完成整个流程，首次生成响应丢失后重传复用原键，刷新恢复计划、选版和成片。[生成与重传测试](../../backend/internal/tests/service/marketing_remake_retry_test.go) · [合成快照测试](../../backend/internal/tests/service/marketing_remake_compose_worker_test.go) · [媒体测试](../../backend/internal/tests/pkg/videoframe/compose_ranges_test.go) · [工作台测试](../../web/src/tests/pages/marketing/remake-production.test.tsx)
- **第二项当时待验收**为真实模型的耗时、实际积分和商品外观。当时未配置真实渠道，以上测试积分与模型替身不能用于评估真实出片质量。后续已配置渠道并发起测试，最新结果见本页“最新能力边界”。[实施次序](营销与短剧仿拍设计.md#实施次序与验收)

## 剧集制作与验证

第三项入口是短剧首页「从参考剧集创建」，已有项目可从工作台「参考剧集」进入。支持原片上传、拆片包导入、逐集异步本地分析、集序调整、场次与镜头查看、对白与说话人校订、跨集角色到项目新角色的映射，以及历史修订回看。新增迁移 `0048`。[导入格式、接口与分析器环境](参考剧集导入.md)

自制两集各 5 秒样例使用蓝衣、红衣人物与双人对白。Chrome 经真实登录路由、PostgreSQL 和素材服务完成上传及校订，第二集错误包失败后单独重传正确包，第一集的修改保留。第二集的「蓝衣人」与第一集「阿澄」关联到同一全剧角色及新角色节点。刷新后映射与说话人可恢复，原镜回放停在 2.5 秒。再次执行真实 rea-video 本地分析后，旧校订稿可从历史修订读取。切镜分析不自动识别人名或台词，界面提示待校订。[解析测试](../../backend/internal/tests/reference/package_test.go) · [双集集成测试](../../backend/internal/tests/service/drama_reference_test.go) · [工作台测试](../../web/src/tests/pages/drama/reference.test.tsx)

第四项入口是参考校订页「采用到片场」。按原镜重拍会建立真实分集和逐镜片段；原创改编固定参考依据和角色对应，再经现有「生成剧本 → 拆解资产 → 分镜」进入片场。已有跟拍集可以只更新勾选的原镜，保留其他镜头及已采用视频。片场新增原镜回放、无声视频参考开关和毫秒裁剪，生成重传恢复固定输入，连播与合成使用相同窗口。新增迁移 `0049–0050`。[实现与接口](短剧仿拍出片.md)

第四项本地验收使用两集各两个 2.5 秒镜头。28 项真实 HTTP 检查通过：重复采用不重复建集，单镜重传只创建一条任务、冻结一次积分，失败退款保留旧版，局部更新和重拍不改变另一集。真实 FFmpeg 合成两集均为 5.000 秒，抽帧颜色及音频频率与镜序一致；原镜裁片为 2.500 秒且无音轨。原创改编经实际写稿、拆解锁集和分镜服务走通，模型替身收到采用时的原稿与新角色快照。Chrome 从正常参考页完成选镜采用、原镜回放、故意丢失生成响应后的重传、选版、裁剪和合成；刷新恢复选版及裁剪，连播正确在目标终点切镜。模型输出均为本地可核对替身，不能代表真实生成质量。

最终运行 `GOMAXPROCS=2 GOFLAGS=-p=1 bash scripts/check.sh web backend`，退出码 0，前后端全部通过：前端 4987 项测试，以及格式、类型、lint 和构建；后端增量 lint 为 0 issues，构建、完整测试和迁移结构一致性通过。相关 `go test -race` 检查也已通过。验收服务和临时 PostgreSQL、Redis 均已关闭；验证日志与媒体未纳入仓库；验收后已清理临时媒体和运行环境。

Docker 打包补充验证在 Linux/amd64 完成：生产与开发镜像均构建成功。生产镜像默认 UID 100 / GID 101 下的健康检查、空库迁移、Redis 连接及素材／日志／socket 目录写入均通过。`TestAnalyzer_DockerImage` 在断网、只读根目录下将真实 10 秒样片拆为四个 2.5 秒镜头，同时校验分析器源码版本、模型哈希及许可文件；开发镜像在相同断网、只读条件下也通过四镜分析，并验证 Go、Air、Node 和 curl 可运行。`bash scripts/check.sh backend` 再次全部通过。依赖清单覆盖 amd64/arm64 的 Python wheel，本地未运行 arm64 容器。本地验收镜像、构建缓存、临时服务、数据库及测试网络均已清理。正式镜像由 GitHub 的版本标签发布流程构建。

验证使用独立 PostgreSQL、Redis 测试实例，持久化用例需要设置 `TEST_DATABASE_DSN`，缓存测试使用 `TEST_REDIS_ADDR`，媒体验证需要 PATH 中有 FFmpeg/ffprobe。复核四项可运行以下命令。

```bash
(cd backend && GOMAXPROCS=2 go test -p 1 ./internal/tests/service ./internal/tests/repository ./internal/tests/handler ./internal/tests/provider/queue -run Remake -count=1)
(cd backend && GOMAXPROCS=2 go test -p 1 ./internal/tests/pkg/videoframe -run ComposeRanges -count=1)
(cd web && unset BUN_OPTIONS && bun test src/tests/pages/marketing/workbench-render.test.tsx src/tests/pages/marketing/remake-production.test.tsx)
(cd backend && GOMAXPROCS=2 go test -p 1 ./internal/tests/reference ./internal/tests/service ./internal/tests/handler ./internal/tests/provider/queue -run 'Reference|Package|Analyzer' -count=1)
(cd web && unset BUN_OPTIONS && bun test src/tests/api/drama-reference.test.ts src/tests/api/drama-remake.test.ts src/tests/pages/drama/reference.test.tsx src/tests/pages/drama/reference-adopt.test.tsx src/tests/pages/drama/remake-panel.test.tsx src/tests/pages/drama/remake-playback.test.tsx src/tests/pages/drama/remake-final.test.tsx)
GOMAXPROCS=2 GOFLAGS=-p=1 bash scripts/check.sh web backend
(cd backend && GOMAXPROCS=2 go test -p 1 -race ./internal/tests/service ./internal/tests/repository ./internal/tests/provider/queue ./internal/tests/reference)
```

> 接续工作先读本页、[Agent 驱动仿拍方案](prd-Agent驱动仿拍-2026-11-21.md)和[实施与验收](Agent仿拍实施与验收.md)，再核对工作树与当前任务。下一阶段优先补齐逐镜证据、语义草稿与助手工具，真实渠道效果验收随后闭环。API 并发与开发资源限制按当前用户约定及模型渠道配置执行。

尚待完成的内容包括 Agent 自动语义拆解、生成路线选择，以及真实模型效果。制作验收继续使用「四镜只重做第 3 镜」和「同一新角色两集出场、只重做一镜并合成指定集」，记录商品／人物外观、音色与动作衔接、时长、耗时和实际扣费。生产与开发 Docker 镜像已内置 rea-video、Python、FFmpeg 和本地分析依赖；直接运行 Go 服务时需准备本机环境，见[导入文档](参考剧集导入.md#本地分析器)。[原设计与验收标准](营销与短剧仿拍设计.md#实施次序与验收)

以下命令从仓库根目录执行。需要启动应用时，按上游开发指南使用 `./scripts/start-docker.sh -d`，随后可执行定向测试。每完成一项，将实际分支、提交或未提交改动、已跑的测试、下一项验收更新在本页。[开发指南](../../docs/development.md) · [Docker 开发环境](../../docs/docker-development.md)

```bash
git status -sb
git remote -v
git rev-parse HEAD origin/master
docker compose -f docker-compose.dev.yml config --quiet
```

开发容器已启动时，可在仓库根目录运行：

```bash
docker compose -f docker-compose.dev.yml exec -T backend go test ./internal/tests/marketing/...
```

## 与上游 v0.7.0 合并

保留上游故事板、参考音频、真人库及自定义单集时长。故事板图片、旁白音频和仿拍原镜视频可以同时带入生成；片场时长继续按仿拍的毫秒裁剪窗口累计。上游已发布的 `0043_virtual_humans.sql` 原样保留，本 fork 尚未发布的仿拍迁移顺延为 `0044–0050`。此前临时验收库已清理，正式部署从已发布版本正常升级。

合并后使用 Bun 1.3 与 Go 1.27 重新检查：`bash scripts/check.sh web` 和 `bash scripts/check.sh backend` 全部通过；前端 5,145 项测试，后端包含真实 PostgreSQL / Redis、迁移结构一致性和新增功能回归。已修复测试中的动画帧时序与编辑器异步销毁问题。
