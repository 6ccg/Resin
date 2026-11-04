package config

import "time"

// RuntimeConfig holds all hot-updatable global settings.
// These are persisted in the database and served via GET /system/config.
type RuntimeConfig struct {
	// Request log
	RequestLogEnabled                  bool `json:"request_log_enabled"`
	ReverseProxyLogDetailEnabled       bool `json:"reverse_proxy_log_detail_enabled"`
	ReverseProxyLogReqHeadersMaxBytes  int  `json:"reverse_proxy_log_req_headers_max_bytes"`
	ReverseProxyLogReqBodyMaxBytes     int  `json:"reverse_proxy_log_req_body_max_bytes"`
	ReverseProxyLogRespHeadersMaxBytes int  `json:"reverse_proxy_log_resp_headers_max_bytes"`
	ReverseProxyLogRespBodyMaxBytes    int  `json:"reverse_proxy_log_resp_body_max_bytes"`

	// Health check
	MaxConsecutiveFailures          int      `json:"max_consecutive_failures"`
	MaxLatencyTestInterval          Duration `json:"max_latency_test_interval"`
	MaxAuthorityLatencyTestInterval Duration `json:"max_authority_latency_test_interval"`
	MaxEgressTestInterval           Duration `json:"max_egress_test_interval"`

	// Probe
	LatencyTestURL           string   `json:"latency_test_url"`
	LatencyAuthorities       []string `json:"latency_authorities"`
	ReachabilityTargets      []string `json:"reachability_targets"`
	ProbeEnabled             bool     `json:"probe_enabled"`
	ProbeConcurrency         int      `json:"probe_concurrency"`
	ProbeTimeout             Duration `json:"probe_timeout"`
	KeepaliveEnabled         bool     `json:"keepalive_enabled"`
	KeepaliveInterval        Duration `json:"keepalive_interval"`
	RecoveryProbeInterval    Duration `json:"recovery_probe_interval"`
	RecoveryProbeMaxInterval Duration `json:"recovery_probe_max_interval"`
	RecoverySuccessThreshold int      `json:"recovery_success_threshold"`
	NodeValidityTTL          Duration `json:"node_validity_ttl"`
	EgressAttributesEnabled  bool     `json:"egress_attributes_enabled"`
	EgressAttributesProvider string   `json:"egress_attributes_provider"`
	EgressAttributesURL      string   `json:"egress_attributes_url"`
	EgressAttributesInterval Duration `json:"egress_attributes_interval"`

	// P2C
	P2CLatencyWindow   Duration `json:"p2c_latency_window"`
	LatencyDecayWindow Duration `json:"latency_decay_window"`

	// Persistence
	CacheFlushInterval       Duration `json:"cache_flush_interval"`
	CacheFlushDirtyThreshold int      `json:"cache_flush_dirty_threshold"`
}

// NewDefaultRuntimeConfig returns a RuntimeConfig populated with the default
// values specified in DESIGN.md §运行时全局设置项.
func NewDefaultRuntimeConfig() *RuntimeConfig {
	return &RuntimeConfig{
		RequestLogEnabled:                  true,
		ReverseProxyLogDetailEnabled:       false,
		ReverseProxyLogReqHeadersMaxBytes:  4096,
		ReverseProxyLogReqBodyMaxBytes:     1024,
		ReverseProxyLogRespHeadersMaxBytes: 1024,
		ReverseProxyLogRespBodyMaxBytes:    1024,

		MaxConsecutiveFailures:          3,
		MaxLatencyTestInterval:          Duration(1 * time.Hour),
		MaxAuthorityLatencyTestInterval: Duration(3 * time.Hour),
		MaxEgressTestInterval:           Duration(24 * time.Hour),

		LatencyTestURL:           "https://www.gstatic.com/generate_204",
		LatencyAuthorities:       []string{"gstatic.com", "google.com", "cloudflare.com", "github.com"},
		ReachabilityTargets:      []string{},
		ProbeEnabled:             true,
		ProbeConcurrency:         32,
		ProbeTimeout:             Duration(15 * time.Second),
		KeepaliveEnabled:         false,
		KeepaliveInterval:        Duration(5 * time.Minute),
		RecoveryProbeInterval:    Duration(time.Minute),
		RecoveryProbeMaxInterval: Duration(time.Hour),
		RecoverySuccessThreshold: 1,
		NodeValidityTTL:          Duration(2 * time.Hour),
		EgressAttributesEnabled:  true,
		EgressAttributesProvider: "check-host",
		EgressAttributesURL:      "https://api.check-host.cc/myinfo",
		EgressAttributesInterval: Duration(24 * time.Hour),

		P2CLatencyWindow:   Duration(10 * time.Minute),
		LatencyDecayWindow: Duration(10 * time.Minute),

		CacheFlushInterval:       Duration(5 * time.Minute),
		CacheFlushDirtyThreshold: 1000,
	}
}
