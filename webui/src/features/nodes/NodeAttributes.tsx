import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Button } from "../../components/ui/Button";
import { useI18n } from "../../i18n";
import { formatApiErrorMessage } from "../../lib/error-message";
import { formatDateTime } from "../../lib/time";
import { probeAttributes } from "./api";
import type { NodeSummary } from "./types";

export function NodeAttributesPanel({ node }: { node: NodeSummary }) {
  const { t } = useI18n();
  const queryClient = useQueryClient();
  const probe = useMutation({
    mutationFn: () => probeAttributes(node.node_hash),
    onSuccess: async () => { await Promise.all([queryClient.invalidateQueries({ queryKey: ["node", node.node_hash] }), queryClient.invalidateQueries({ queryKey: ["nodes"] }), queryClient.invalidateQueries({ queryKey: ["egresses"] })]); },
  });
  const attributes = node.attributes;
  return <section className="platform-drawer-section">
    <div className="platform-drawer-section-head"><h4>{t("入口与出口属性")}</h4><Button variant="secondary" disabled={!node.has_outbound || probe.isPending} onClick={() => probe.mutate()}>{probe.isPending ? t("探测中...") : t("检测出口属性")}</Button></div>
    <div className="stats-grid">
      <div><span>{t("入口地址")}</span><p>{node.entry.protocol}://{node.entry.host}:{node.entry.port}</p></div>
      <div><span>{t("入口解析 IP")}</span><p>{node.entry_ips.join(", ") || "unknown"}</p></div>
      <div><span>{t("出口 IP")}</span><p>{node.egress_ip || "unknown"}</p></div>
      <div><span>{t("国家 / 城市")}</span><p>{attributes?.country_code || node.region?.toUpperCase() || "unknown"} / {attributes?.city || "unknown"}</p></div>
      <div><span>{t("公司/云商")}</span><p>{attributes?.company || "unknown"}</p></div>
      <div><span>{t("网络类型")}</span><p>{attributes?.network_type || "unknown"}</p></div>
      <div><span>{t("公司类型 / ASN 类型")}</span><p>{attributes?.company_type || "unknown"} / {attributes?.asn_type || "unknown"}</p></div>
      <div><span>ASN</span><p>{attributes?.asn || "unknown"} {attributes?.organization}</p></div>
      <div><span>Hosting / VPN / Proxy / Tor</span><p>{[attributes?.hosting, attributes?.is_vpn, attributes?.is_proxy, attributes?.is_tor].map((value) => value == null ? "unknown" : String(value)).join(" / ")}</p></div>
      <div><span>{t("属性来源 / 检测时间")}</span><p>{attributes?.provider || "-"} / {formatDateTime(attributes?.checked_at || "")}</p></div>
    </div>
    {attributes && !node.attributes_current ? <p className="muted">{t("此属性样本已过期或属于其他出口/来源；筛选和导出会视为未知，请重新检测。")}</p> : null}
    {node.attributes_error || probe.error ? <p className="callout callout-error">{probe.error ? formatApiErrorMessage(probe.error, t) : node.attributes_error}</p> : null}
    <p className="muted">{t("入口解析 IP 是 DNS 解析快照；出口 IP 是通过该节点访问检测服务的实测结果。")}</p>
  </section>;
}
