package service

import (
	"strings"

	"github.com/Resinat/Resin/internal/node"
)

type CheckTargetRequest struct {
	URL                 string `json:"url"`
	ExpectedStatusCodes []int  `json:"expected_status_codes"`
}

func (s *ControlPlaneService) GetNodeDiagnostics(hash string) (*node.ProbeDiagnostics, error) {
	h, err := node.ParseHex(hash)
	if err != nil {
		return nil, invalidArg("node_hash: invalid format")
	}
	entry, ok := s.Pool.GetEntry(h)
	if !ok {
		return nil, notFound("node not found")
	}
	result := entry.Diagnostics()
	return &result, nil
}

func (s *ControlPlaneService) CheckNodeTarget(hash string, request CheckTargetRequest) (*node.ProbeObservation, error) {
	h, err := node.ParseHex(hash)
	if err != nil {
		return nil, invalidArg("node_hash: invalid format")
	}
	request.URL = strings.TrimSpace(request.URL)
	if _, verr := parseHTTPAbsoluteURL("url", request.URL); verr != nil {
		return nil, verr
	}
	for _, status := range request.ExpectedStatusCodes {
		if status < 100 || status > 599 {
			return nil, invalidArg("expected_status_codes: must be HTTP status codes between 100 and 599")
		}
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
	result, err := s.ProbeMgr.ProbeTargetSync(h, request.URL, request.ExpectedStatusCodes)
	if err != nil {
		return nil, internal("target check failed", err)
	}
	return result, nil
}
