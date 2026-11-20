import { useRef, useState } from "react";
import { useNavigate } from "react-router";
import { createReferenceDrama } from "@/api/drama/reference";
import { DialogSession } from "@/components/dialog-session";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";

/** 从参考剧集起步，新项目随后逐集添加原片。 */
function CreateForm() {
  const [title, setTitle] = useState(""),
    [busy, setBusy] = useState(false),
    [failed, setFailed] = useState(false);
  const key = useRef(crypto.randomUUID());
  const navigate = useNavigate();
  const submit = async () => {
    setBusy(true);
    setFailed(false);
    try {
      const p = await createReferenceDrama(title.trim(), key.current);
      navigate(`/drama/${p.id}/references`);
    } catch {
      setFailed(true);
    } finally {
      setBusy(false);
    }
  };
  return (
    <form
      className="grid gap-4"
      onSubmit={(e) => {
        e.preventDefault();
        if (!busy && title.trim()) void submit();
      }}
    >
      <label className="grid gap-2">
        短剧名称
        <Input autoFocus value={title} onChange={(e) => setTitle(e.target.value)} maxLength={255} />
      </label>
      {failed && <p role="status">创建失败，请重试</p>}
      <Button type="submit" disabled={busy || !title.trim()}>
        {busy ? "正在创建…" : "创建参考项目"}
      </Button>
    </form>
  );
}

/** 短剧首页可直接进入参考剧集流程。 */
export function CreateReferenceDramaButton() {
  const [open, setOpen] = useState(false);
  return (
    <>
      <Button variant="outline" onClick={() => setOpen(true)}>
        从参考剧集创建
      </Button>
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>从参考剧集创建</DialogTitle>
          </DialogHeader>
          <DialogSession open={open}>
            <CreateForm />
          </DialogSession>
        </DialogContent>
      </Dialog>
    </>
  );
}
