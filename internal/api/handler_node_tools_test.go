package api

import (
	"net/http"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/Resinat/Resin/internal/config"
	"github.com/Resinat/Resin/internal/node"
	"github.com/Resinat/Resin/internal/probe"
	"github.com/Resinat/Resin/internal/subscription"
)

func TestNodeInspection_FilterEgressExportAndBatch(t *testing.T) {
	srv, cp, runtimeCfg := newControlPlaneTestServer(t)
	sub := subscription.NewSubscription("11111111-1111-1111-1111-111111111111", "source", "https://example.com", true, false)
	cp.SubMgr.Register(sub)
	cp.ProbeMgr = probe.NewProbeManager(probe.ProbeConfig{Pool: cp.Pool, Concurrency: 2, RuntimeConfig: func() *config.RuntimeConfig { return runtimeCfg.Load() }, Fetcher: func(_ node.Hash, url string) ([]byte, time.Duration, error) {
		if strings.Contains(url, "myinfo") {
			return []byte(`{"success":true,"ip":"203.0.113.10","countryCode":"US","company":{"name":"University Cloud","type":"isp"},"asn":{"asn":"AS64500","type":"education"},"privacy":{"isHosting":false}}`), time.Millisecond, nil
		}
		return nil, time.Millisecond, nil
	}})
	defer cp.ProbeMgr.Stop()
	hashes := []string{}
	for _, raw := range []string{
		`{"type":"http","server":"entry.example","server_port":8080,"username":"alice","password":"a@b:c"}`,
		`{"type":"http","server":"entry.example","server_port":8080,"username":"bob","password":"password"}`,
	} {
		addNodeForNodeListTest(t, cp, sub, raw, "")
		markNodeHealthyForNodeListTest(t, cp, raw)
		hash := node.HashFromRawOptions([]byte(raw)).Hex()
		hashes = append(hashes, hash)
		for _, action := range []string{"probe-latency", "probe-attributes"} {
			response := doJSONRequest(t, srv, http.MethodPost, "/api/v1/nodes/"+hash+"/actions/"+action, nil, true)
			if response.Code != http.StatusOK {
				t.Fatalf("%s: %d: %s", action, response.Code, response.Body.String())
			}
		}
	}
	// Prefer the selected attribute source consistently if another IP source
	// reports a different country for the same IP.
	firstHash, _ := node.ParseHex(hashes[0])
	sameIP := netip.MustParseAddr("203.0.113.10")
	otherRegion := "jp"
	cp.Pool.UpdateNodeEgressIP(firstHash, &sameIP, &otherRegion)
	response := doJSONRequest(t, srv, http.MethodGet, "/api/v1/nodes?entry_keyword=entry.example&egress_ip=203.0.113.10&network_types=education&hosting=false&region=us&validity=valid", nil, true)
	if response.Code != http.StatusOK {
		t.Fatal(response.Body.String())
	}
	body := decodeJSONMap(t, response)
	if body["total"] != float64(2) {
		t.Fatalf("combined filters failed: %s", response.Body.String())
	}
	first := body["items"].([]any)[0].(map[string]any)
	if first["entry"].(map[string]any)["host"] != "entry.example" || first["egress_ip"] != "203.0.113.10" {
		t.Fatal("entry and exit were mixed")
	}
	response = doJSONRequest(t, srv, http.MethodGet, "/api/v1/nodes/egresses", nil, true)
	body = decodeJSONMap(t, response)
	if body["total"] != float64(1) || body["items"].([]any)[0].(map[string]any)["nodes"] != float64(2) {
		t.Fatalf("shared exit lost entry identities: %s", response.Body.String())
	}
	for _, format := range []string{"txt", "base64", "csv", "json", "sing-box", "clash"} {
		t.Run(format, func(t *testing.T) {
			response := doJSONRequest(t, srv, http.MethodPost, "/api/v1/nodes/export", map[string]any{"format": format, "filters": map[string]any{"platform_id": "", "subscription_id": "", "company_keyword": "", "region": "", "network_types": []string{"education"}, "validity": "valid"}, "dedupe_egress": true}, true)
			if response.Code != http.StatusOK {
				t.Fatalf("export: %d: %s", response.Code, response.Body.String())
			}
			body := decodeJSONMap(t, response)
			if body["exported"] != float64(1) || len(body["skipped"].([]any)) != 0 {
				t.Fatalf("incorrect export counts: %s", response.Body.String())
			}
			content := body["content"].(string)
			if format != "base64" && (!strings.Contains(content, "[US][isp][University Cloud]") || !strings.Contains(content, "entry.example")) {
				t.Fatalf("export name or entry endpoint lost: %s", content)
			}
		})
	}
	hash, _ := node.ParseHex(hashes[0])
	ip := netip.MustParseAddr("203.0.113.11")
	cp.Pool.UpdateNodeEgressIP(hash, &ip, nil)
	response = doJSONRequest(t, srv, http.MethodGet, "/api/v1/nodes?hosting=false", nil, true)
	if decodeJSONMap(t, response)["total"] != float64(1) {
		t.Fatal("stale attributes were applied to a new exit")
	}
	response = doJSONRequest(t, srv, http.MethodGet, "/api/v1/nodes?hosting=unknown", nil, true)
	if decodeJSONMap(t, response)["total"] != float64(1) {
		t.Fatal("missing attributes were treated as hosting=false")
	}
	response = doJSONRequest(t, srv, http.MethodPost, "/api/v1/nodes/probe-jobs", map[string]any{"kind": "latency", "filters": map[string]any{"node_hashes": append(hashes, hashes[0])}}, true)
	if response.Code != http.StatusAccepted {
		t.Fatalf("start batch: %d: %s", response.Code, response.Body.String())
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		status := cp.ProbeMgr.BatchStatus()
		if !status.Running {
			if status.Total != 2 || status.Completed != 2 || status.Successes != 2 {
				t.Fatalf("batch missed nodes: %+v", status)
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("batch did not finish")
}

func TestUnconfirmedFailureIsFilterableButNotExportedAsValid(t *testing.T) {
	srv, cp, _ := newControlPlaneTestServer(t)
	sub := subscription.NewSubscription("11111111-1111-1111-1111-111111111111", "source", "", true, false)
	cp.SubMgr.Register(sub)
	raw := `{"type":"http","server":"entry.example","server_port":8080}`
	addNodeForNodeListTest(t, cp, sub, raw, "")
	markNodeHealthyForNodeListTest(t, cp, raw)
	entry, _ := cp.Pool.GetEntry(node.HashFromRawOptions([]byte(raw)))
	entry.FailureCount.Store(1)
	entry.RecordProbe(node.ProbeObservation{Kind: "latency", CheckedAt: time.Now(), Success: false})
	r := doJSONRequest(t, srv, http.MethodGet, "/api/v1/nodes?validity=unstable", nil, true)
	if r.Code != http.StatusOK || decodeJSONMap(t, r)["total"] != float64(1) {
		t.Fatalf("pending verification filter failed: %s", r.Body.String())
	}
	for state, want := range map[string]float64{"valid": 0, "unstable": 1} {
		r = doJSONRequest(t, srv, http.MethodPost, "/api/v1/nodes/export", map[string]any{"format": "json", "filters": map[string]any{"validity": state}}, true)
		if r.Code != http.StatusOK || decodeJSONMap(t, r)["exported"] != want {
			t.Fatalf("incorrect %s export: %s", state, r.Body.String())
		}
	}
}
