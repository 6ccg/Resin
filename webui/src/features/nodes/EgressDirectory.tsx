import { useQuery } from "@tanstack/react-query";
import { createColumnHelper } from "@tanstack/react-table";
import { useState } from "react";
import { Button } from "../../components/ui/Button";
import { Card } from "../../components/ui/Card";
import { DataTable } from "../../components/ui/DataTable";
import { OffsetPagination } from "../../components/ui/OffsetPagination";
import { useI18n } from "../../i18n";
import { formatApiErrorMessage } from "../../lib/error-message";
import { listEgresses } from "./api";
import type { EgressSummary, NodeListFilters } from "./types";

const col = createColumnHelper<EgressSummary>();
export function EgressDirectory({ filters, onSelect }: { filters: NodeListFilters; onSelect: (ip: string) => void }) {
  const { t } = useI18n();
  const [page, setPage] = useState(0);
  const [pageSize, setPageSize] = useState(50);
  const query = useQuery({ queryKey: ["egresses", filters, page, pageSize], queryFn: ({ signal }) => listEgresses({ ...filters, offset: page * pageSize, limit: pageSize }, signal), refetchInterval: 30000, staleTime: 15000, placeholderData: (previous) => previous });
  const columns = [
    col.accessor("ip", { header: t("出口 IP"), cell: (info) => <Button variant="secondary" size="sm" onClick={() => onSelect(info.getValue())}>{info.getValue()}</Button> }),
    col.accessor("region", { header: t("国家"), cell: (info) => info.getValue().toUpperCase() }),
    col.accessor("nodes", { header: t("关联入口数") }), col.accessor("valid_nodes", { header: t("有效入口数") }),
    col.display({ id: "type", header: t("网络类型"), cell: (info) => info.row.original.attributes?.network_type || "unknown" }),
    col.display({ id: "company", header: t("公司/云商"), cell: (info) => info.row.original.attributes?.company || "unknown" }),
  ];
  return <Card className="node-pool-card node-pool-results">
    <div className="node-pool-results-toolbar"><span className="node-pool-hint">{t("点击出口 IP 查看通向该出口的入口节点。")}</span><span className="node-pool-hint">{t("共 {{count}} 个出口", { count: query.data?.total ?? 0 })}{query.isFetching ? " · " + t("更新中...") : ""}</span></div>
    {query.isLoading ? <p role="status" className="node-pool-loading">{t("正在加载出口数据...")}</p> : null}
    {query.error ? <p className="callout callout-error">{formatApiErrorMessage(query.error, t)}</p> : null}
    <DataTable data={query.data?.items ?? []} columns={columns} className="node-pool-egress-table" wrapClassName="node-pool-table-wrap" />
    <OffsetPagination page={page} totalPages={Math.max(1, Math.ceil((query.data?.total ?? 0) / pageSize))} totalItems={query.data?.total ?? 0} pageSize={pageSize} pageSizeOptions={[50, 100, 200]} onPageChange={setPage} onPageSizeChange={(size) => { setPageSize(size); setPage(0); }} />
  </Card>;
}
