import { useCallback, useEffect, useRef, useState } from "react";

import { listVirtualHumans } from "@/api/virtual-human";
import type { VirtualHumanDto } from "@/api/virtual-human/type";
import { shouldPoll } from "@/utils/virtual-human/virtual-human";

/** 有图片审核中时的轮询间隔（毫秒）：方舟没有回调，后端对同一用户也按 3 秒节流 */
const POLL_MS = 3000;

/**
 * 当前用户的真人库（每用户不超过 200 个，一次拉完）。
 * 资产页的真人库、个人资产库（判断图片是否已在真人库）和角色卡的「从真人库引入」共用。
 * 有图片审核中时每 3 秒带 refresh 重拉一次，拿到方舟的审核结果；页面关掉就停，下次打开再刷新。
 * 加入、改名、加图等操作成功后用 upsert 就地更新，不整体重拉。失败提示由请求层统一弹。
 * @param enabled 是否需要数据；false 时不请求（弹窗关着时）
 */
export function useVirtualHumans(enabled = true) {
  const [humans, setHumans] = useState<VirtualHumanDto[] | null>(null);
  const [filingReady, setFilingReady] = useState(false);
  const [error, setError] = useState(false);
  /** 递增一次就重拉一次 */
  const [reloadKey, setReloadKey] = useState(0);
  /** 下一次请求是否带 refresh：首次加载不带（先尽快出列表），之后的轮询都带 */
  const refreshNext = useRef(false);

  useEffect(() => {
    if (!enabled) return;
    let active = true;
    listVirtualHumans(refreshNext.current)
      .then((list) => {
        if (!active) return;
        setHumans(list.items);
        setFilingReady(list.filingReady);
        setError(false);
      })
      .catch(() => {
        if (active) setError(true);
      });
    return () => {
      active = false;
    };
  }, [enabled, reloadKey]);

  /** 有图片审核中就排下一次轮询；列表一变（含轮询结果回来）就重新判断 */
  useEffect(() => {
    if (!enabled || !humans || !shouldPoll(humans)) return;
    const timer = window.setTimeout(() => {
      refreshNext.current = true;
      setReloadKey((key) => key + 1);
    }, POLL_MS);
    return () => window.clearTimeout(timer);
  }, [enabled, humans]);

  const reload = useCallback(() => setReloadKey((key) => key + 1), []);

  /** 就地写入一个真人（新建的放最前，已有的原位替换） */
  const upsert = useCallback((human: VirtualHumanDto) => {
    setHumans((prev) => {
      const list = prev ?? [];
      return list.some((h) => h.id === human.id)
        ? list.map((h) => (h.id === human.id ? human : h))
        : [human, ...list];
    });
  }, []);

  /** 就地删掉一个真人 */
  const drop = useCallback((id: string) => {
    setHumans((prev) => (prev ? prev.filter((h) => h.id !== id) : prev));
  }, []);

  return {
    humans,
    /** 管理员是否已配置好虚拟人像库；false 时不能加入新图片 */
    filingReady,
    /** 还没拿到第一份数据 */
    loading: enabled && humans === null && !error,
    error: error && humans === null,
    reload,
    upsert,
    drop,
  };
}
