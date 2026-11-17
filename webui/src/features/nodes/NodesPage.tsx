import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { createColumnHelper } from "@tanstack/react-table";
import { AlertTriangle, ArrowDownUp, ChevronDown, Eraser, Globe, RefreshCw, Search, SlidersHorizontal, Sparkles, X, Zap } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useLocation } from "react-router-dom";
import { Badge } from "../../components/ui/Badge";
import { Button } from "../../components/ui/Button";
import { DataTable } from "../../components/ui/DataTable";
import { Card } from "../../components/ui/Card";
import { Input } from "../../components/ui/Input";
import { OffsetPagination } from "../../components/ui/OffsetPagination";
import { Select } from "../../components/ui/Select";
import { ToastContainer } from "../../components/ui/Toast";
import { useToast } from "../../hooks/useToast";
import { useI18n } from "../../i18n";
import { formatApiErrorMessage } from "../../lib/error-message";
import { formatDateTime, formatRelativeTime } from "../../lib/time";
import { listPlatforms } from "../platforms/api";
import type { Platform } from "../platforms/types";
import { listSubscriptions } from "../subscriptions/api";
import { getNode, listNodes, probeEgress, probeLatency } from "./api";
import { NodeDiagnosticsPanel } from "./NodeDiagnostics";
import { NodePoolActions } from "./NodePoolActions";
import { EgressDirectory } from "./EgressDirectory";
import { NodeAttributesPanel } from "./NodeAttributes";
import type { NodeSummary } from "./types";
import { getAllRegions, getRegionName } from "./regions";
import type { NodeListFilters, NodeSortBy, SortOrder } from "./types";
import "./nodes.css";

type NodeStatusFilter = "all" | "healthy" | "circuit_open" | "error" | "disabled";
type NodeDisplayStatus = "healthy" | "circuit_open" | "pending_test" | "error" | "disabled";
type ProbeAction = "egress" | "latency";

type NodeFilterDraft = {
  platform_id: string;
  subscription_id: string;
  tag_keyword: string;
  region: string;
  egress_ip: string;
  status: NodeStatusFilter;
  entry_keyword: string;
  network_types: string;
  exclude_regions: string;
  hosting: string;
  company_keyword: string;
  asn_keyword: string;
  validity: string;
};

const defaultFilterDraft: NodeFilterDraft = {
  platform_id: "",
  subscription_id: "",
  tag_keyword: "",
  region: "",
  egress_ip: "",
  status: "all",
  entry_keyword: "", network_types: "", exclude_regions: "", hosting: "", company_keyword: "", asn_keyword: "", validity: "",
};

const PAGE_SIZE_OPTIONS = [20, 50, 100, 200] as const;
const EMPTY_PLATFORMS: Platform[] = [];
const col = createColumnHelper<NodeSummary>();
const EMPTY_NODES: NodeSummary[] = [];

function parseBoolParam(value: string | null): boolean | undefined {
  if (value === null) {
    return undefined;
  }

  const normalized = value.trim().toLowerCase();
  if (normalized === "true" || normalized === "1") {
    return true;
  }
  if (normalized === "false" || normalized === "0") {
    return false;
  }

  return undefined;
}

function parseStatusParam(value: string | null): NodeStatusFilter | undefined {
  if (value === null) {
    return undefined;
  }

  const normalized = value.trim().toLowerCase();
  if (normalized === "all" || normalized === "healthy" || normalized === "circuit_open" || normalized === "error" || normalized === "disabled") {
    return normalized;
  }

  return undefined;
}

function statusFromQuery(params: URLSearchParams): NodeStatusFilter {
  const explicitStatus = parseStatusParam(params.get("status"));
  if (explicitStatus) {
    return explicitStatus;
  }

  const hasOutbound = parseBoolParam(params.get("has_outbound"));
  const circuitOpen = parseBoolParam(params.get("circuit_open"));
  const enabled = parseBoolParam(params.get("enabled"));

  if (enabled === false) {
    return "disabled";
  }

  if (hasOutbound === false) {
    return "error";
  }
  if (hasOutbound === true && circuitOpen === true) {
    return "circuit_open";
  }
  if (hasOutbound === true && circuitOpen === false) {
    return "healthy";
  }

  return "all";
}

function trimQueryValue(params: URLSearchParams, key: string): string {
  return params.get(key)?.trim() ?? "";
}

function draftFromQuery(search: string): NodeFilterDraft {
  const params = new URLSearchParams(search);
  const tagKeyword = trimQueryValue(params, "tag_keyword") || trimQueryValue(params, "tag");

  return {
    platform_id: trimQueryValue(params, "platform_id"),
    subscription_id: trimQueryValue(params, "subscription_id"),
    tag_keyword: tagKeyword,
    region: trimQueryValue(params, "region").toUpperCase(),
    egress_ip: trimQueryValue(params, "egress_ip"),
    status: statusFromQuery(params),
    entry_keyword: trimQueryValue(params, "entry_keyword"),
    network_types: trimQueryValue(params, "network_types"),
    exclude_regions: trimQueryValue(params, "exclude_regions"),
    hosting: trimQueryValue(params, "hosting"),
    company_keyword: trimQueryValue(params, "company_keyword"),
    asn_keyword: trimQueryValue(params, "asn_keyword"),
    validity: trimQueryValue(params, "validity"),
  };
}



