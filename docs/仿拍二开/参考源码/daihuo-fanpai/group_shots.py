"""Grouping-only excerpt of wangcanyu/daihuo-fanpai plan_segments.py at 7754f2e. See LICENSE."""

MAX_DUR = 12      # 单段目标时长上限(multimodal 硬上限15,留余量)

MAX_CUTS = 3      # 单段最多归并 3 个镜头(=2 个内部硬切;即梦内部硬切实测 5 崩,留足余量)

LEG_BY_ROLE = {"package_text": "mmh3", "hero_real": "jimeng", "dynamic": "mmh3", "none": "mmh3"}

LEG_DEFAULT = "mmh3"

LEG_RATE = {"mmh3": 0.11, "jimeng": 0.742, "jimeng25": 1.378}   # ¥/秒,08-10 实测口径

HERO_HIGH = ("占满画面", "占满", "2/3", "特写", "微距", "质感", "占画面中央部分")

def _is_real_hero(s):
    blob = (s.get("product_in_frame") or "") + (s.get("action") or "")
    return any(k in blob for k in HERO_HIGH)

def shot_leg(s, hero_strict=False):
    """单镜该走哪条腿。★hero_real 一票否决:同段里只要有一个形体镜,整段必须走即梦
    (形体画错整段废,省不得那点钱)。
    hero_strict=True 时对 hero_real 再过一道占比门槛,不够格的降级到便宜腿。"""
    role = s.get("product_role") or "none"
    if role == "hero_real" and hero_strict and not _is_real_hero(s):
        return LEG_DEFAULT
    return LEG_BY_ROLE.get(role, LEG_DEFAULT)

def seg_leg(shots, hero_strict=False):
    return "jimeng" if any(shot_leg(s, hero_strict) == "jimeng" for s in shots) else LEG_DEFAULT

def group_shots(shots, max_cuts=MAX_CUTS, min_dur=0, hard_max_cuts=None, by_leg=False,
                hero_strict=False):
    """按 ≤MAX_DUR 且 ≤max_cuts 把连续镜头归并成段。

    ★min_dur = 后端的最短生成时长(即梦4s / 海螺h3 5s)。>0 时开启"填满"模式:
      段跨度还没够 min_dur 就继续并镜,不受 max_cuts 提前掐断 —— 治「快切片每段只有
      1.6~2.4秒却要按后端下限付 5 秒的钱」。08-09 李时珍片实测:10段×5s=50s 的账
      对着 30.5s 的片,39% 的钱花在被裁掉的画面上。
      min_dur=0(默认)时行为与旧版逐字节一致,老片重跑不会变。
    ★hard_max_cuts = 绝不可越过的镜数天花板(默认=max_cuts)。填满模式必须有这道闸:
      快切片(本片30.5s里27刀)不设闸会把9个镜头塞进一段,远超即梦"内部硬切5崩"的红线、
      也超出 h3 已验证的3刀。够不到 min_dur 的段就认了那笔下限钱,别拿崩片去省。"""
    hard = hard_max_cuts or max_cuts
    # ★by_leg:同段必须同腿。不同腿的镜头之间强制断开——否则一段里混了 hero_real,
    #   整段被拖去走即梦(贵7倍);或反过来把形体镜混进 h3 段,产品画错整段废。
    segs, cur = [], []
    for s in shots:
        if not cur:
            cur = [s]; continue
        dur = s["end"] - cur[0]["start"]
        span = cur[-1]["end"] - cur[0]["start"]
        leg_break = by_leg and shot_leg(s, hero_strict) != shot_leg(cur[-1], hero_strict)
        if leg_break or dur > MAX_DUR or len(cur) >= hard or (len(cur) >= max_cuts and span >= min_dur):
            segs.append(cur); cur = [s]
        else:
            cur.append(s)
    if cur:
        segs.append(cur)

    # ★拆细与最短时长是对冲的:单独拆一段要按 min_dur 付费,并进邻段只按它的真实跨度付。
    #   临界点 = min_dur × 本腿单价 ÷ 邻段单价。mmh3 镜并进即梦段:4×0.11÷0.742 ≈ 0.6秒
    #   → 短于 0.6 秒的 mmh3 碎段并回去更省(08-10 实测撞见 0.04 秒的碎段却要付 4 秒的钱)。
    #   ⚠反向不并:hero_real(jimeng)碎段再短也单独拆——形体画错整段废,且并进去会把
    #   整段拖成即梦价(贵7倍),质量和成本都指向隔离。
    if by_leg and min_dur:
        i = 0
        while i < len(segs) and len(segs) > 1:
            g = segs[i]
            span = g[-1]["end"] - g[0]["start"]
            if seg_leg(g, hero_strict) == "jimeng":
                i += 1; continue
            merged = False
            for j in (i - 1, i + 1):                    # 优先并回前一段,其次后一段
                if not (0 <= j < len(segs)):
                    continue
                host = segs[j]
                thresh = min_dur * LEG_RATE.get(seg_leg(g, hero_strict), .11) / \
                    LEG_RATE.get(seg_leg(host, hero_strict), .11)
                merged_span = max(g[-1]["end"], host[-1]["end"]) - min(g[0]["start"], host[0]["start"])
                if span >= thresh or len(host) + len(g) > hard or merged_span > MAX_DUR:
                    continue
                if j < i:
                    host.extend(g)                      # 并到前一段尾部
                else:
                    segs[j] = g + host                  # 并到后一段头部(保持镜序)
                segs.pop(i)
                merged = True
                break
            if not merged:
                i += 1

    # 收尾:末段不足 min_dur 时并回上一段(并回后不得超 MAX_DUR)
    if min_dur and len(segs) >= 2:
        last = segs[-1]
        if last[-1]["end"] - last[0]["start"] < min_dur and \
           (not by_leg or seg_leg(segs[-2], hero_strict) == seg_leg(last, hero_strict)) and \
           len(segs[-2]) + len(last) <= hard and \
           last[-1]["end"] - segs[-2][0]["start"] <= MAX_DUR:
            segs[-2].extend(segs.pop())
    return segs
