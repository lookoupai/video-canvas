# 仿拍项目交接

**2026-11-18，已完成仓库准备与设计，下一项是营销逐镜来源预览。** 产品同时面向商品短片和多集短剧。方案将带原片时间码的分镜作为共用数据，从第一项开发起分别记录原片与成片时间。[营销与短剧仿拍设计](营销与短剧仿拍设计.md)

| 位置 | 用途 |
| --- | --- |
| 仓库根目录 | 自有 fork 的工作树。当前 `master` 跟踪 `origin/master`，克隆基线为 v0.6.18 / `f0b8ced3687b22de6af0358c682c5e768001ff4f`。下载时与 `upstream/master` 一致。[fork](https://github.com/lookoupai/video-canvas) · [上游](https://github.com/zaylora/video-canvas) |
| `docs/仿拍二开/` | 随 fork 保存的方案和交接资料。[原始需求](初步需求.md) · [调研、许可与离线验证](短视频仿拍二开准备.md) |
| `docs/仿拍二开/参考源码/` | 已选的拆片包读取器和生成段规划摘录，各自附许可。后续按需集成。[读取器](参考源码/capsule-cinema/scripts/remake_reference.py) · [分组函数](参考源码/daihuo-fanpai/group_shots.py) |

## 当前进度

- **已核对**：fork 和上游远端、工作树提交、Compose 开发配置解析。仓库里的护手霜 MP4 容器标注时长 10 秒，拆解 JSON 有 4 镜。[参考视频](../../backend/internal/marketing/samples/cream_ref.mp4) · [拆解样例](../../backend/internal/marketing/samples/cream_reverse.json)
- **待开发**：新方案的预览、逐镜裁片与营销选版合成、短剧参考集导入及角色校订。仓库内已有的营销整条出片与短剧片场可作为基线。[开发约定](../../AGENTS.md)
- **待运行验证**：应用启动、针对新增行为的测试、真实模型出片。首个只读预览可用现成视频和测试映射完成。之后再准备自有两集剧情样片与模型渠道。

## 下次对话从这里开始

> 请读 `docs/仿拍二开/README.md` 和 `docs/仿拍二开/营销与短剧仿拍设计.md`，在仓库根目录先核对工作树和现有进度，从第一项未完成的验收场景继续。当前第一项是营销跟拍脚本逐镜来源预览。

第一项先读营销的跟拍映射和落库逻辑。在现有测试目录增加能重现“一个目标镜对应多个原镜”的用例，连同修订过拆解的历史脚本一起测，再实现只读预览与原片区间回放。[跟拍实现](../../backend/internal/marketing/follow.go) · [跟拍持久化](../../backend/internal/service/marketing_apply.go) · [现有跟拍测试](../../backend/internal/tests/marketing/follow_test.go) · [工作台](../../web/src/pages/marketing/detail)

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
