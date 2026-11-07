import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Download, X, Zap } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { Button } from "../../components/ui/Button";
import { Card } from "../../components/ui/Card";
import { Select } from "../../components/ui/Select";
import { useI18n } from "../../i18n";
import { formatApiErrorMessage } from "../../lib/error-message";
import { cancelBatch, exportNodes, getBatchStatus, startBatchProbes } from "./api";
import type { BatchStatus, NodeListFilters } from "./types";

function saveDownload(content: string, type: string, name: string) {
  const url = URL.createObjectURL(new Blob([content], { type }));
  const link = document.createElement("a");
  link.href = url;
  link.download = name;
  link.click();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
}

export function NodePoolActions({ open, filters, selected, clearSelection }: { open: boolean; filters: NodeListFilters; selected: string[]; clearSelection: () => void }) {
  const { t } = useI18n();
  const queryClient = useQueryClient();
  const [scope, setScope] = useState("filtered");
  const [kind, setKind] = useState("latency");
  const [format, setFormat] = useState("txt");
  const [onlyValid, setOnlyValid] = useState(true);
  const [dedupe, setDedupe] = useState(false);
  const scopedFilters: NodeListFilters = scope === "all" ? {} : scope === "selected" ? { node_hashes: selected } : filters;
  const batchQuery = useQuery({ queryKey: ["node-probe-job"], queryFn: getBatchStatus, refetchInterval: (query) => query.state.data?.running ? 1000 : 15000, staleTime: 5000 });
  const job = batchQuery.data;
  const previousJob = useRef<BatchStatus | null>(null);
  useEffect(() => {
    if (job && previousJob.current?.id === job.id && previousJob.current.running && !job.running) {
      void queryClient.invalidateQueries({ queryKey: ["nodes"] });
      void queryClient.invalidateQueries({ queryKey: ["egresses"] });
      void queryClient.invalidateQueries({ queryKey: ["node-diagnostics"] });
      void queryClient.invalidateQueries({ queryKey: ["node"] });
    }
    previousJob.current = job ?? null;
  }, [job, queryClient]);
  const batch = useMutation({ mutationFn: () => startBatchProbes(kind, scopedFilters), onSuccess: (result) => queryClient.setQueryData(["node-probe-job"], result) });
  const cancel = useMutation({ mutationFn: () => cancelBatch(job!.id), onSuccess: (result) => queryClient.setQueryData(["node-probe-job"], result) });
  const download = useMutation({
    mutationFn: () => exportNodes(format, onlyValid ? { ...scopedFilters, validity: "valid" } : scopedFilters, dedupe),
    onSuccess: (result) => {
      if (result.exported) saveDownload(result.content, result.content_type, result.filename);
    },
  });
  const disabled = scope === "selected" && !selected.length;
  if (!open && !job?.running) return null;
  return <Card className="node-pool-card node-pool-tools" id="node-pool-tools">
    {open ? <>
      <div className="node-pool-scope">
        <label htmlFor="node-action-scope">{t("操作范围")}</label>
        <Select id="node-action-scope" value={scope} onChange={(event) => setScope(event.target.value)}>
          <option value="filtered">{t("当前筛选的全部结果")}</option><option value="all">{t("全部节点")}</option><option value="selected">{t("选中节点")} ({selected.length})</option>
        </Select>
        {selected.length ? <Button size="sm" variant="ghost" onClick={clearSelection}><X size={14} />{t("清空选择")}</Button> : null}
      </div>
      <div className="node-pool-tool-grid">
        <section className="node-pool-tool-section">
          <h3><Zap size={16} />{t("批量检测")}</h3>
          <div className="node-pool-tool-controls">
            <Select aria-label={t("检测类型")} value={kind} onChange={(event) => setKind(event.target.value)}>
              <option value="latency">{t("重新测试延迟")}</option><option value="egress">{t("出口探测")}</option><option value="attributes">{t("出口属性检测")}</option><option value="targets">{t("指定目标可达性")}</option>
            </Select>
            <Button size="sm" variant="secondary" disabled={disabled || batch.isPending || Boolean(job?.running)} onClick={() => batch.mutate()}>{batch.isPending ? t("启动中...") : t("开始检测")}</Button>
          </div>
          <p className="node-pool-hint">{t("指定目标可在系统设置中配置，检测过程支持取消。")}</p>
        </section>
        <section className="node-pool-tool-section">
          <h3><Download size={16} />{t("导出节点")}</h3>
          <div className="node-pool-tool-controls">
            <Select aria-label={t("导出格式")} value={format} onChange={(event) => setFormat(event.target.value)}><option value="txt">TXT / URI</option><option value="base64">Base64 URI</option><option value="sing-box">sing-box JSON</option><option value="clash">Clash YAML</option><option value="csv">CSV</option><option value="json">JSON</option></Select>
            <Button size="sm" disabled={disabled || download.isPending} onClick={() => download.mutate()}><Download size={14} />{download.isPending ? t("导出中...") : t("导出")}</Button>
          </div>
          <div className="node-pool-export-options">
            <label><input type="checkbox" checked={onlyValid} onChange={(event) => setOnlyValid(event.target.checked)} />{t("仅有效节点")}</label>
            <label><input type="checkbox" checked={dedupe} onChange={(event) => setDedupe(event.target.checked)} />{t("按出口去重")}</label>
          </div>
          <p className="node-pool-hint">{t("节点名包含国家、网络类型和公司/云商；复杂选项建议使用 sing-box 或 JSON。")}</p>
        </section>
      </div>
    </> : null}
    {[batch.error, cancel.error, download.error, batchQuery.error].filter(Boolean).map((error, index) => <p className="callout callout-error" key={index}>{formatApiErrorMessage(error, t)}</p>)}
    {job ? <div className="node-pool-progress">
      <div><strong>{job.cancelled ? t("已取消") : job.running ? t("检测进行中") : t("检测已完成")}</strong><span>{job.completed.toLocaleString()} / {job.total.toLocaleString()} · {t("成功")} {job.successes} · {t("失败")} {job.failures} · {t("跳过")} {job.skipped}</span>
        {job.running ? <Button variant="ghost" size="sm" disabled={cancel.isPending || job.cancelled} onClick={() => cancel.mutate()}>{t("取消检测")}</Button> : null}
      </div>
      <progress aria-label={t("检测进度")} max={Math.max(1, job.total)} value={job.completed} />
    </div> : null}
    {open && download.data ? <div className="node-pool-export-result"><p>{t("已导出 {{count}} 个节点，无法转换 {{skipped}} 个。", { count: download.data.exported, skipped: download.data.skipped.length })}</p>
      {download.data.skipped.length ? <details><summary>{t("查看无法转换的节点")}</summary>{download.data.skipped.slice(0, 20).map((item) => <p key={item.node_hash}><code>{item.node_hash}</code> {item.reason}</p>)}<Button size="sm" variant="secondary" onClick={() => saveDownload(JSON.stringify(download.data!.skipped, null, 2), "application/json", "resin-export-skipped.json")}>{t("下载完整跳过报告")}</Button></details> : null}
    </div> : null}
  </Card>;
}
