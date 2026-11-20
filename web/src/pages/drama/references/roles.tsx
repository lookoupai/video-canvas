import { useState } from "react";
import { Link } from "react-router";
import { saveReferenceRole } from "@/api/drama/reference";
import type { DramaReferences, ReferenceRole } from "@/api/drama/reference-type";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";

/** 一个全剧角色关联项目画布中的新角色，形象和音色继续由画布管理。 */
function RoleEditor({
  id,
  role,
  targets,
  onSaved,
}: {
  id: string;
  role: ReferenceRole;
  targets: DramaReferences["targets"];
  onSaved: () => Promise<void>;
}) {
  const [name, setName] = useState(role.name),
    [target, setTarget] = useState(role.targetNodeId),
    [busy, setBusy] = useState(false),
    [failed, setFailed] = useState(false);
  const save = async () => {
    setBusy(true);
    setFailed(false);
    try {
      await saveReferenceRole(id, role.id, name, target, role.revision);
      await onSaved();
    } catch {
      setFailed(true);
    } finally {
      setBusy(false);
    }
  };
  return (
    <div className="grid gap-2 sm:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_auto]">
      <Input
        aria-label={`${role.name}的全剧名称`}
        value={name}
        maxLength={128}
        onChange={(e) => setName(e.target.value)}
      />
      <select
        className="bg-chrome-hover min-w-0 rounded-lg px-3 py-2"
        aria-label={`${role.name}对应的新角色`}
        value={target}
        onChange={(e) => setTarget(e.target.value)}
      >
        <option value="">新角色待选择</option>
        {target && !targets.some((t) => t.nodeId === target) && (
          <option value={target}>原角色节点已移除</option>
        )}
        {targets.map((t) => (
          <option key={t.nodeId} value={t.nodeId}>
            {t.name}
          </option>
        ))}
      </select>
      <Button
        size="sm"
        variant="outline"
        disabled={busy || !name.trim() || (name === role.name && target === role.targetNodeId)}
        onClick={() => void save()}
      >
        保存角色
      </Button>
      {failed && (
        <p role="status" className="text-destructive text-xs sm:col-span-3">
          角色未保存，请刷新后重试
        </p>
      )}
    </div>
  );
}

/** 跨集角色表由所有集共享，本集别称通过各集的映射选择合并或拆分。 */
export function ReferenceRoles({
  id,
  data,
  onSaved,
}: {
  id: string;
  data: DramaReferences;
  onSaved: () => Promise<void>;
}) {
  const [name, setName] = useState(""),
    [busy, setBusy] = useState(false),
    [failed, setFailed] = useState(false);
  const add = async () => {
    setBusy(true);
    setFailed(false);
    try {
      await saveReferenceRole(id, null, name.trim(), "");
      setName("");
      await onSaved();
    } catch {
      setFailed(true);
    } finally {
      setBusy(false);
    }
  };
  return (
    <details className="bg-chrome ring-chrome-border rounded-2xl p-4 ring-1">
      <summary className="cursor-pointer font-medium">全剧角色 · {data.roles.length}</summary>
      <div className="mt-4 grid gap-3">
        <Link to={`/drama/${id}?area=assets&ep=0`} className="text-beam justify-self-start text-sm">
          管理新角色形象和音色
        </Link>
        {data.roles.map((role) => (
          <RoleEditor
            key={role.id + "-" + role.revision}
            id={id}
            role={role}
            targets={data.targets}
            onSaved={onSaved}
          />
        ))}
        <div className="flex gap-2">
          <Input
            aria-label="新增全剧角色"
            placeholder="全剧角色名"
            value={name}
            maxLength={128}
            onChange={(e) => setName(e.target.value)}
          />
          <Button variant="outline" disabled={busy || !name.trim()} onClick={() => void add()}>
            添加全剧角色
          </Button>
        </div>
        {failed && (
          <p role="status" className="text-destructive text-sm">
            角色未创建，请重试
          </p>
        )}
      </div>
    </details>
  );
}
