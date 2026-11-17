package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/Resinat/Resin/internal/node"
	"github.com/Resinat/Resin/internal/probe/attributes"
	"github.com/Resinat/Resin/internal/service"
)

func HandleAttributeProviders() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) { WriteJSON(w, http.StatusOK, attributes.List()) }
}

func parseNodeFilters(w http.ResponseWriter, r *http.Request) (service.NodeFilters, bool) {
	filters := service.NodeFilters{}
	q := r.URL.Query()
	var ok bool
	if filters.PlatformID, ok = parseOptionalUUIDQuery(w, r, "platform_id", "platform_id"); !ok {
		return filters, false
	}
	if filters.SubscriptionID, ok = parseOptionalUUIDQuery(w, r, "subscription_id", "subscription_id"); !ok {
		return filters, false
	}
	for key, target := range map[string]**string{"region": &filters.Region, "egress_ip": &filters.EgressIP, "tag_keyword": &filters.TagKeyword, "entry_keyword": &filters.EntryKeyword, "company_keyword": &filters.CompanyKeyword, "asn_keyword": &filters.ASNKeyword} {
		if value := strings.TrimSpace(q.Get(key)); value != "" {
			*target = &value
		}
	}
	for key, target := range map[string]**bool{"circuit_open": &filters.CircuitOpen, "has_outbound": &filters.HasOutbound, "enabled": &filters.Enabled} {
		if *target, ok = parseBoolQueryOrWriteInvalid(w, r, key); !ok {
			return filters, false
		}
	}
	if q.Get("hosting") == "unknown" {
		filters.HostingUnknown = true
	} else if filters.Hosting, ok = parseBoolQueryOrWriteInvalid(w, r, "hosting"); !ok {
		return filters, false
	}
	if value := q.Get("probed_since"); value != "" {
		parsed, err := time.Parse(time.RFC3339Nano, value)
		if err != nil {
			writeInvalidArgument(w, "probed_since: invalid RFC3339 timestamp")
			return filters, false
		}
		filters.ProbedSince = &parsed
	}
	split := func(value string) []string {
		result := []string{}
		for _, item := range strings.Split(value, ",") {
			if trimmed := strings.TrimSpace(item); trimmed != "" {
				result = append(result, trimmed)
			}
		}
		return result
	}
	filters.NetworkTypes = split(q.Get("network_types"))
	filters.ExcludeRegions = split(q.Get("exclude_regions"))
	filters.NodeHashes = split(q.Get("node_hashes"))
	for _, hash := range filters.NodeHashes {
		if _, err := node.ParseHex(hash); err != nil {
			writeInvalidArgument(w, "node_hashes: invalid node hash")
			return filters, false
		}
	}
	filters.Validity = q.Get("validity")
	switch filters.Validity {
	case "", "all", "valid", "invalid", "expired", "untested", "disabled", "error":
	default:
		writeInvalidArgument(w, "validity: unsupported state")
		return filters, false
	}
	return filters, true
}

func HandleNodeAttributes(cp *service.ControlPlaneService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		result, err := cp.ProbeAttributes(PathParam(r, "hash"))
		if err != nil {
			writeServiceError(w, err)
			return
		}
		WriteJSON(w, http.StatusOK, result)
	}
}

func HandleListEgresses(cp *service.ControlPlaneService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		filters, ok := parseNodeFilters(w, r)
		if !ok {
			return
		}
		result, err := cp.ListEgresses(filters)
		if err != nil {
			writeServiceError(w, err)
			return
		}
		pg, ok := parsePaginationOrWriteInvalid(w, r)
		if !ok {
			return
		}
		WriteJSON(w, http.StatusOK, map[string]any{"items": PaginateSlice(result, pg), "total": len(result), "limit": pg.Limit, "offset": pg.Offset})
	}
}

func HandleExportNodes(cp *service.ControlPlaneService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var request service.ExportRequest
		if err := DecodeBody(r, &request); err != nil {
			writeDecodeBodyError(w, err)
			return
		}
		result, err := cp.ExportNodes(request)
		if err != nil {
			writeServiceError(w, err)
			return
		}
		WriteJSON(w, http.StatusOK, result)
	}
}

func HandleStartBatchProbes(cp *service.ControlPlaneService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var request service.BatchProbeRequest
		if err := DecodeBody(r, &request); err != nil {
			writeDecodeBodyError(w, err)
			return
		}
		result, err := cp.StartBatchProbes(request)
		if err != nil {
			writeServiceError(w, err)
			return
		}
		WriteJSON(w, http.StatusAccepted, result)
	}
}

func HandleBatchProbeStatus(cp *service.ControlPlaneService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if cp.ProbeMgr == nil {
			writeInvalidArgument(w, "probe manager not configured")
			return
		}
		WriteJSON(w, http.StatusOK, cp.ProbeMgr.BatchStatus())
	}
}

func HandleCancelBatchProbes(cp *service.ControlPlaneService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if cp.ProbeMgr == nil || !cp.ProbeMgr.CancelBatch(PathParam(r, "id")) {
			WriteError(w, http.StatusNotFound, "NOT_FOUND", "batch probe not found")
			return
		}
		WriteJSON(w, http.StatusOK, cp.ProbeMgr.BatchStatus())
	}
}
