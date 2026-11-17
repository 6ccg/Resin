package service

import (
	"bytes"
	"cmp"
	"net/netip"
	"slices"
	"strings"

	"github.com/Resinat/Resin/internal/node"
)

type NodePageOptions struct {
	SortBy, SortOrder string
	Limit, Offset     int
}

type NodeListPage struct {
	Items                  []NodeSummary `json:"items"`
	Total                  int           `json:"total"`
	Limit                  int           `json:"limit"`
	Offset                 int           `json:"offset"`
	UniqueEgressIPs        int           `json:"unique_egress_ips"`
	UniqueHealthyEgressIPs int           `json:"unique_healthy_egress_ips"`
}

// ListNodesPage sorts small candidate records and builds details for one page.
// Counts still describe all matching nodes, not just the current page.
func (s *ControlPlaneService) ListNodesPage(filters NodeFilters, options NodePageOptions) (*NodeListPage, error) {
	type candidate struct {
		hash    node.Hash
		entry   *node.NodeEntry
		textKey string
		numKey  int64
	}
	candidates := make([]candidate, 0)
	exits := make(map[netip.Addr]struct{})
	healthyExits := make(map[netip.Addr]struct{})
	subLookup := s.Pool.MakeSubLookup()
	err := s.rangeFilteredNodes(filters, func(h node.Hash, entry *node.NodeEntry) {
		item := candidate{hash: h, entry: entry}
		switch options.SortBy {
		case "created_at":
			item.numKey = entry.CreatedAt.UnixNano()
		case "failure_count":
			item.numKey = int64(entry.FailureCount.Load())
		case "region":
			item.textKey = s.nodeRegion(entry)
		default:
			item.textKey = s.Pool.ResolveNodeDisplayTag(h)
			// A pool without a subscription lookup can still expose API tags.
			if item.textKey == "" && s.SubMgr != nil {
				var oldest int64
				for _, id := range entry.SubscriptionIDs() {
					sub := s.SubMgr.Lookup(id)
					if sub == nil {
						continue
					}
					managed, ok := sub.ManagedNodes().LoadNode(h)
					if !ok {
						continue
					}
					for _, tag := range managed.Tags {
						full := sub.Name() + "/" + tag
						if item.textKey == "" || sub.CreatedAtNs < oldest || (sub.CreatedAtNs == oldest && full < item.textKey) {
							oldest, item.textKey = sub.CreatedAtNs, full
						}
					}
				}
			}
		}
		candidates = append(candidates, item)
		if ip := entry.GetEgressIP(); ip.IsValid() {
			exits[ip] = struct{}{}
			if entry.IsHealthy() && !entry.IsDisabledBySubscriptions(subLookup) {
				healthyExits[ip] = struct{}{}
			}
		}
	})
	if err != nil {
		return nil, err
	}
	slices.SortFunc(candidates, func(a, b candidate) int {
		order := strings.Compare(a.textKey, b.textKey)
		if order == 0 {
			order = cmp.Compare(a.numKey, b.numKey)
		}
		if order == 0 {
			order = bytes.Compare(a.hash[:], b.hash[:])
		}
		if options.SortOrder == "desc" {
			return -order
		}
		return order
	})
	start := min(max(options.Offset, 0), len(candidates))
	end := start + min(max(options.Limit, 0), len(candidates)-start)
	result := &NodeListPage{Items: make([]NodeSummary, 0, end-start), Total: len(candidates), Limit: options.Limit, Offset: options.Offset, UniqueEgressIPs: len(exits), UniqueHealthyEgressIPs: len(healthyExits)}
	for _, item := range candidates[start:end] {
		result.Items = append(result.Items, s.nodeEntryToSummary(item.hash, item.entry))
	}
	return result, nil
}
