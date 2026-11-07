package node

import (
	"encoding/json"
	"net/netip"
	"strings"
	"time"
)

// EgressAttributes belongs to a measured IP and source, never to the entry IP.
type EgressAttributes struct {
	IP           string    `json:"ip"`
	CountryCode  string    `json:"country_code"`
	Country      string    `json:"country,omitempty"`
	City         string    `json:"city,omitempty"`
	ASN          string    `json:"asn,omitempty"`
	Organization string    `json:"organization,omitempty"`
	Company      string    `json:"company,omitempty"`
	CompanyType  string    `json:"company_type,omitempty"`
	ASNType      string    `json:"asn_type,omitempty"`
	NetworkType  string    `json:"network_type"`
	Hosting      *bool     `json:"hosting"`
	IsProxy      *bool     `json:"is_proxy"`
	IsVPN        *bool     `json:"is_vpn"`
	IsTor        *bool     `json:"is_tor"`
	Provider     string    `json:"provider"`
	SourceURL    string    `json:"source_url"`
	CheckedAt    time.Time `json:"checked_at"`
}

type Inspection struct {
	EntryIPs            []string          `json:"entry_ips"`
	EntryResolvedAt     *time.Time        `json:"entry_resolved_at,omitempty"`
	Attributes          *EgressAttributes `json:"attributes,omitempty"`
	AttributesAttemptAt *time.Time        `json:"attributes_attempt_at,omitempty"`
	AttributesError     string            `json:"attributes_error,omitempty"`
	AttributesFailures  int               `json:"attributes_failures,omitempty"`
}

type EntryEndpoint struct {
	Protocol string `json:"protocol"`
	Host     string `json:"host"`
	Port     uint16 `json:"port"`
}

func (e *NodeEntry) Endpoint() EntryEndpoint {
	e.endpointOnce.Do(func() {
		var options struct {
			Type   string `json:"type"`
			Server string `json:"server"`
			Port   uint16 `json:"server_port"`
			TLS    struct {
				Enabled bool `json:"enabled"`
			} `json:"tls"`
		}
		_ = json.Unmarshal(e.RawOptions, &options)
		protocol := options.Type
		if protocol == "http" && options.TLS.Enabled {
			protocol = "https"
		}
		e.endpoint = EntryEndpoint{Protocol: protocol, Host: options.Server, Port: options.Port}
	})
	return e.endpoint
}

func (e *NodeEntry) Inspection() Inspection {
	e.mu.RLock()
	defer e.mu.RUnlock()
	result := e.inspection
	result.EntryIPs = append([]string{}, result.EntryIPs...)
	if result.Attributes != nil {
		copy := *result.Attributes
		result.Attributes = &copy
	}
	return result
}

func (e *NodeEntry) SetEntryIPs(ips []string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.inspection.EntryIPs = append([]string{}, ips...)
	now := time.Now()
	e.inspection.EntryResolvedAt = &now
}

func (e *NodeEntry) SetAttributes(attributes *EgressAttributes, err error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	now := time.Now()
	e.inspection.AttributesAttemptAt = &now
	e.inspection.AttributesError = ""
	if err != nil {
		e.inspection.AttributesError = err.Error()
		e.inspection.AttributesFailures++
		return
	}
	e.inspection.AttributesFailures = 0
	if attributes != nil {
		copy := *attributes
		e.inspection.Attributes = &copy
	}
}

func (e *NodeEntry) InspectionJSON() string {
	value, _ := json.Marshal(e.Inspection())
	return string(value)
}

func (e *NodeEntry) RestoreInspection(data string) error {
	if data == "" {
		return nil
	}
	var inspection Inspection
	if err := json.Unmarshal([]byte(data), &inspection); err != nil {
		return err
	}
	e.mu.Lock()
	e.inspection = inspection
	e.mu.Unlock()
	return nil
}

// CurrentAttributes rejects data for a previous exit or an expired source sample.
func (e *NodeEntry) CurrentAttributes(ttl time.Duration) *EgressAttributes {
	e.mu.RLock()
	var attributes *EgressAttributes
	if e.inspection.Attributes != nil {
		copy := *e.inspection.Attributes
		attributes = &copy
	}
	e.mu.RUnlock()
	if attributes == nil || attributes.IP != e.GetEgressIP().String() {
		return nil
	}
	if ttl > 0 && time.Since(attributes.CheckedAt) > ttl {
		return nil
	}
	return attributes
}

func (e *NodeEntry) EntryIPs() []string {
	e.mu.RLock()
	ips := append([]string{}, e.inspection.EntryIPs...)
	e.mu.RUnlock()
	if len(ips) == 0 {
		if ip, err := netip.ParseAddr(e.Endpoint().Host); err == nil {
			return []string{ip.String()}
		}
	}
	return ips
}

func (a *EgressAttributes) HasNetworkType(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	for _, kind := range []string{a.CompanyType, a.ASNType, a.NetworkType} {
		for _, part := range strings.Split(strings.ToLower(kind), "/") {
			if strings.TrimSpace(part) == value {
				return true
			}
		}
	}
	return false
}

func (e *NodeEntry) Validity(enabled bool, ttl time.Duration) string {
	if !enabled {
		return "disabled"
	}
	if !e.HasOutbound() {
		return "error"
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	for i := len(e.probeHistory) - 1; i >= 0; i-- {
		sample := e.probeHistory[i]
		if sample.Kind != "latency" && sample.Kind != "egress" {
			continue
		}
		if e.IsCircuitOpen() {
			return "invalid"
		}
		if !sample.Success || e.FailureCount.Load() > 0 {
			return "unstable"
		}
		if time.Since(sample.CheckedAt) > ttl {
			return "expired"
		}
		return "valid"
	}
	if e.FailureCount.Load() > 0 {
		if e.IsCircuitOpen() {
			return "invalid"
		}
		return "unstable"
	}
	return "untested"
}
