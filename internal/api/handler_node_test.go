package api

import (
	"fmt"
	"net/http"
	"net/netip"
	"testing"
	"time"

	"github.com/Resinat/Resin/internal/config"
	"github.com/Resinat/Resin/internal/node"
	"github.com/Resinat/Resin/internal/probe"
	"github.com/Resinat/Resin/internal/service"
	"github.com/Resinat/Resin/internal/subscription"
	"github.com/Resinat/Resin/internal/testutil"
)

func TestHandleListNodes_SortBeforePagination(t *testing.T) {
	srv, cp, _ := newControlPlaneTestServer(t)
	sub := subscription.NewSubscription("11111111-1111-1111-1111-111111111111", "source", "", true, false)
	cp.SubMgr.Register(sub)
	hashes := map[string]string{}
	for i, tag := range []string{"Charlie", "Alpha", "Bravo"} {
		raw := fmt.Sprintf(`{"type":"http","server":"192.0.2.%d","server_port":8080}`, i+1)
		addNodeForNodeListTestWithTag(t, cp, sub, raw, fmt.Sprintf("203.0.113.%d", i+1), tag)
		h := node.HashFromRawOptions([]byte(raw))
		hashes[tag] = h.Hex()
		entry, _ := cp.Pool.GetEntry(h)
		entry.FailureCount.Store(int32(i))
		entry.CreatedAt = time.Unix(int64(i+1), 0)
		entry.SetEgressRegion([]string{"us", "de", "jp"}[i])
	}
	for _, tc := range []struct{ query, want string }{
		{"sort_by=tag&offset=1", "Bravo"},
		{"sort_by=tag&sort_order=desc", "Charlie"},
		{"sort_by=failure_count&sort_order=desc", "Bravo"},
		{"sort_by=created_at&sort_order=desc", "Bravo"},
		{"sort_by=region&offset=1", "Bravo"},
	} {
		t.Run(tc.query, func(t *testing.T) {
			response := doJSONRequest(t, srv, http.MethodGet, "/api/v1/nodes?limit=1&"+tc.query, nil, true)
			if response.Code != http.StatusOK {
				t.Fatal(response.Body.String())
			}
			body := decodeJSONMap(t, response)
			items := body["items"].([]any)
			if body["total"] != float64(3) || body["unique_egress_ips"] != float64(3) || len(items) != 1 || items[0].(map[string]any)["node_hash"] != hashes[tc.want] {
				t.Fatalf("incorrect page: %s", response.Body.String())
			}
		})
	}
	response := doJSONRequest(t, srv, http.MethodGet, "/api/v1/nodes?limit=1&offset=99", nil, true)
	body := decodeJSONMap(t, response)
	if body["total"] != float64(3) || len(body["items"].([]any)) != 0 {
		t.Fatalf("out-of-range page lost total: %s", response.Body.String())
	}
}

func addNodeForNodeListTest(t *testing.T, cp *service.ControlPlaneService, sub *subscription.Subscription, raw string, egressIP string) {
	addNodeForNodeListTestWithTag(t, cp, sub, raw, egressIP, "tag")
}

func addNodeForNodeListTestWithTag(
	t *testing.T,
	cp *service.ControlPlaneService,
	sub *subscription.Subscription,
	raw string,
	egressIP string,
	tag string,
) {
	t.Helper()

	hash := node.HashFromRawOptions([]byte(raw))
	cp.Pool.AddNodeFromSub(hash, []byte(raw), sub.ID)
	sub.ManagedNodes().StoreNode(hash, subscription.ManagedNode{Tags: []string{tag}})

	if egressIP == "" {
		return
	}
	entry, ok := cp.Pool.GetEntry(hash)
	if !ok {
		t.Fatalf("node %s missing after add", hash.Hex())
	}
	entry.SetEgressIP(netip.MustParseAddr(egressIP))
}

func markNodeHealthyForNodeListTest(t *testing.T, cp *service.ControlPlaneService, raw string) {
	t.Helper()

	hash := node.HashFromRawOptions([]byte(raw))
	entry, ok := cp.Pool.GetEntry(hash)
	if !ok {
		t.Fatalf("node %s missing after add", hash.Hex())
	}
	ob := testutil.NewNoopOutbound()
	entry.Outbound.Store(&ob)
	entry.CircuitOpenSince.Store(0)
}

