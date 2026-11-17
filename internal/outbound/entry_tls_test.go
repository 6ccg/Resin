package outbound

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Resinat/Resin/internal/config"
	"github.com/Resinat/Resin/internal/netutil"
	M "github.com/sagernet/sing/common/metadata"
)

func TestProxyEntryTLSSettingAppliesToNewDials(t *testing.T) {
	target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer target.Close()

	var tunnels atomic.Int32
	proxy := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodConnect || r.Host != target.Listener.Addr().String() {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		remote, err := net.DialTimeout("tcp", r.Host, time.Second)
		if err != nil {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		defer remote.Close()
		conn, rw, err := w.(http.Hijacker).Hijack()
		if err != nil {
			return
		}
		defer conn.Close()
		tunnels.Add(1)
		if _, err := rw.WriteString("HTTP/1.1 200 Connection established\r\n\r\n"); err != nil {
			return
		}
		if err := rw.Flush(); err != nil {
			return
		}
		go func() {
			_, _ = io.Copy(remote, rw)
			_ = remote.Close()
		}()
		_, _ = io.Copy(conn, remote)
	}))
	defer proxy.Close()

	var allow atomic.Bool
	builder, err := NewSingboxBuilderWithConfig(SingboxBuilderConfig{
		DNSUpstreams:          config.DefaultNodeDNSUpstreams(),
		AllowInsecureProxyTLS: allow.Load,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer builder.Close()

	host, portText, err := net.SplitHostPort(proxy.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	entryTLS := map[string]any{"enabled": true}
	options := map[string]any{"type": "http", "server": host, "server_port": port, "tls": entryTLS}
	raw, err := json.Marshal(options)
	if err != nil {
		t.Fatal(err)
	}
	ob, err := builder.Build(raw)
	if err != nil {
		t.Fatal(err)
	}
	defer closeOutbound(ob)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	destination := M.ParseSocksaddr(target.Listener.Addr().String())
	strictDial := func() {
		t.Helper()
		conn, err := ob.DialContext(ctx, "tcp", destination)
		if conn != nil {
			_ = conn.Close()
		}
		if err == nil || !strings.Contains(err.Error(), "x509") {
			t.Fatalf("strict entry verification error = %v", err)
		}
	}
	strictDial()

	allow.Store(true)
	conn, err := ob.DialContext(ctx, "tcp", destination)
	if err != nil {
		t.Fatalf("dial with global override: %v", err)
	}
	_ = conn.Close()
	_, _, err = netutil.HTTPGetViaOutbound(ctx, ob, target.URL, netutil.OutboundHTTPOptions{})
	if err == nil || !strings.Contains(err.Error(), "x509") || tunnels.Load() != 2 {
		t.Fatalf("target verification should fail after reaching the proxy: error=%v tunnels=%d", err, tunnels.Load())
	}

	allow.Store(false)
	strictDial()
	entryTLS["insecure"] = true
	raw, err = json.Marshal(options)
	if err != nil {
		t.Fatal(err)
	}
	explicit, err := builder.Build(raw)
	if err != nil {
		t.Fatal(err)
	}
	defer closeOutbound(explicit)
	conn, err = explicit.DialContext(ctx, "tcp", destination)
	if err != nil {
		t.Fatalf("per-node allowInsecure should still work: %v", err)
	}
	_ = conn.Close()
}
