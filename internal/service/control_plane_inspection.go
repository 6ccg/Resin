package service

import (
	"net/netip"
	"sort"
	"strings"
	"time"

	"github.com/Resinat/Resin/internal/config"
	"github.com/Resinat/Resin/internal/node"
	"github.com/Resinat/Resin/internal/probe"
)

func (s *ControlPlaneService) nodeSettings() *config.RuntimeConfig {
	if s != nil && s.RuntimeCfg != nil {
		if cfg := s.RuntimeCfg.Load(); cfg != nil {
			return cfg
		}
	}
	return config.NewDefaultRuntimeConfig()
}

func (s *ControlPlaneService) currentAttributes(entry *node.NodeEntry) *node.EgressAttributes {
	cfg := s.nodeSettings()
	attributes := entry.CurrentAttributes(time.Duration(cfg.EgressAttributesInterval))
	if attributes == nil || attributes.Provider != cfg.EgressAttributesProvider || attributes.SourceURL != cfg.EgressAttributesURL {
		return nil
	}
	return attributes
}

func (s *ControlPlaneService) nodeRegion(entry *node.NodeEntry) string {
	if attributes := s.currentAttributes(entry); attributes != nil && attributes.CountryCode != "" {
		return strings.ToLower(attributes.CountryCode)
	}
	if s.GeoIP != nil {
		return entry.GetRegion(s.GeoIP.Lookup)
	}
	return entry.GetRegion(nil)
}

func matchesCSV(value, csv string) bool {
	for _, item := range strings.Split(csv, ",") {
		if strings.EqualFold(strings.TrimSpace(value), strings.TrimSpace(item)) {
			return true
		}
	}
	return false
}

func matchesIPSearch(value, search string) bool {
	search = strings.TrimSpace(search)
	if prefix, err := netip.ParsePrefix(search); err == nil {
		ip, err := netip.ParseAddr(value)
		return err == nil && prefix.Contains(ip)
	}
	return strings.Contains(strings.ToLower(value), strings.ToLower(search))
}

func (s *ControlPlaneService) nodeMatchesInspection(entry *node.NodeEntry, filters NodeFilters) bool {
	if len(filters.NodeHashes) > 0 {
		found := false
		for _, hash := range filters.NodeHashes {
			if hash == entry.Hash.Hex() {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	if filters.EntryKeyword != nil {
		endpoint := entry.Endpoint()
		keyword := strings.ToLower(strings.TrimSpace(*filters.EntryKeyword))
		matched := strings.Contains(strings.ToLower(endpoint.Host), keyword)
		for _, ip := range entry.EntryIPs() {
			if matchesIPSearch(ip, keyword) {
				matched = true
			}
		}
		if !matched {
			return false
		}
	}
	if filters.Validity != "" && filters.Validity != "all" && entry.Validity(!s.Pool.IsNodeDisabled(entry.Hash), time.Duration(s.nodeSettings().NodeValidityTTL)) != filters.Validity {
		return false
	}
	region := s.nodeRegion(entry)
	for _, excluded := range filters.ExcludeRegions {
		if strings.EqualFold(region, strings.TrimSpace(excluded)) {
			return false
		}
	}
	attributes := s.currentAttributes(entry)
	if filters.HostingUnknown {
		if attributes != nil && attributes.Hosting != nil {
			return false
		}
	}
	if filters.Hosting != nil && (attributes == nil || attributes.Hosting == nil || *attributes.Hosting != *filters.Hosting) {
		return false
	}
	if len(filters.NetworkTypes) > 0 {
		matched := false
		if attributes != nil {
			for _, kind := range filters.NetworkTypes {
				if attributes.HasNetworkType(kind) {
					matched = true
					break
				}
			}
		}
		if !matched {
			return false
		}
	}
	if filters.CompanyKeyword != nil {
		if attributes == nil || !strings.Contains(strings.ToLower(attributes.Company+" "+attributes.Organization), strings.ToLower(*filters.CompanyKeyword)) {
			return false
		}
	}
	if filters.ASNKeyword != nil {
		if attributes == nil || !strings.Contains(strings.ToLower(attributes.ASN), strings.ToLower(*filters.ASNKeyword)) {
			return false
		}
	}
	return true
}

func (s *ControlPlaneService) ProbeAttributes(hash string) (*node.EgressAttributes, error) {
	h, err := node.ParseHex(hash)
	if err != nil {
		return nil, invalidArg("node_hash: invalid format")
	}
	entry, ok := s.Pool.GetEntry(h)
	if !ok {
		return nil, notFound("node not found")
	}
	if !entry.HasOutbound() {
		return nil, invalidArg("node outbound not ready")
	}
	if s.ProbeMgr == nil {
		return nil, internal("probe manager not configured", nil)
	}
	result, err := s.ProbeMgr.ProbeAttributesSync(h)
	if err != nil {
		return nil, internal("attribute probe failed", err)
	}
	return result, nil
}

type BatchProbeRequest struct {
	Kind    string      `json:"kind"`
	Filters NodeFilters `json:"filters"`
}

func (s *ControlPlaneService) StartBatchProbes(request BatchProbeRequest) (*probe.BatchStatus, error) {
	if s.ProbeMgr == nil {
		return nil, internal("probe manager not configured", nil)
	}
	if request.Kind == "targets" && len(s.nodeSettings().ReachabilityTargets) == 0 {
		return nil, invalidArg("configure reachability_targets first")
	}
	nodes, err := s.ListNodes(request.Filters)
	if err != nil {
		return nil, err
	}
	hashes := make([]node.Hash, 0, len(nodes))
	for _, entry := range nodes {
		hash, _ := node.ParseHex(entry.NodeHash)
		hashes = append(hashes, hash)
	}
	result, err := s.ProbeMgr.StartBatch(request.Kind, hashes)
	if err != nil {
		return nil, invalidArg(err.Error())
	}
	return result, nil
}

type EgressSummary struct {
	IP         string                 `json:"ip"`
	Region     string                 `json:"region"`
	Nodes      int                    `json:"nodes"`
	ValidNodes int                    `json:"valid_nodes"`
	Attributes *node.EgressAttributes `json:"attributes,omitempty"`
}

func (s *ControlPlaneService) ListEgresses(filters NodeFilters) ([]EgressSummary, error) {
	nodes, err := s.ListNodes(filters)
	if err != nil {
		return nil, err
	}
	groups := make(map[string]*EgressSummary)
	for _, entry := range nodes {
		if entry.EgressIP == "" {
			continue
		}
		group := groups[entry.EgressIP]
		if group == nil {
			group = &EgressSummary{IP: entry.EgressIP, Region: entry.Region}
			groups[entry.EgressIP] = group
		}
		group.Nodes++
		if entry.Validity == "valid" {
			group.ValidNodes++
		}
		if entry.AttributesCurrent && (group.Attributes == nil || entry.Attributes.CheckedAt.After(group.Attributes.CheckedAt)) {
			group.Attributes = entry.Attributes
		}
	}
	result := make([]EgressSummary, 0, len(groups))
	for _, group := range groups {
		result = append(result, *group)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].IP < result[j].IP })
	return result, nil
}
