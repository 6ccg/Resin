package nodeexport

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Resinat/Resin/internal/node"
)

func TestName_MatchesEasyTest(t *testing.T) {
	name := Name("us", &node.EgressAttributes{CountryCode: "US", NetworkType: "isp / hosting", Company: "[Example]\nCloud"})
	if name != "[US][isp / hosting][Example Cloud]" {
		t.Fatalf("incorrect name: %q", name)
	}
	if name := Name("", nil); name != "[unknown][unknown][unknown]" {
		t.Fatalf("incorrect missing values: %s", name)
	}
}

func TestURIAndClash_RoundTripCredentialsAndOptions(t *testing.T) {
	cases := []string{
		`{"type":"http","server":"2001:db8::1","server_port":8080,"username":"a@b","password":"p:a#ss"}`,
		`{"type":"http","server":"example.com","server_port":443,"username":"user","password":"pass","tls":{"enabled":true,"server_name":"proxy.example.com","insecure":true}}`,
		`{"type":"socks","server":"127.0.0.1","server_port":1080,"version":"5","username":"user","password":"p@ss"}`,
		`{"type":"shadowsocks","server":"example.com","server_port":443,"method":"aes-128-gcm","password":"a:b@c"}`,
		`{"type":"vless","server":"example.com","server_port":443,"uuid":"11111111-1111-1111-1111-111111111111","flow":"xtls-rprx-vision","tls":{"enabled":true,"server_name":"www.example.com","utls":{"enabled":true,"fingerprint":"chrome"},"reality":{"enabled":true,"public_key":"test-key","short_id":"1234"}}}`,
		`{"type":"trojan","server":"example.com","server_port":443,"password":"p@ss","tls":{"enabled":true,"server_name":"www.example.com"},"transport":{"type":"ws","path":"/proxy","headers":{"Host":"www.example.com"}}}`,
		`{"type":"vmess","server":"example.com","server_port":443,"uuid":"11111111-1111-1111-1111-111111111111","security":"auto","alter_id":0,"tls":{"enabled":true,"server_name":"www.example.com"},"transport":{"type":"grpc","service_name":"proxy"}}`,
		`{"type":"hysteria2","server":"example.com","server_port":443,"password":"p@ss","tls":{"enabled":true,"server_name":"example.com"},"obfs":{"type":"salamander","password":"obfs"}}`,
	}
	for _, raw := range cases {
		var header struct {
			Type string `json:"type"`
		}
		_ = json.Unmarshal([]byte(raw), &header)
		t.Run(header.Type, func(t *testing.T) {
			if _, err := URI(json.RawMessage(raw), "[US][hosting][Example]"); err != nil {
				t.Errorf("URI: %v", err)
			}
			if _, err := Clash(json.RawMessage(raw), "[US][hosting][Example]"); err != nil {
				t.Errorf("Clash: %v", err)
			}
		})
	}
}

func TestExportRejectsLostOptions(t *testing.T) {
	raw := json.RawMessage(`{"type":"http","server":"example.com","server_port":443,"tls":{"enabled":true,"certificate":["CERTIFICATE"]}}`)
	if _, err := URI(raw, "name"); err == nil || !strings.Contains(err.Error(), "certificate") {
		t.Fatalf("unsupported TLS option silently lost: %v", err)
	}
	if _, err := Tagged(raw, "name"); err != nil {
		t.Fatal(err)
	}
}
