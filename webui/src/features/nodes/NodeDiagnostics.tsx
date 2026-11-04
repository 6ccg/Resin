import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { createColumnHelper } from "@tanstack/react-table";
import { useState } from "react";
import { Badge } from "../../components/ui/Badge";
import { Button } from "../../components/ui/Button";
import { DataTable } from "../../components/ui/DataTable";
import { Input } from "../../components/ui/Input";
import { Select } from "../../components/ui/Select";
import { useI18n } from "../../i18n";
import { formatApiErrorMessage } from "../../lib/error-message";
import { formatDateTime } from "../../lib/time";
import { getSystemConfig } from "../systemConfig/api";
import { checkNodeTarget, getNodeDiagnostics } from "./api";
import type { ProbeObservation } from "./types";

const col = createColumnHelper<ProbeObservation>();

export function NodeDiagnosticsPanel({ hash, ready }: { hash: string; ready: boolean }) {
  const { t } = useI18n();
  const queryClient = useQueryClient();
  const [draftURL, setDraftURL] = useState<string | null>(null);
  const [statuses, setStatuses] = useState("");
  const [scope, setScope] = useState("all");
  const config = useQuery({ queryKey: ["system-config"], queryFn: getSystemConfig });
  const diagnostics = useQuery({
    queryKey: ["node-diagnostics", hash],
    queryFn: () => getNodeDiagnostics(hash),
    refetchInterval: 5000,
  });
  const targetURL = draftURL ?? config.data?.reachability_targets[0] ?? "";
  const check = useMutation({
    mutationFn: async () => {
      const expected = statuses.trim() ? statuses.split(",").map((value) => Number(value.trim())) : [];
      if (expected.some((code) => !Number.isInteger(code) || code < 100 || code > 599)) {
        throw new Error(t("预期状态码须为 100 到 599 的整数，以逗号分隔。"));
      }
      return checkNodeTarget(hash, targetURL.trim(), expected);
    },
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["node-diagnostics", hash] }),
        queryClient.invalidateQueries({ queryKey: ["nodes"] }),
      ]);
    },
  });
  const stats = diagnostics.data?.stability;
  const checks = diagnostics.data?.checks.filter((sample) => scope === "all" || (scope === "target" ? sample.kind === "target" : sample.kind !== "target")) ?? [];
  const columns = [
    col.accessor("checked_at", { header: t("检测时间"), cell: (info) => formatDateTime(info.getValue()) }),
    col.accessor("kind", {
      header: t("检测类型"),
      cell: (info) => info.getValue() === "target" ? t("指定目标") : info.getValue() === "egress" ? t("出口探测") : t("延迟探测"),
    }),
    col.accessor("target", { header: t("检测目标"), cell: (info) => <span title={info.getValue()}>{info.getValue()}</span> }),
    col.accessor("success", {
      header: t("结果"),
      cell: (info) => <Badge variant={info.getValue() ? "success" : "danger"}>{info.getValue() ? t("成功") : t("失败")}</Badge>,
    }),
    col.accessor("http_status", { header: t("HTTP 状态码"), cell: (info) => info.getValue() || "-" }),
    col.accessor("duration_ms", { header: t("耗时"), cell: (info) => `${info.getValue().toFixed(1)} ms` }),
    col.accessor("latency_ms", { header: t("本次网络延迟"), cell: (info) => info.getValue() ? `${info.getValue()!.toFixed(1)} ms` : "-" }),
    col.accessor("error", { header: t("错误"), cell: (info) => info.getValue() || "-" }),
  ];

  return (
    <section className="platform-drawer-section">
      <div className="platform-drawer-section-head">
        <h4>{t("稳定性统计")}</h4>
        <p>{t("基于最近 50 次通用探活；指定目标检测单独记录。耗时为完整探测请求耗时。")}</p>
      </div>
      {diagnostics.error ? <p className="callout callout-error">{formatApiErrorMessage(diagnostics.error, t)}</p> : null}
      {stats && stats.samples > 0 ? (
        <div className="stats-grid">
          <div><span>{t("探测成功率")}</span><p>{(stats.success_rate * 100).toFixed(1)}% ({stats.successes}/{stats.samples})</p></div>
          <div><span>{t("连续探测失败")}</span><p>{stats.consecutive_failures}</p></div>
          <div><span>{t("成功探测平均耗时")}</span><p>{stats.successes ? `${stats.average_duration_ms.toFixed(1)} ms` : "-"}</p></div>
          <div><span>{t("耗时标准差")}</span><p>{stats.successes ? `${stats.duration_stddev_ms.toFixed(1)} ms` : "-"}</p></div>
          <div><span>{t("统计开始")}</span><p>{formatDateTime(stats.first_checked_at || "")}</p></div>
          <div><span>{t("最近探测")}</span><p>{formatDateTime(stats.last_checked_at || "")}</p></div>
        </div>
      ) : <p className="muted">{t("暂无通用探活样本，执行出口或延迟探测后生成统计。")}</p>}

      <div className="platform-drawer-section-head"><h4>{t("指定目标可达性")}</h4></div>
      <form className="form-grid" onSubmit={(event) => { event.preventDefault(); check.mutate(); }}>
        <div className="field-group field-span-2">
          <label className="field-label" htmlFor={`target-url-${hash}`}>{t("检测目标 URL")}</label>
          <Input id={`target-url-${hash}`} type="url" required value={targetURL} list={`targets-${hash}`} placeholder="https://example.com/health" onChange={(event) => setDraftURL(event.target.value)} />
          <datalist id={`targets-${hash}`}>{config.data?.reachability_targets.map((url) => <option key={url} value={url} />)}</datalist>
        </div>
        <div className="field-group">
          <label className="field-label" htmlFor={`target-status-${hash}`}>{t("预期 HTTP 状态码")}</label>
          <Input id={`target-status-${hash}`} value={statuses} placeholder={t("留空接受 2xx/3xx")} onChange={(event) => setStatuses(event.target.value)} />
        </div>
        <div className="field-group"><Button type="submit" disabled={!ready || check.isPending}>{check.isPending ? t("探测中...") : t("检测目标")}</Button></div>
      </form>
      <p className="muted">{t("通过此节点访问指定 URL，不跟随重定向。此结果不影响节点全局熔断；定期目标在系统设置中管理。")}</p>
      {check.error ? <p className="callout callout-error">{formatApiErrorMessage(check.error, t)}</p> : null}
      {check.data ? <p className={check.data.success ? "muted" : "callout callout-error"}>{check.data.success ? t("目标检测成功") : t("目标检测失败")} · {check.data.http_status || "-"} · {check.data.duration_ms.toFixed(1)} ms {check.data.error}</p> : null}

      <div className="platform-drawer-section-head">
        <h4>{t("最近检测记录")}</h4>
        <Select aria-label={t("检测类型")} value={scope} onChange={(event) => setScope(event.target.value)}>
          <option value="all">{t("全部检测")}</option>
          <option value="general">{t("通用探活")}</option>
          <option value="target">{t("指定目标")}</option>
        </Select>
      </div>
      <DataTable data={checks} columns={columns} />
      {!checks.length ? <p className="muted">{t("暂无检测记录")}</p> : null}
    </section>
  );
}
