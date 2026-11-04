import { apiRequest } from "../../lib/api-client";
import type {
  EgressProbeResult,
  LatencyProbeResult,
  NodeListQuery,
  NodeSummary,
  PageResponse,
  NodeDiagnostics,
  ProbeObservation,
  EgressAttributes, EgressSummary, BatchStatus, ExportResult, NodeListFilters,
} from "./types";

const basePath = "/api/v1/nodes";

type ApiNodeSummary = Omit<NodeSummary, "tags"> & {
  tags?: NodeSummary["tags"] | null;
  enabled?: boolean | null;
  display_tag?: string | null;
  last_error?: string | null;
  circuit_open_since?: string | null;
  egress_ip?: string | null;
  reference_latency_ms?: number | null;
  region?: string | null;
  last_egress_update?: string | null;
  last_latency_probe_attempt?: string | null;
  last_authority_latency_probe_attempt?: string | null;
  last_egress_update_attempt?: string | null;
};

function normalizeNode(raw: ApiNodeSummary): NodeSummary {
  const { reference_latency_ms, ...rest } = raw;
  const normalized: NodeSummary = {
    ...rest,
    enabled: raw.enabled !== false,
    display_tag: raw.display_tag || "",
    tags: Array.isArray(raw.tags) ? raw.tags : [],
    last_error: raw.last_error || "",
    circuit_open_since: raw.circuit_open_since || "",
    egress_ip: raw.egress_ip || "",
    region: raw.region || "",
    last_egress_update: raw.last_egress_update || "",
    last_latency_probe_attempt: raw.last_latency_probe_attempt || "",
    last_authority_latency_probe_attempt: raw.last_authority_latency_probe_attempt || "",
    last_egress_update_attempt: raw.last_egress_update_attempt || "",
  };

  // Backend uses `omitempty`; field missing means "no reference latency".
  if (typeof reference_latency_ms === "number") {
    normalized.reference_latency_ms = reference_latency_ms;
  }

  return normalized;
}

function nodeQuery(filters: NodeListQuery): URLSearchParams {
  const query = new URLSearchParams({
    limit: String(filters.limit ?? 50),
    offset: String(filters.offset ?? 0),
    sort_by: filters.sort_by || "tag",
    sort_order: filters.sort_order || "asc",
  });

  const appendIfNotEmpty = (key: string, value?: string) => {
    if (!value) {
      return;
    }
    const trimmed = value.trim();
    if (!trimmed) {
      return;
    }
    query.set(key, trimmed);
  };

  appendIfNotEmpty("platform_id", filters.platform_id);
  appendIfNotEmpty("subscription_id", filters.subscription_id);
  appendIfNotEmpty("tag_keyword", filters.tag_keyword);
  appendIfNotEmpty("region", filters.region?.toLowerCase());
  appendIfNotEmpty("egress_ip", filters.egress_ip);
  appendIfNotEmpty("probed_since", filters.probed_since);
  appendIfNotEmpty("entry_keyword", filters.entry_keyword);
  appendIfNotEmpty("company_keyword", filters.company_keyword);
  appendIfNotEmpty("asn_keyword", filters.asn_keyword);
  appendIfNotEmpty("validity", filters.validity);
  appendIfNotEmpty("network_types", filters.network_types?.join(","));
  appendIfNotEmpty("exclude_regions", filters.exclude_regions?.join(","));
  appendIfNotEmpty("node_hashes", filters.node_hashes?.join(","));
  if (filters.hosting_unknown) query.set("hosting", "unknown");
  else if (filters.hosting !== undefined) query.set("hosting", String(filters.hosting));

  if (filters.circuit_open !== undefined) {
    query.set("circuit_open", String(filters.circuit_open));
  }
  if (filters.has_outbound !== undefined) {
    query.set("has_outbound", String(filters.has_outbound));
  }
  if (filters.enabled !== undefined) {
    query.set("enabled", String(filters.enabled));
  }

  return query;
}

export async function listNodes(filters: NodeListQuery): Promise<PageResponse<NodeSummary>> {
  const data = await apiRequest<PageResponse<ApiNodeSummary>>(`${basePath}?${nodeQuery(filters).toString()}`);
  return {
    ...data,
    items: data.items.map(normalizeNode),
  };
}

export async function getNode(hash: string): Promise<NodeSummary> {
  const data = await apiRequest<ApiNodeSummary>(`${basePath}/${hash}`);
  return normalizeNode(data);
}

export async function probeEgress(hash: string): Promise<EgressProbeResult> {
  return apiRequest<EgressProbeResult>(`${basePath}/${hash}/actions/probe-egress`, {
    method: "POST",
  });
}

export async function probeLatency(hash: string): Promise<LatencyProbeResult> {
  return apiRequest<LatencyProbeResult>(`${basePath}/${hash}/actions/probe-latency`, {
    method: "POST",
  });
}

export function getNodeDiagnostics(hash: string): Promise<NodeDiagnostics> {
  return apiRequest<NodeDiagnostics>(`${basePath}/${hash}/diagnostics`);
}

export function checkNodeTarget(hash: string, url: string, expectedStatusCodes: number[]): Promise<ProbeObservation> {
  return apiRequest<ProbeObservation>(`${basePath}/${hash}/actions/check-target`, {
    method: "POST",
    body: { url, expected_status_codes: expectedStatusCodes },
  });
}


export function probeAttributes(hash: string): Promise<EgressAttributes> {
  return apiRequest(`${basePath}/${hash}/actions/probe-attributes`, { method: "POST" });
}
export function listEgresses(filters: NodeListQuery): Promise<PageResponse<EgressSummary>> {
  return apiRequest(`${basePath}/egresses?${nodeQuery(filters).toString()}`);
}
export function exportNodes(format: string, filters: NodeListFilters, dedupeEgress: boolean): Promise<ExportResult> {
  return apiRequest(`${basePath}/export`, { method: "POST", body: { format, filters, dedupe_egress: dedupeEgress } });
}
export function startBatchProbes(kind: string, filters: NodeListFilters): Promise<BatchStatus> {
  return apiRequest(`${basePath}/probe-jobs`, { method: "POST", body: { kind, filters } });
}
export function getBatchStatus(): Promise<BatchStatus | null> { return apiRequest(`${basePath}/probe-jobs/current`); }
export function cancelBatch(id: string): Promise<BatchStatus> { return apiRequest(`${basePath}/probe-jobs/${id}/cancel`, { method: "POST" }); }
