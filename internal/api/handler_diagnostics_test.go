package api

import (
	"net/http"
	"testing"

	"github.com/Resinat/Resin/internal/node"
	"github.com/Resinat/Resin/internal/probe"
	"github.com/Resinat/Resin/internal/subscription"
)

func TestNodeTargetCheck_StatusValidationAndHistory(t *testing.T) {
	srv, cp, _ := newControlPlaneTestServer(t)
	sub := subscription.NewSubscription("11111111-1111-1111-1111-111111111111", "sub", "https://example.com", true, false)
	cp.SubMgr.Register(sub)
	raw := `{"type":"http","server":"127.0.0.1","server_port":8080}`
	addNodeForNodeListTest(t, cp, sub, raw, "")
	markNodeHealthyForNodeListTest(t, cp, raw)
	hash := node.HashFromRawOptions([]byte(raw))
	cp.ProbeMgr = probe.NewProbeManager(probe.ProbeConfig{
		Pool:          cp.Pool,
		TargetFetcher: func(node.Hash, string) (int, error) { return 403, nil },
	})
	path := "/api/v1/nodes/" + hash.Hex() + "/actions/check-target"
	response := doJSONRequest(t, srv, http.MethodPost, path, map[string]any{"url": "https://example.com"}, true)
	if response.Code != http.StatusOK {
		t.Fatalf("check status: %d: %s", response.Code, response.Body.String())
	}
	body := decodeJSONMap(t, response)
	if body["success"] != false || body["http_status"] != float64(403) {
		t.Fatalf("target failure was not reported: %+v", body)
	}
	response = doJSONRequest(t, srv, http.MethodGet, "/api/v1/nodes/"+hash.Hex()+"/diagnostics", nil, true)
	if response.Code != http.StatusOK {
		t.Fatalf("diagnostics status: %d: %s", response.Code, response.Body.String())
	}
	body = decodeJSONMap(t, response)
	if len(body["checks"].([]any)) != 1 || body["stability"].(map[string]any)["samples"] != float64(0) {
		t.Fatalf("target result mixed with general stability: %+v", body)
	}
	for _, request := range []map[string]any{
		{"url": "file:///tmp/test"},
		{"url": "https://example.com", "expected_status_codes": []int{999}},
	} {
		response = doJSONRequest(t, srv, http.MethodPost, path, request, true)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("expected invalid input to fail: %d: %s", response.Code, response.Body.String())
		}
	}
}
