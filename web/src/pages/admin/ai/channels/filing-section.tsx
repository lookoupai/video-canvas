import type { ChannelFilingView } from "@/api/admin/ai/type.d";
import { FormField } from "@/components/admin-ui/form-field";
import { Tag } from "@/components/admin-ui/tag";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { filingSiteOf, type ChannelFormState } from "@/utils/admin/channel-form";

/**
 * 渠道弹窗里的「虚拟人像库」：用户真人库里的图片备案到这个渠道的方舟素材组，生成时以 asset:// 引用。
 * 备案站点由 Base URL 推断，不手选；AK/SK 只写，留空不改，两项一起填。全平台只能一个渠道启用（后端校验）。
 * @param form 渠道表单
 * @param filing 渠道当前的虚拟人像库视图（看 AK/SK 是否已设置）；旧后端没有
 * @param accessKey 正在填写的 Access Key
 * @param secretKey 正在填写的 Secret Key
 * @param errors 表单错误（filingGroupId、filingKeys）
 * @param disabled 保存中
 * @param onPatch 改表单字段
 * @param onAccessKey 改 Access Key
 * @param onSecretKey 改 Secret Key
 */
export function FilingSection({
  form,
  filing,
  accessKey,
  secretKey,
  errors,
  disabled,
  onPatch,
  onAccessKey,
  onSecretKey,
}: {
  form: ChannelFormState;
  filing?: ChannelFilingView;
  accessKey: string;
  secretKey: string;
  errors: Record<string, string>;
  disabled: boolean;
  onPatch: (next: Partial<ChannelFormState>) => void;
  onAccessKey: (value: string) => void;
  onSecretKey: (value: string) => void;
}) {
  const keysSet = !!filing?.access_key_set && !!filing?.secret_key_set;
  return (
    <div className="flex flex-col gap-3 rounded-lg border p-3">
      <div className="flex items-center gap-2 text-sm">
        <Switch
          id="channel-filing"
          checked={form.filingEnabled}
          disabled={disabled}
          onCheckedChange={(checked) => onPatch({ filingEnabled: checked })}
        />
        <Label htmlFor="channel-filing">虚拟人像库</Label>
        {form.filingEnabled && (
          <Tag tone="neutral" className="ml-auto">
            {filingSiteOf(form.baseUrl)}
          </Tag>
        )}
      </div>

      {form.filingEnabled && (
        <>
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <FormField
              label="素材组 ID"
              htmlFor="channel-filing-group"
              required
              error={errors.filingGroupId}
            >
              <Input
                id="channel-filing-group"
                autoComplete="off"
                spellCheck={false}
                value={form.filingGroupId}
                disabled={disabled}
                aria-invalid={!!errors.filingGroupId}
                onChange={(event) => onPatch({ filingGroupId: event.target.value })}
              />
            </FormField>
            <FormField label="项目名" htmlFor="channel-filing-project">
              <Input
                id="channel-filing-project"
                autoComplete="off"
                spellCheck={false}
                placeholder="default"
                value={form.filingProjectName}
                disabled={disabled}
                onChange={(event) => onPatch({ filingProjectName: event.target.value })}
              />
            </FormField>
          </div>
          <FormField
            label={
              <span className="flex items-center gap-2">
                Access Key / Secret Key
                <Tag tone={keysSet ? "success" : "warning"}>{keysSet ? "已配置" : "未配置"}</Tag>
              </span>
            }
            htmlFor="channel-filing-ak"
            error={errors.filingKeys}
          >
            <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
              <Input
                id="channel-filing-ak"
                type="password"
                autoComplete="new-password"
                spellCheck={false}
                placeholder={keysSet ? "留空不改" : "Access Key"}
                value={accessKey}
                disabled={disabled}
                aria-label="Access Key"
                aria-invalid={!!errors.filingKeys}
                onChange={(event) => onAccessKey(event.target.value)}
              />
              <Input
                type="password"
                autoComplete="new-password"
                spellCheck={false}
                placeholder={keysSet ? "留空不改" : "Secret Key"}
                value={secretKey}
                disabled={disabled}
                aria-label="Secret Key"
                aria-invalid={!!errors.filingKeys}
                onChange={(event) => onSecretKey(event.target.value)}
              />
            </div>
          </FormField>
        </>
      )}
    </div>
  );
}
