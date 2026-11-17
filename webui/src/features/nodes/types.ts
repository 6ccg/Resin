export type NodeTag = {
  subscription_id: string;
  subscription_name: string;
  tag: string;
};

export type NodeSummary = {
  node_hash: string;
  created_at: string;
  enabled: boolean;
  display_tag?: string;
  has_outbound: boolean;
  last_error?: string;
  circuit_open_since?: string;
  failure_count: number;
  egress_ip?: string;
  reference_latency_ms?: number;
  region?: string;
  last_egress_update?: string;
  last_latency_probe_attempt?: string;
  last_authority_latency_probe_attempt?: string;
  last_egress_update_attempt?: string;
  tags: NodeTag[];
  probe_stability?: ProbeStability;
  entry: { protocol: string; host: string; port: number };
  entry_ips: string[];
  attributes?: EgressAttributes;
  attributes_current: boolean;
  attributes_error?: string;
  validity: string;
};

export type PageResponse<T> = {
  items: T[];
  total: number;
  limit: number;
  offset: number;
  unique_egress_ips: number;
  unique_healthy_egress_ips: number;
};

export type NodeSortBy = "tag" | "created_at" | "failure_count" | "region";
export type SortOrder = "asc" | "desc";

export type NodeListFilters = {
  platform_id?: string;
  subscription_id?: string;
  tag_keyword?: string;
  region?: string;
  egress_ip?: string;
  probed_since?: string;
  enabled?: boolean;
  circuit_open?: boolean;
  has_outbound?: boolean;
  entry_keyword?: string;
  network_types?: string[];
  exclude_regions?: string[];
  hosting?: boolean;
  hosting_unknown?: boolean;
  company_keyword?: string;
  asn_keyword?: string;
  validity?: string;
  node_hashes?: string[];
};

export type NodeListQuery = NodeListFilters & {
  sort_by?: NodeSortBy;
  sort_order?: SortOrder;
  limit?: number;
  offset?: number;
};

export type EgressProbeResult = {
  egress_ip: string;
  region?: string;
  latency_ewma_ms: number;
};

export type LatencyProbeResult = {
  latency_ewma_ms: number;
  latency_ms: number;
};

export type ProbeStability = {
  samples: number;
  successes: number;
  success_rate: number;
  consecutive_failures: number;
  average_duration_ms: number;
  duration_stddev_ms: number;
  first_checked_at?: string;
  last_checked_at?: string;
};

export type ProbeObservation = {
  kind: "latency" | "egress" | "egress_metadata" | "target";
  target: string;
  checked_at: string;
  success: boolean;
  duration_ms: number;
  latency_ms?: number;
  http_status?: number;
  error?: string;
};

export type NodeDiagnostics = {
  stability: ProbeStability;
  checks: ProbeObservation[];
};

export type EgressAttributes = {
  ip: string; country_code: string; country?: string; city?: string;
  asn?: string; organization?: string; company?: string; company_type?: string; asn_type?: string;
  network_type: string; hosting: boolean | null; is_proxy: boolean | null; is_vpn: boolean | null; is_tor: boolean | null;
  provider: string; source_url: string; checked_at: string;
};

export type EgressSummary = { ip: string; region: string; nodes: number; valid_nodes: number; attributes?: EgressAttributes };
export type BatchStatus = {
  id: string; kind: string; total: number; completed: number; successes: number; failures: number; skipped: number;
  running: boolean; cancelled: boolean; started_at: string; finished_at?: string;
};
export type ExportResult = { filename: string; content_type: string; content: string; exported: number; skipped: { node_hash: string; reason: string }[] };
