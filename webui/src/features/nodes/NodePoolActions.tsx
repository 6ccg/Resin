import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { Button } from "../../components/ui/Button";
import { Card } from "../../components/ui/Card";
import { Select } from "../../components/ui/Select";
import { useI18n } from "../../i18n";
import { formatApiErrorMessage } from "../../lib/error-message";
import { cancelBatch, exportNodes, getBatchStatus, startBatchProbes } from "./api";
import type { NodeListFilters } from "./types";

export function NodePoolActions({ filters, selected, clearSelection }: { filters: NodeListFilters; selected: string[]; clearSelection: () => void }) {
  const { t } = useI18n();
  const queryClient = useQueryClient();
  const [scope, setScope] = useState("filtered");
  const [kind, setKind] = useState("latency");
  const [format, setFormat] = useState("txt");
  const [onlyValid, setOnlyValid] = useState(true);
  const [dedupe, setDedupe] = useState(false);
  const scopedFilters: NodeListFilters = scope === "all" ? {} : scope === "selected" ? { node_hashes: selected } : filters;
  const batchQuery = useQuery({ queryKey: ["node-probe-job"], queryFn: getBatchStatus, refetchInterval: (query) => query.state.data?.running ? 1000 : 5000 });
  const job = batchQuery.data;
  const finishedJob = job && !job.running ? job.id : "";
  useEffect(() => {
    if (finishedJob) {
      void queryClient.invalidateQueries({ queryKey: ["nodes"] });
      void queryClient.invalidateQueries({ queryKey: ["egresses"] });
      void queryClient.invalidateQueries({ queryKey: ["node-diagnostics"] });
      void queryClient.invalidateQueries({ queryKey: ["node"] });
    }
  }, [finishedJob, queryClient]);
  const batch = useMutation({ mutationFn: () => startBatchProbes(kind, scopedFilters), onSuccess: (result) => queryClient.setQueryData(["node-probe-job"], result) });
  const cancel = useMutation({ mutationFn: () => cancelBatch(job!.id), onSuccess: (result) => queryClient.setQueryData(["node-probe-job"], result) });
  const download = useMutation({
    mutationFn: () => exportNodes(format, onlyValid ? { ...scopedFilters, validity: "valid" } : scopedFilters, dedupe),
    onSuccess: (result) => {
      if (!result.exported) return;
      const blob = new Blob([result.content], { type: result.content_type });
      const url = URL.createObjectURL(blob);
      const link = document.createElement("a"); link.href = url; link.download = result.filename; link.click();
      setTimeout(() => URL.revokeObjectURL(url), 1000);
    },
  });
  const disabled = scope === "selected" && !selected.length;
  return (
    <Card>
      <div className="list-card-header"><h3>{t("批量检测与导出")}</h3></div>
      <div className="form-grid">
        <div className="field-group">
          <label className="field-label" htmlFor="node-action-scope">{t("操作范围")}</label>
          <Select id="node-action-scope" value={scope} onChange={(event) => setScope(event.target.value)}>
            <option value="filtered">{t("当前筛选的全部结果")}</option>
            <option value="all">{t("全部节点")}</option>
            <option value="selected">{t("选中节点")} ({selected.length})</option>
          </Select>
          {selected.length ? <Button size="sm" variant="secondary" onClick={clearSelection}>{t("清空选择")}</Button> : null}
        </div>
        <div className="field-group">
          <label className="field-label" htmlFor="node-probe-kind">{t("检测类型")}</label>
          <Select id="node-probe-kind" value={kind} onChange={(event) => setKind(event.target.value)}>
            <option value="latency">{t("重新测试延迟")}</option><option value="egress">{t("出口探测")}</option>
            <option value="attributes">{t("出口属性检测")}</option><option value="targets">{t("指定目标可达性")}</option>
          </Select>
          <Button variant="secondary" disabled={disabled || batch.isPending || Boolean(job?.running)} onClick={() => batch.mutate()}>{t("开始批量检测")}</Button>
        </div>
        <div className="field-group">
          <label className="field-label" htmlFor="node-export-format">{t("导出格式")}</label>
          <Select id="node-export-format" value={format} onChange={(event) => setFormat(event.target.value)}>
            <option value="txt">TXT / URI</option><option value="base64">Base64 URI</option><option value="sing-box">sing-box JSON</option>
            <option value="clash">Clash YAML</option><option value="csv">CSV</option><option value="json">JSON</option>
          </Select>
          <label><input type="checkbox" checked={onlyValid} onChange={(event) => setOnlyValid(event.target.checked)} /> {t("仅导出检测有效的节点")}</label>
          <label><input type="checkbox" checked={dedupe} onChange={(event) => setDedupe(event.target.checked)} /> {t("每个出口只保留一个节点")}</label>
          <Button disabled={disabled || download.isPending} onClick={() => download.mutate()}>{download.isPending ? t("导出中...") : t("导出节点")}</Button>
        </div>
      </div>
      <p className="muted">{t("导出名称：[国家代码][网络类型][公司/云商]。未检测或已过期的属性显示 unknown。复杂协议与选项请使用 sing-box 或完整 JSON；无法转换的节点会列出原因。")}</p>
      {[batch.error, cancel.error, download.error, batchQuery.error].filter(Boolean).map((error, index) => <p className="callout callout-error" key={index}>{formatApiErrorMessage(error, t)}</p>)}
      {job ? <div>
        <p>{t("检测进度")} {job.completed}/{job.total} · {t("成功")} {job.successes} · {t("失败")} {job.failures} · {t("跳过")} {job.skipped} {job.cancelled ? t("已取消") : job.running ? t("进行中") : t("已完成")}</p>
        <progress aria-label={t("检测进度")} max={Math.max(1, job.total)} value={job.completed} />
        {job.running ? <Button variant="secondary" size="sm" disabled={cancel.isPending || job.cancelled} onClick={() => cancel.mutate()}>{t("取消剩余检测")}</Button> : null}
      </div> : null}
      {download.data ? <div><p>{t("已导出 {{count}} 个节点，无法转换 {{skipped}} 个。", { count: download.data.exported, skipped: download.data.skipped.length })}</p>
        {download.data.skipped.length ? <details><summary>{t("查看无法转换的节点")}</summary>{download.data.skipped.map((item) => <p key={item.node_hash}><code>{item.node_hash}</code> {item.reason}</p>)}</details> : null}
      </div> : null}
    </Card>
  );
}
