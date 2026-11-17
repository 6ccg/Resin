package service

import (
	"bytes"
	"encoding/base64"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Resinat/Resin/internal/node"
	"github.com/Resinat/Resin/internal/nodeexport"
	"gopkg.in/yaml.v3"
)

type ExportRequest struct {
	Format       string      `json:"format"`
	Filters      NodeFilters `json:"filters"`
	DedupeEgress bool        `json:"dedupe_egress"`
}
type ExportSkip struct {
	NodeHash string `json:"node_hash"`
	Reason   string `json:"reason"`
}
type ExportResult struct {
	Filename    string       `json:"filename"`
	ContentType string       `json:"content_type"`
	Content     string       `json:"content"`
	Exported    int          `json:"exported"`
	Skipped     []ExportSkip `json:"skipped"`
}

func (s *ControlPlaneService) ExportNodes(request ExportRequest) (*ExportResult, error) {
	switch request.Format {
	case "txt", "base64", "csv", "json", "sing-box", "clash":
	default:
		return nil, invalidArg("unsupported export format")
	}
	nodes, err := s.ListNodes(request.Filters)
	if err != nil {
		return nil, err
	}
	sort.Slice(nodes, func(i, j int) bool {
		a, b := nodes[i], nodes[j]
		if request.DedupeEgress {
			if (a.Validity == "valid") != (b.Validity == "valid") {
				return a.Validity == "valid"
			}
			if (a.ReferenceLatencyMs != nil) != (b.ReferenceLatencyMs != nil) {
				return a.ReferenceLatencyMs != nil
			}
			if a.ReferenceLatencyMs != nil && b.ReferenceLatencyMs != nil && *a.ReferenceLatencyMs != *b.ReferenceLatencyMs {
				return *a.ReferenceLatencyMs < *b.ReferenceLatencyMs
			}
		}
		return a.NodeHash < b.NodeHash
	})
	result := &ExportResult{ContentType: "text/plain;charset=utf-8", Skipped: []ExportSkip{}}
	seen := map[string]bool{}
	names := map[string]int{}
	lines := []string{}
	configs := []map[string]any{}
	records := []map[string]any{}
	var csvBuffer bytes.Buffer
	writer := csv.NewWriter(&csvBuffer)
	if request.Format == "csv" {
		csvBuffer.WriteString("\ufeff")
		_ = writer.Write([]string{"name", "entry_protocol", "entry_host", "entry_port", "entry_ips", "egress_ip", "country", "network_type", "company", "asn", "hosting", "validity", "attributes_provider", "attributes_checked_at", "outbound_json"})
	}
	for _, summary := range nodes {
		if request.DedupeEgress && summary.EgressIP != "" && seen[summary.EgressIP] {
			continue
		}
		hash, _ := node.ParseHex(summary.NodeHash)
		entry, ok := s.Pool.GetEntry(hash)
		if !ok {
			continue
		}
		attributes := s.currentAttributes(entry)
		name := nodeexport.Name(summary.Region, attributes)
		if request.Format == "sing-box" || request.Format == "clash" {
			names[name]++
			if names[name] > 1 {
				name += fmt.Sprintf(" %d", names[name])
			}
		}
		config, err := nodeexport.Tagged(entry.RawOptions, name)
		if err != nil {
			result.Skipped = append(result.Skipped, ExportSkip{summary.NodeHash, err.Error()})
			continue
		}
		switch request.Format {
		case "txt", "base64":
			line, encodeErr := nodeexport.URI(entry.RawOptions, name)
			err = encodeErr
			if err == nil {
				lines = append(lines, line)
			}
		case "sing-box":
			configs = append(configs, config)
		case "clash":
			var proxy map[string]any
			proxy, err = nodeexport.Clash(entry.RawOptions, name)
			if err == nil {
				configs = append(configs, proxy)
			}
		case "json":
			records = append(records, map[string]any{"name": name, "node": summary, "outbound": config})
		case "csv":
			kind, company, asn, hosting, provider, checked := "unknown", "unknown", "", "unknown", "", ""
			if attributes != nil {
				kind = attributes.NetworkType
				company = attributes.Company
				asn = attributes.ASN
				provider = attributes.Provider
				checked = attributes.CheckedAt.Format(time.RFC3339)
				if attributes.Hosting != nil {
					hosting = fmt.Sprint(*attributes.Hosting)
				}
			}
			encoded, _ := json.Marshal(config)
			err = writer.Write([]string{name, summary.Entry.Protocol, summary.Entry.Host, fmt.Sprint(summary.Entry.Port), strings.Join(summary.EntryIPs, ","), summary.EgressIP, strings.ToUpper(summary.Region), kind, company, asn, hosting, summary.Validity, provider, checked, string(encoded)})
		}
		if err != nil {
			result.Skipped = append(result.Skipped, ExportSkip{summary.NodeHash, err.Error()})
			continue
		}
		if summary.EgressIP != "" {
			seen[summary.EgressIP] = true
		}
		result.Exported++
	}
	extension := "txt"
	switch request.Format {
	case "txt", "base64":
		result.Content = strings.Join(lines, "\r\n")
		if len(lines) > 0 {
			result.Content += "\r\n"
		}
		if request.Format == "base64" {
			result.Content = base64.StdEncoding.EncodeToString([]byte(result.Content))
		} else {
			result.Content = "\ufeff" + result.Content
		}
	case "sing-box":
		data, _ := json.MarshalIndent(map[string]any{"outbounds": configs}, "", "  ")
		result.Content = string(data)
		extension = "json"
		result.ContentType = "application/json"
	case "json":
		data, _ := json.MarshalIndent(records, "", "  ")
		result.Content = string(data)
		extension = "json"
		result.ContentType = "application/json"
	case "clash":
		data, encodeErr := yaml.Marshal(map[string]any{"proxies": configs})
		if encodeErr != nil {
			return nil, internal("encode Clash export", encodeErr)
		}
		result.Content = string(data)
		extension = "yaml"
		result.ContentType = "application/yaml"
	case "csv":
		writer.Flush()
		if err := writer.Error(); err != nil {
			return nil, internal("encode CSV export", err)
		}
		result.Content = csvBuffer.String()
		extension = "csv"
		result.ContentType = "text/csv;charset=utf-8"
	}
	result.Filename = "resin-nodes-" + time.Now().Format("20060102-150405") + "." + extension
	return result, nil
}