function draftToActiveFilters(draft: NodeFilterDraft): NodeListFilters {
  let circuit_open: boolean | undefined = undefined;
  let has_outbound: boolean | undefined = undefined;
  let enabled: boolean | undefined = undefined;

  switch (draft.status) {
    case "healthy":
      enabled = true;
      has_outbound = true;
      circuit_open = false;
      break;
    case "circuit_open":
      enabled = true;
      has_outbound = true;
      circuit_open = true;
      break;
    case "error":
      enabled = true;
      has_outbound = false;
      break;
    case "disabled":
      enabled = false;
      break;
    case "all":
    default:
      break;
  }

  return {
    platform_id: draft.platform_id,
    subscription_id: draft.subscription_id,
    tag_keyword: draft.tag_keyword,
    region: draft.region,
    egress_ip: draft.egress_ip,
    entry_keyword: draft.entry_keyword,
    network_types: draft.network_types.split(",").map((value) => value.trim()).filter(Boolean),
    exclude_regions: draft.exclude_regions.split(",").map((value) => value.trim()).filter(Boolean),
    company_keyword: draft.company_keyword,
    asn_keyword: draft.asn_keyword,
    validity: draft.validity,
    hosting: draft.hosting === "true" ? true : draft.hosting === "false" ? false : undefined,
    hosting_unknown: draft.hosting === "unknown",
    enabled,
    circuit_open,
    has_outbound,
  };
}

function firstTag(node: { display_tag?: string; tags: { tag: string }[] }): string {
  if (node.display_tag && node.display_tag.trim()) {
    return node.display_tag;
  }
  if (!node.tags.length) {
    return "-";
  }
  return node.tags[0].tag;
}

function hasReferenceLatency(node: NodeSummary): node is NodeSummary & { reference_latency_ms: number } {
  return typeof node.reference_latency_ms === "number";
}

function isPendingTestNode(node: NodeSummary): boolean {
  return Boolean(node.circuit_open_since) && node.failure_count === 0;
}

function getNodeDisplayStatus(node: NodeSummary): NodeDisplayStatus {
  if (!node.enabled) {
    return "disabled";
  }
  if (!node.has_outbound) {
    return "error";
  }
  if (isPendingTestNode(node)) {
    return "pending_test";
  }
  if (node.circuit_open_since) {
    return "circuit_open";
  }
  return "healthy";
}

function referenceLatencyColor(latencyMs: number): string {
  if (!Number.isFinite(latencyMs)) {
    return "var(--text-secondary)";
  }
  if (latencyMs <= 400) {
    return "var(--success)";
  }
  if (latencyMs <= 1000) {
    return "var(--warning)";
  }
  return "var(--danger)";
}

function displayableReferenceLatencyMs(node: NodeSummary): number | null {
  if (getNodeDisplayStatus(node) !== "healthy") {
    return null;
  }
  if (!hasReferenceLatency(node)) {
    return null;
  }
  return node.reference_latency_ms;
}


function formatLatency(value: number): string {
  if (!Number.isFinite(value)) {
    return "-";
  }
  return `${value.toFixed(0)} ms`;
}

function sortIndicator(active: boolean, order: SortOrder): string {
  if (!active) {
    return "↕";
  }
  return order === "asc" ? "▲" : "▼";
}

function regionToFlag(region: string | undefined): string {
  if (!region || region.length !== 2) {
    return region || "-";
  }
  const code = region.toUpperCase();
  const flag = String.fromCodePoint(...[...code].map((c) => c.charCodeAt(0) + 127397));
  const name = getRegionName(code);
  return name ? `${flag} ${code} (${name})` : `${flag} ${code}`;
}