func TestHandleListNodes_TagKeywordFiltersByNodeName(t *testing.T) {
	srv, cp, _ := newControlPlaneTestServer(t)

	subA := subscription.NewSubscription("11111111-1111-1111-1111-111111111111", "sub-a", "https://example.com/a", true, false)
	cp.SubMgr.Register(subA)

	addNodeForNodeListTestWithTag(t, cp, subA, `{"type":"ss","server":"1.1.1.1","port":443}`, "", "hongkong-fast-01")
	addNodeForNodeListTestWithTag(t, cp, subA, `{"type":"ss","server":"2.2.2.2","port":443}`, "", "japan-slow-01")

	rec := doJSONRequest(
		t,
		srv,
		http.MethodGet,
		"/api/v1/nodes?subscription_id="+subA.ID+"&tag_keyword=FAST",
		nil,
		true,
	)
	if rec.Code != http.StatusOK {
		t.Fatalf("list nodes with tag_keyword status: got %d, want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	body := decodeJSONMap(t, rec)
	if body["total"] != float64(1) {
		t.Fatalf("tag_keyword total: got %v, want 1", body["total"])
	}
}

func TestHandleListNodes_UniqueEgressIPsUsesFilteredResult(t *testing.T) {
	srv, cp, _ := newControlPlaneTestServer(t)

	subA := subscription.NewSubscription("11111111-1111-1111-1111-111111111111", "sub-a", "https://example.com/a", true, false)
	subB := subscription.NewSubscription("22222222-2222-2222-2222-222222222222", "sub-b", "https://example.com/b", true, false)
	cp.SubMgr.Register(subA)
	cp.SubMgr.Register(subB)

	const rawA1 = `{"type":"ss","server":"1.1.1.1","port":443}`
	const rawA2 = `{"type":"ss","server":"2.2.2.2","port":443}`
	const rawA3 = `{"type":"ss","server":"3.3.3.3","port":443}`
	const rawA4 = `{"type":"ss","server":"4.4.4.4","port":443}`
	const rawB1 = `{"type":"ss","server":"5.5.5.5","port":443}`

	addNodeForNodeListTest(t, cp, subA, rawA1, "203.0.113.10")
	addNodeForNodeListTest(t, cp, subA, rawA2, "203.0.113.10")
	addNodeForNodeListTest(t, cp, subA, rawA3, "203.0.113.11")
	addNodeForNodeListTest(t, cp, subA, rawA4, "")
	addNodeForNodeListTest(t, cp, subB, rawB1, "203.0.113.99")

	// Healthy condition: has outbound + not circuit-open.
	markNodeHealthyForNodeListTest(t, cp, rawA1)
	markNodeHealthyForNodeListTest(t, cp, rawA2)

	rec := doJSONRequest(t, srv, http.MethodGet, "/api/v1/nodes?subscription_id="+subA.ID, nil, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("list nodes status: got %d, want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	body := decodeJSONMap(t, rec)
	if body["total"] != float64(4) {
		t.Fatalf("total: got %v, want 4", body["total"])
	}
	if body["unique_egress_ips"] != float64(2) {
		t.Fatalf("unique_egress_ips: got %v, want 2", body["unique_egress_ips"])
	}
	if body["unique_healthy_egress_ips"] != float64(1) {
		t.Fatalf("unique_healthy_egress_ips: got %v, want 1", body["unique_healthy_egress_ips"])
	}

	rec = doJSONRequest(
		t,
		srv,
		http.MethodGet,
		"/api/v1/nodes?subscription_id="+subA.ID+"&limit=1",
		nil,
		true,
	)
	if rec.Code != http.StatusOK {
		t.Fatalf("list nodes paged status: got %d, want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	body = decodeJSONMap(t, rec)
	if body["total"] != float64(4) {
		t.Fatalf("paged total: got %v, want 4", body["total"])
	}
	if body["unique_egress_ips"] != float64(2) {
		t.Fatalf("paged unique_egress_ips: got %v, want 2", body["unique_egress_ips"])
	}
	if body["unique_healthy_egress_ips"] != float64(1) {
		t.Fatalf("paged unique_healthy_egress_ips: got %v, want 1", body["unique_healthy_egress_ips"])
	}

	rec = doJSONRequest(
		t,
		srv,
		http.MethodGet,
		"/api/v1/nodes?subscription_id="+subA.ID+"&egress_ip=203.0.113.10",
		nil,
		true,
	)
	if rec.Code != http.StatusOK {
		t.Fatalf("list nodes with egress filter status: got %d, want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	body = decodeJSONMap(t, rec)
	if body["total"] != float64(2) {
		t.Fatalf("filtered total: got %v, want 2", body["total"])
	}
	if body["unique_egress_ips"] != float64(1) {
		t.Fatalf("filtered unique_egress_ips: got %v, want 1", body["unique_egress_ips"])
	}
	if body["unique_healthy_egress_ips"] != float64(1) {
		t.Fatalf("filtered unique_healthy_egress_ips: got %v, want 1", body["unique_healthy_egress_ips"])
	}
}

func TestHandleListNodes_IncludesReferenceLatencyMs(t *testing.T) {
	srv, cp, runtimeCfg := newControlPlaneTestServer(t)

	cfg := config.NewDefaultRuntimeConfig()
	cfg.LatencyAuthorities = []string{"cloudflare.com", "github.com", "google.com"}
	runtimeCfg.Store(cfg)

	subA := subscription.NewSubscription("11111111-1111-1111-1111-111111111111", "sub-a", "https://example.com/a", true, false)
	cp.SubMgr.Register(subA)

	raw := `{"type":"ss","server":"1.1.1.1","port":443}`
	hash := node.HashFromRawOptions([]byte(raw))
	addNodeForNodeListTest(t, cp, subA, raw, "203.0.113.10")

	entry, ok := cp.Pool.GetEntry(hash)
	if !ok {
		t.Fatalf("node %s missing after add", hash.Hex())
	}
	entry.LatencyTable.LoadEntry("cloudflare.com", node.DomainLatencyStats{
		Ewma:        40 * time.Millisecond,
		LastUpdated: time.Now(),
	})
	entry.LatencyTable.LoadEntry("github.com", node.DomainLatencyStats{
		Ewma:        80 * time.Millisecond,
		LastUpdated: time.Now(),
	})
	entry.LatencyTable.LoadEntry("example.com", node.DomainLatencyStats{
		Ewma:        10 * time.Millisecond,
		LastUpdated: time.Now(),
	})

	rec := doJSONRequest(t, srv, http.MethodGet, "/api/v1/nodes?subscription_id="+subA.ID, nil, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("list nodes status: got %d, want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	body := decodeJSONMap(t, rec)
	items, ok := body["items"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("items mismatch: got %T len=%d", body["items"], len(items))
	}
	item, ok := items[0].(map[string]any)
	if !ok {
		t.Fatalf("item type: got %T", items[0])
	}
	if item["reference_latency_ms"] != float64(60) {
		t.Fatalf("reference_latency_ms: got %v, want 60", item["reference_latency_ms"])
	}
}

func TestHandleProbeEgress_ReturnsRegion(t *testing.T) {
	srv, cp, _ := newControlPlaneTestServer(t)

	sub := subscription.NewSubscription("11111111-1111-1111-1111-111111111111", "sub-a", "https://example.com/a", true, false)
	cp.SubMgr.Register(sub)

	raw := []byte(`{"type":"ss","server":"1.1.1.1","port":443}`)
	hash := node.HashFromRawOptions(raw)
	cp.Pool.AddNodeFromSub(hash, raw, sub.ID)
	sub.ManagedNodes().StoreNode(hash, subscription.ManagedNode{Tags: []string{"tag"}})

	entry, ok := cp.Pool.GetEntry(hash)
	if !ok {
		t.Fatalf("node %s missing after add", hash.Hex())
	}
	ob := testutil.NewNoopOutbound()
	entry.Outbound.Store(&ob)

	cp.ProbeMgr = probe.NewProbeManager(probe.ProbeConfig{
		Pool: cp.Pool,
		Fetcher: func(_ node.Hash, _ string) ([]byte, time.Duration, error) {
			return []byte("ip=203.0.113.88\nloc=JP"), 25 * time.Millisecond, nil
		},
	})

	rec := doJSONRequest(t, srv, http.MethodPost, "/api/v1/nodes/"+hash.Hex()+"/actions/probe-egress", nil, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("probe-egress status: got %d, want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	body := decodeJSONMap(t, rec)
	if body["egress_ip"] != "203.0.113.88" {
		t.Fatalf("egress_ip: got %v, want %q", body["egress_ip"], "203.0.113.88")
	}
	if body["region"] != "jp" {
		t.Fatalf("region: got %v, want %q", body["region"], "jp")
	}
}

func TestHandleListNodes_EnabledFilter(t *testing.T) {
	srv, cp, _ := newControlPlaneTestServer(t)

	subEnabled := subscription.NewSubscription("11111111-1111-1111-1111-111111111111", "sub-enabled", "https://example.com/a", true, false)
	subDisabled := subscription.NewSubscription("22222222-2222-2222-2222-222222222222", "sub-disabled", "https://example.com/b", false, false)
	cp.SubMgr.Register(subEnabled)
	cp.SubMgr.Register(subDisabled)

	addNodeForNodeListTestWithTag(t, cp, subEnabled, `{"type":"ss","server":"1.1.1.1","port":443}`, "", "enabled-tag")
	addNodeForNodeListTestWithTag(t, cp, subDisabled, `{"type":"ss","server":"2.2.2.2","port":443}`, "", "disabled-tag")

	rec := doJSONRequest(t, srv, http.MethodGet, "/api/v1/nodes?enabled=true", nil, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("enabled=true status: got %d, want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	body := decodeJSONMap(t, rec)
	if body["total"] != float64(1) {
		t.Fatalf("enabled=true total: got %v, want 1", body["total"])
	}

	rec = doJSONRequest(t, srv, http.MethodGet, "/api/v1/nodes?enabled=false", nil, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("enabled=false status: got %d, want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	body = decodeJSONMap(t, rec)
	if body["total"] != float64(1) {
		t.Fatalf("enabled=false total: got %v, want 1", body["total"])
	}
}