export function NodesPage() {
  const { locale, t } = useI18n();
  const location = useLocation();
  const [draftFilters, setDraftFilters] = useState<NodeFilterDraft>(() => draftFromQuery(location.search));
  const [activeFilters, setActiveFilters] = useState<NodeListFilters>(() =>
    draftToActiveFilters(draftFromQuery(location.search))
  );
  const [sortBy, setSortBy] = useState<NodeSortBy>("tag");
  const [sortOrder, setSortOrder] = useState<SortOrder>("asc");
  const [page, setPage] = useState(0);
  const [pageSize, setPageSize] = useState<number>(50);
  const [selectedNodeHash, setSelectedNodeHash] = useState("");
  const [drawerOpen, setDrawerOpen] = useState(false);
  const [view, setView] = useState("nodes");
  const [advancedOpen, setAdvancedOpen] = useState(false);
  const [toolsOpen, setToolsOpen] = useState(false);
  const [checkedHashes, setCheckedHashes] = useState<Set<string>>(new Set());
  const [pendingEgressHashes, setPendingEgressHashes] = useState<Set<string>>(() => new Set());
  const [pendingLatencyHashes, setPendingLatencyHashes] = useState<Set<string>>(() => new Set());
  const { toasts, showToast, dismissToast } = useToast();
  const pendingEgressHashesRef = useRef<Set<string>>(new Set());
  const pendingLatencyHashesRef = useRef<Set<string>>(new Set());

  const queryClient = useQueryClient();

  const allRegions = useMemo(() => { void locale; return getAllRegions(); }, [locale]);

  useEffect(() => {
    const timer = window.setTimeout(() => {
      setActiveFilters(draftToActiveFilters(draftFilters));
      setPage(0);
    }, 300);
    return () => window.clearTimeout(timer);
  }, [draftFilters]);

  const platformsQuery = useQuery({
    queryKey: ["platforms", "all"],
    queryFn: async () => {
      const data = await listPlatforms({
        limit: 100000,
        offset: 0,
      });
      return data.items;
    },
    staleTime: 60_000,
  });
  const platforms = platformsQuery.data ?? EMPTY_PLATFORMS;

  const subscriptionsQuery = useQuery({
    queryKey: ["subscriptions", "all"],
    queryFn: async () => {
      const data = await listSubscriptions({
        limit: 100000,
        offset: 0,
      });
      return data.items;
    },
    staleTime: 60_000,
  });
  const subscriptions = subscriptionsQuery.data ?? [];

  const nodesQuery = useQuery({
    queryKey: ["nodes", activeFilters, sortBy, sortOrder, page, pageSize],
    queryFn: ({ signal }) =>
      listNodes({
        ...activeFilters,
        sort_by: sortBy,
        sort_order: sortOrder,
        limit: pageSize,
        offset: page * pageSize,
      }, signal),
    enabled: view === "nodes",
    staleTime: 15_000,
    refetchInterval: 30_000,
    placeholderData: (prev) => prev,
  });

  const nodesPage = nodesQuery.data ?? {
    items: EMPTY_NODES,
    total: 0,
    limit: pageSize,
    offset: page * pageSize,
    unique_egress_ips: 0,
    unique_healthy_egress_ips: 0,
  };
  const nodes = nodesPage.items;
  const advancedCount = Object.entries(draftFilters).filter(([key, value]) => !["tag_keyword", "region", "validity"].includes(key) && value !== "" && !(key === "status" && value === "all")).length;
  const hasFilters = Object.entries(draftFilters).some(([key, value]) => value !== "" && !(key === "status" && value === "all"));

  const totalPages = Math.max(1, Math.ceil(nodesPage.total / pageSize));

  const selectedNode = useMemo(() => {
    if (!selectedNodeHash) {
      return null;
    }
    return nodes.find((item) => item.node_hash === selectedNodeHash) ?? null;
  }, [nodes, selectedNodeHash]);

  const selectedHash = selectedNode?.node_hash || "";

  const nodeDetailQuery = useQuery({
    queryKey: ["node", selectedHash],
    queryFn: () => getNode(selectedHash),
    enabled: Boolean(selectedHash) && drawerOpen,
    refetchInterval: 30_000,
  });

  const detailNode = nodeDetailQuery.data ?? selectedNode;
  const drawerVisible = drawerOpen && Boolean(detailNode);

  useEffect(() => {
    if (!drawerVisible) {
      return;
    }

    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key !== "Escape") {
        return;
      }
      setDrawerOpen(false);
    };

    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [drawerVisible]);

  const openDrawer = useCallback((hash: string) => {
    setSelectedNodeHash(hash);
    setDrawerOpen(true);
  }, []);

  const refreshNodes = async () => {
    await queryClient.invalidateQueries({ queryKey: ["nodes"] });
    await queryClient.invalidateQueries({ queryKey: ["egresses"] });
    if (selectedHash) {
      await queryClient.invalidateQueries({ queryKey: ["node", selectedHash] });
      await queryClient.invalidateQueries({ queryKey: ["node-diagnostics", selectedHash] });
    }
  };

  const probeEgressMutation = useMutation({
    mutationFn: async (hash: string) => probeEgress(hash),
    onSuccess: async (result) => {
      await refreshNodes();
      showToast(
        "success",
        t("出口探测完成：出口 IP={{ip}}，区域={{region}}，延迟={{latency}}", {
          ip: result.egress_ip || "-",
          region: result.region || "-",
          latency: formatLatency(result.latency_ewma_ms),
        })
      );
    },
    onError: async (error) => {
      await refreshNodes();
      showToast("error", formatApiErrorMessage(error, t));
    },
  });

  const probeLatencyMutation = useMutation({
    mutationFn: async (hash: string) => probeLatency(hash),
    onSuccess: async (result) => {
      await refreshNodes();
      showToast("success", t("延迟探测完成：本次 {{latency}}，平滑 {{ewma}}", { latency: formatLatency(result.latency_ms), ewma: formatLatency(result.latency_ewma_ms) }));
    },
    onError: async (error) => {
      await refreshNodes();
      showToast("error", formatApiErrorMessage(error, t));
    },
  });

  const mutateEgress = probeEgressMutation.mutateAsync;
  const mutateLatency = probeLatencyMutation.mutateAsync;

  const markProbePending = useCallback((hash: string, action: ProbeAction): boolean => {
    if (action === "egress") {
      if (pendingEgressHashesRef.current.has(hash)) {
        return false;
      }
      const next = new Set(pendingEgressHashesRef.current);
      next.add(hash);
      pendingEgressHashesRef.current = next;
      setPendingEgressHashes(next);
      return true;
    }

    if (pendingLatencyHashesRef.current.has(hash)) {
      return false;
    }
    const next = new Set(pendingLatencyHashesRef.current);
    next.add(hash);
    pendingLatencyHashesRef.current = next;
    setPendingLatencyHashes(next);
    return true;
  }, []);

  const clearProbePending = useCallback((hash: string, action: ProbeAction) => {
    if (action === "egress") {
      if (!pendingEgressHashesRef.current.has(hash)) {
        return;
      }
      const next = new Set(pendingEgressHashesRef.current);
      next.delete(hash);
      pendingEgressHashesRef.current = next;
      setPendingEgressHashes(next);
      return;
    }

    if (!pendingLatencyHashesRef.current.has(hash)) {
      return;
    }
    const next = new Set(pendingLatencyHashesRef.current);
    next.delete(hash);
    pendingLatencyHashesRef.current = next;
    setPendingLatencyHashes(next);
  }, []);

  const isProbePending = useCallback((hash: string, action: ProbeAction): boolean =>
    action === "egress" ? pendingEgressHashes.has(hash) : pendingLatencyHashes.has(hash), [pendingEgressHashes, pendingLatencyHashes]);

  const runProbeEgress = useCallback(async (hash: string) => {
    if (!markProbePending(hash, "egress")) {
      return;
    }
    try {
      await mutateEgress(hash);
    } catch {
      // Mutation callbacks already surface the failure to the user.
    } finally {
      clearProbePending(hash, "egress");
    }
  }, [markProbePending, clearProbePending, mutateEgress]);

  const runProbeLatency = useCallback(async (hash: string) => {
    if (!markProbePending(hash, "latency")) {
      return;
    }
    try {
      await mutateLatency(hash);
    } catch {
      // Mutation callbacks already surface the failure to the user.
    } finally {
      clearProbePending(hash, "latency");
    }
  }, [markProbePending, clearProbePending, mutateLatency]);

  const handleFilterChange = (key: keyof NodeFilterDraft, value: string) => {
    setDraftFilters((prev) => ({ ...prev, [key]: value }));
    setSelectedNodeHash("");
    setDrawerOpen(false);
    setCheckedHashes(new Set());
  };

  const resetFilters = () => {
    setDraftFilters(defaultFilterDraft);
    setActiveFilters(draftToActiveFilters(defaultFilterDraft));
    setSelectedNodeHash("");
    setDrawerOpen(false);
    setPage(0);
    setCheckedHashes(new Set());
  };

  const changeSort = useCallback((target: NodeSortBy) => {
    if (sortBy === target) {
      setSortOrder((prev) => (prev === "asc" ? "desc" : "asc"));
    } else {
      setSortBy(target);
      setSortOrder("asc");
    }
    setPage(0);
  }, [sortBy]);

  const changePageSize = (next: number) => {
    setPageSize(next);
    setPage(0);
  };

  const nodeColumns = useMemo(() => [
    col.display({
      id: "selected", size: 36,
      header: () => <input type="checkbox" aria-label={t("选择本页节点")} checked={Boolean(nodes.length) && nodes.every((node) => checkedHashes.has(node.node_hash))} onChange={(event) => setCheckedHashes((previous) => { const next = new Set(previous); for (const node of nodes) { if (event.target.checked) next.add(node.node_hash); else next.delete(node.node_hash); } return next; })} />,
      cell: (info) => <span onClick={(event) => event.stopPropagation()}><input type="checkbox" aria-label={t("选择节点")} checked={checkedHashes.has(info.row.original.node_hash)} onChange={(event) => setCheckedHashes((previous) => { const next = new Set(previous); if (event.target.checked) next.add(info.row.original.node_hash); else next.delete(info.row.original.node_hash); return next; })} /></span>,
    }),
    col.accessor((row) => firstTag(row), {
      id: "tag", size: 190,
      header: () => <button type="button" className="table-sort-btn" onClick={() => changeSort("tag")}>{t("节点名称")}<span>{sortIndicator(sortBy === "tag", sortOrder)}</span></button>,
      cell: (info) => {
        const node = info.row.original;
        const name = info.getValue();
        const slash = name.indexOf("/");
        return <div className="node-pool-cell node-pool-name" title={name}>
          <strong>{slash >= 0 ? name.slice(slash + 1) : name}</strong>
          <small><span className="node-pool-protocol">{node.entry.protocol || "-"}</span>{slash >= 0 ? name.slice(0, slash) : node.node_hash.slice(0, 8)}</small>
        </div>;
      },
    }),
    col.display({
      id: "entry", size: 155, header: t("入口地址"),
      cell: (info) => {
        const node = info.row.original;
        const host = node.entry.host.includes(":") ? "[" + node.entry.host + "]" : node.entry.host;
        return <div className="node-pool-cell node-pool-address" title={node.entry_ips.join(", ")}><code>{host || "-"}:{node.entry.port}</code></div>;
      },
    }),
    col.accessor("egress_ip", {
      size: 150,
      header: () => <button type="button" className="table-sort-btn" onClick={() => changeSort("region")}>{t("出口 / 国家")}<span>{sortIndicator(sortBy === "region", sortOrder)}</span></button>,
      cell: (info) => <div className="node-pool-cell node-pool-address"><code title={info.getValue()}>{info.getValue() || t("待探测")}</code><small title={regionToFlag(info.row.original.region)}>{regionToFlag(info.row.original.region)}</small></div>,
    }),
    col.display({
      id: "attributes", size: 155, header: t("出口属性"),
      cell: (info) => {
        const node = info.row.original;
        const attrs = node.attributes_current ? node.attributes : undefined;
        return <div className="node-pool-cell node-pool-attributes"><span>{attrs?.network_type || t("未检测")}</span><small title={attrs?.company}>{attrs?.company || attrs?.organization || "—"}</small></div>;
      },
    }),
    col.display({
      id: "status", size: 108, header: t("有效性 / 状态"),
      cell: (info) => {
        const node = info.row.original;
        const value = node.validity;
        const statusLabel = { healthy: "健康", circuit_open: "熔断", pending_test: "待测", error: "错误", disabled: "禁用" }[getNodeDisplayStatus(node)];
        return <div className="node-pool-cell"><Badge variant={value === "valid" ? "success" : value === "invalid" ? "danger" : "neutral"}>{t(({ valid: "有效", invalid: "无效", expired: "已过期", untested: "未测试", disabled: "禁用", error: "错误" } as Record<string, string>)[value] || "未知")}</Badge><small>{t(statusLabel)}{node.failure_count > 0 ? " · " + t("失败 {{count}} 次", { count: node.failure_count }) : ""}</small></div>;
      },
    }),
    col.display({
      id: "latency", size: 115, header: t("延迟 / 上次探测"),
      cell: (info) => {
        const node = info.row.original;
        const latency = displayableReferenceLatencyMs(node);
        return <div className="node-pool-cell"><strong style={latency === null ? undefined : { color: referenceLatencyColor(latency) }}>{latency === null ? "—" : formatLatency(latency)}</strong><small title={formatDateTime(node.last_latency_probe_attempt || "")}>{formatRelativeTime(node.last_latency_probe_attempt)}</small></div>;
      },
    }),
    col.display({
      id: "stability", size: 85, header: t("稳定性"),
      cell: (info) => {
        const stats = info.row.original.probe_stability;
        return <div className="node-pool-cell"><strong>{stats ? (stats.success_rate * 100).toFixed(1) + "%" : "—"}</strong><small>{stats ? t("{{count}} 次探测", { count: stats.samples }) : t("暂无记录")}</small></div>;
      },
    }),
    col.display({
      id: "actions", size: 80, header: t("检测"),
      cell: (info) => {
        const node = info.row.original;
        return <div className="nodes-row-actions" onClick={(event) => event.stopPropagation()}>
          <Button size="sm" variant="ghost" title={t("触发出口探测")} aria-label={t("触发出口探测")} onClick={() => void runProbeEgress(node.node_hash)} disabled={isProbePending(node.node_hash, "egress")}><Globe size={14} className={isProbePending(node.node_hash, "egress") ? "spin" : undefined} /></Button>
          <Button size="sm" variant="ghost" title={t("触发延迟探测")} aria-label={t("触发延迟探测")} onClick={() => void runProbeLatency(node.node_hash)} disabled={isProbePending(node.node_hash, "latency")}><Zap size={14} className={isProbePending(node.node_hash, "latency") ? "spin" : undefined} /></Button>
        </div>;
      },
    }),
  ], [t, nodes, checkedHashes, changeSort, sortBy, sortOrder, runProbeEgress, runProbeLatency, isProbePending]);

  return (
    <section className="nodes-page">
      <header className="module-header node-pool-header">
        <div>
          <h2>{t("节点池")}</h2>
          <p className="module-description">{t("管理入口与出口，筛选、检测并导出所需节点。")}</p>
        </div>
        <div className="node-pool-header-actions">
          <Button variant="secondary" size="sm" onClick={refreshNodes} disabled={nodesQuery.isFetching}><RefreshCw size={15} className={nodesQuery.isFetching ? "spin" : undefined} />{t("刷新")}</Button>
          <Button variant={toolsOpen ? "primary" : "secondary"} size="sm" aria-expanded={toolsOpen} aria-controls="node-pool-tools" onClick={() => setToolsOpen(!toolsOpen)}><Zap size={15} />{t("批量检测与导出")}<ChevronDown size={14} /></Button>
        </div>
      </header>
      <ToastContainer toasts={toasts} onDismiss={dismissToast} />

      <Card className="node-pool-card">
        <div className="node-pool-overview">
          <div className="node-pool-tabs" role="tablist" aria-label={t("节点池视图")}>
            <button type="button" role="tab" aria-selected={view === "nodes"} onClick={() => setView("nodes")}>{t("节点视图")}</button>
            <button type="button" role="tab" aria-selected={view === "egresses"} onClick={() => setView("egresses")}>{t("出口视图")}</button>
          </div>
          {view === "nodes" ? <div className="node-pool-totals" aria-live="polite">
            <span>{t("节点")} <strong>{nodesPage.total.toLocaleString()}</strong></span>
            <span>{t("出口 IP")} <strong>{nodesPage.unique_egress_ips.toLocaleString()}</strong></span>
            <span>{t("健康 IP")} <strong className="node-pool-healthy">{nodesPage.unique_healthy_egress_ips.toLocaleString()}</strong></span>
          </div> : <span className="node-pool-hint">{t("按出口聚合，查看关联入口与检测属性。")}</span>}
        </div>
        <div className="node-pool-filterbar">
          <div className="node-pool-search"><Search size={16} /><Input aria-label={t("搜索节点名称")} value={draftFilters.tag_keyword} onChange={(event) => handleFilterChange("tag_keyword", event.target.value)} placeholder={t("搜索节点名称...")} /></div>
          <Input className="node-pool-country" aria-label={t("国家筛选")} value={draftFilters.region} onChange={(event) => handleFilterChange("region", event.target.value)} list="node-countries" placeholder={t("国家：US,JP,HK")} />
          <datalist id="node-countries">{allRegions.map((r) => <option key={r.code} value={r.code}>{r.name}</option>)}</datalist>
          <Select aria-label={t("检测有效性")} value={draftFilters.validity} onChange={(event) => handleFilterChange("validity", event.target.value)}>
            <option value="">{t("全部有效性")}</option><option value="valid">{t("有效")}</option><option value="invalid">{t("无效")}</option><option value="expired">{t("已过期")}</option><option value="untested">{t("未测试")}</option>
          </Select>
          <Button variant={advancedOpen ? "primary" : "secondary"} size="sm" aria-expanded={advancedOpen} aria-controls="node-pool-advanced" onClick={() => setAdvancedOpen(!advancedOpen)}><SlidersHorizontal size={15} />{t("高级筛选")}{advancedCount ? <span className="node-pool-filter-count">{advancedCount}</span> : null}</Button>
          {hasFilters ? <Button size="sm" variant="ghost" onClick={resetFilters} title={t("清空筛选")}><Eraser size={15} />{t("重置")}</Button> : null}
        </div>
        {advancedOpen ? <div className="node-pool-advanced" id="node-pool-advanced">
          <label className="field-group"><span className="field-label">{t("来自此订阅")}</span><Select value={draftFilters.subscription_id} onChange={(event) => handleFilterChange("subscription_id", event.target.value)}><option value="">{t("全部订阅")}</option>{subscriptions.map((s) => <option key={s.id} value={s.id}>{s.name}</option>)}</Select></label>
          <label className="field-group"><span className="field-label">{t("被此平台路由")}</span><Select value={draftFilters.platform_id} onChange={(event) => handleFilterChange("platform_id", event.target.value)}><option value="">{t("全部平台")}</option>{platforms.map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}</Select></label>
          {([
            ["entry_keyword", "入口域名/IP", "host / IP / CIDR"],
            ["egress_ip", "出口 IP", "IP / CIDR"],
            ["network_types", "网络类型", "education,isp,hosting"],
            ["company_keyword", "公司/云商", "Amazon, Google..."],
            ["asn_keyword", "ASN", "AS12345"],
            ["exclude_regions", "排除国家", "CN,RU"],
          ] as const).map(([key, label, placeholder]) => <label className="field-group" key={key}><span className="field-label">{t(label)}</span><Input value={draftFilters[key]} placeholder={placeholder} onChange={(event) => handleFilterChange(key, event.target.value)} /></label>)}
          <label className="field-group"><span className="field-label">{t("运行状态")}</span><Select value={draftFilters.status} onChange={(event) => handleFilterChange("status", event.target.value)}><option value="all">{t("全部")}</option><option value="healthy">{t("健康")}</option><option value="circuit_open">{t("熔断 / 待测")}</option><option value="error">{t("错误")}</option><option value="disabled">{t("禁用")}</option></Select></label>
          <label className="field-group"><span className="field-label">Hosting</span><Select value={draftFilters.hosting} onChange={(event) => handleFilterChange("hosting", event.target.value)}><option value="">{t("全部")}</option><option value="true">{t("是")}</option><option value="false">{t("否")}</option><option value="unknown">{t("未知")}</option></Select></label>
        </div> : null}
      </Card>

      <NodePoolActions open={toolsOpen} filters={activeFilters} selected={[...checkedHashes]} clearSelection={() => setCheckedHashes(new Set())} />

      {view === "egresses" ? <EgressDirectory key={JSON.stringify(activeFilters)} filters={activeFilters} onSelect={(ip) => { handleFilterChange("egress_ip", ip); setView("nodes"); }} /> : <Card className="node-pool-card node-pool-results">
        <div className="node-pool-results-toolbar">
          <div className="node-pool-result-hint">
            {checkedHashes.size ? <><strong>{t("已选 {{count}} 个节点", { count: checkedHashes.size })}</strong><button type="button" className="node-pool-text-button" onClick={() => setCheckedHashes(new Set())}>{t("清空选择")}</button></> : <span>{t("点击节点查看详情，勾选后可批量检测或导出。")}</span>}
            {nodesQuery.isFetching ? <span role="status" className="node-pool-updating"><RefreshCw size={12} className="spin" />{t("更新中...")}</span> : null}
          </div>
          <div className="node-pool-sort">
            <label htmlFor="node-sort-by">{t("排序")}</label>
            <Select id="node-sort-by" value={sortBy} onChange={(event) => { setSortBy(event.target.value as NodeSortBy); setPage(0); }}><option value="tag">{t("节点名称")}</option><option value="region">{t("国家")}</option><option value="failure_count">{t("连续失败")}</option><option value="created_at">{t("创建时间")}</option></Select>
            <Button variant="ghost" size="sm" onClick={() => { setSortOrder((value) => value === "asc" ? "desc" : "asc"); setPage(0); }} aria-label={sortOrder === "asc" ? t("改为降序") : t("改为升序")} title={sortOrder === "asc" ? t("升序") : t("降序")}><ArrowDownUp size={14} /></Button>
          </div>
        </div>
        {nodesQuery.isLoading ? <div className="node-pool-loading" role="status"><RefreshCw size={20} className="spin" /><p>{t("正在加载节点数据...")}</p></div> : null}
        {nodesQuery.isError ? <div className="callout callout-error"><AlertTriangle size={14} /><span>{formatApiErrorMessage(nodesQuery.error, t)}</span></div> : null}
        {!nodesQuery.isLoading && !nodesQuery.isError && !nodes.length ? <div className="empty-box"><Sparkles size={16} /><p>{t("没有匹配的节点")}</p>{hasFilters ? <Button size="sm" variant="secondary" onClick={resetFilters}>{t("清空筛选")}</Button> : null}</div> : null}
        {nodes.length ? <DataTable data={nodes} columns={nodeColumns} onRowClick={(node) => openDrawer(node.node_hash)} getRowId={(node) => node.node_hash} className="node-pool-table" wrapClassName="node-pool-table-wrap" useColumnWidths /> : null}
        <OffsetPagination page={page} totalPages={totalPages} totalItems={nodesPage.total} pageSize={pageSize} pageSizeOptions={PAGE_SIZE_OPTIONS} disabled={nodesQuery.isPlaceholderData} onPageChange={setPage} onPageSizeChange={changePageSize} />
      </Card>}

      {drawerVisible && detailNode ? (
        <div
          className="drawer-overlay"
          role="dialog"
          aria-modal="true"
          aria-label={t("节点详情 {{name}}", { name: firstTag(detailNode) })}
          onClick={() => setDrawerOpen(false)}
        >
          <Card className="drawer-panel" onClick={(event) => event.stopPropagation()}>
            <div className="drawer-header">
              <div>
                <h3>{firstTag(detailNode)}</h3>
                <p>{detailNode.node_hash}</p>
              </div>
              <div className="drawer-header-actions">
                <Button
                  variant="ghost"
                  size="sm"
                  aria-label={t("关闭详情面板")}
                  onClick={() => setDrawerOpen(false)}
                >
                  <X size={16} />
                </Button>
              </div>
            </div>

            <div className="platform-drawer-layout">
              <section className="platform-drawer-section">
                <div className="platform-drawer-section-head">
                  <h4>{t("节点状态")}</h4>
                  <p>{t("节点的网络出口、探测状态以及失败历史。")}</p>
                </div>

                <div className="stats-grid">
                  <div>
                    <span>{t("创建时间")}</span>
                    <p>{formatDateTime(detailNode.created_at)}</p>
                  </div>
                  <div>
                    <span>{t("连续失败")}</span>
                    <p>{!detailNode.has_outbound ? "-" : detailNode.failure_count}</p>
                  </div>
                  <div>
                    <span>{t("状态")}</span>
                    <div>
                      {(() => {
                        const status = getNodeDisplayStatus(detailNode);
                        return (
                          <div style={{ display: "flex", alignItems: "baseline", gap: "4px", flexWrap: "wrap" }}>
                            {status === "error" ? (
                              <Badge variant="danger">{t("错误")}</Badge>
                            ) : status === "disabled" ? (
                              <Badge variant="neutral">{t("禁用")}</Badge>
                            ) : status === "pending_test" ? (
                              <Badge variant="muted">{t("待测")}</Badge>
                            ) : status === "circuit_open" ? (
                              <Badge variant="warning">{t("熔断")}</Badge>
                            ) : (
                              <Badge variant="success">{t("健康")}</Badge>
                            )}
                            {(status === "circuit_open" || status === "pending_test") && detailNode.circuit_open_since ? (
                              <span
                                style={{
                                  fontSize: "11px",
                                  color: "var(--text-muted)",
                                  fontWeight: "normal",
                                }}
                              >
                                ({formatRelativeTime(detailNode.circuit_open_since)})
                              </span>
                            ) : null}
                          </div>
                        );
                      })()}
                    </div>
                  </div>
                  <div>
                    <span>{t("出口 / 区域")}</span>
                    <p>
                      {detailNode.egress_ip || "-"} / {regionToFlag(detailNode.region)}
                    </p>
                  </div>
                  <div>
                    <span>{t("参考延迟")}</span>
                    {(() => {
                      const latencyMs = displayableReferenceLatencyMs(detailNode);
                      if (latencyMs === null) {
                        return <p>-</p>;
                      }
                      return <p style={{ color: referenceLatencyColor(latencyMs) }}>{formatLatency(latencyMs)}</p>;
                    })()}
                  </div>
                  <div>
                    <span>{t("上次探测")}</span>
                    <p>{formatDateTime(detailNode.last_latency_probe_attempt || "")}</p>
                  </div>
                </div>

                {detailNode.last_error ? (
                  <div className="callout callout-error">{t("最近错误：{{message}}", { message: detailNode.last_error })}</div>
                ) : null}
              </section>

              <NodeDiagnosticsPanel key={detailNode.node_hash} hash={detailNode.node_hash} ready={detailNode.has_outbound} />
              <NodeAttributesPanel node={detailNode} />

              <section className="platform-drawer-section">
                <div className="platform-drawer-section-head">
                  <h4>{t("节点别名")}</h4>
                </div>
                {!detailNode.tags.length ? (
                  <p className="muted">{t("无节点名信息")}</p>
                ) : (
                  <div className="tag-list">
                    {detailNode.tags.map((tag) => (
                      <div key={`${tag.subscription_id}:${tag.tag}`} className="tag-item">
                        <p>{tag.tag}</p>
                        <span>{tag.subscription_name}</span>
                        <code>{tag.subscription_id}</code>
                      </div>
                    ))}
                  </div>
                )}
              </section>

              <section className="platform-drawer-section platform-ops-section">
                <div className="platform-drawer-section-head">
                  <h4>{t("运维操作")}</h4>
                </div>
                <div className="platform-ops-list">
                  <div className="platform-op-item">
                    <div className="platform-op-copy">
                      <h5>{t("出口探测")}</h5>
                      <p className="platform-op-hint">{t("检查节点当前出口 IP。")}</p>
                    </div>
                    <Button
                      variant="secondary"
                      onClick={() => void runProbeEgress(detailNode.node_hash)}
                      disabled={isProbePending(detailNode.node_hash, "egress")}
                    >
                      {isProbePending(detailNode.node_hash, "egress") ? t("探测中...") : t("触发出口探测")}
                    </Button>
                  </div>
                  <div className="platform-op-item">
                    <div className="platform-op-copy">
                      <h5>{t("延迟探测")}</h5>
                      <p className="platform-op-hint">{t("检测节点网络延迟。")}</p>
                    </div>
                    <Button
                      variant="secondary"
                      onClick={() => void runProbeLatency(detailNode.node_hash)}
                      disabled={isProbePending(detailNode.node_hash, "latency")}
                    >
                      {isProbePending(detailNode.node_hash, "latency") ? t("探测中...") : t("触发延迟探测")}
                    </Button>
                  </div>
                </div>
              </section>
            </div>
          </Card>
        </div>
      ) : null}
    </section>
  );
}
