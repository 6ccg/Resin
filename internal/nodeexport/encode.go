// Package nodeexport serializes normalized nodes without changing their entry.
package nodeexport

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"reflect"
	"strconv"
	"strings"

	"github.com/Resinat/Resin/internal/node"
	"github.com/Resinat/Resin/internal/subscription"
)

func cleanField(value string) string {
	value = strings.NewReplacer("\r", " ", "\n", " ", "[", "", "]", "").Replace(value)
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}
	return value
}

func Name(country string, attributes *node.EgressAttributes) string {
	kind, company := "", ""
	if attributes != nil {
		if attributes.CountryCode != "" {
			country = attributes.CountryCode
		}
		kind = attributes.NetworkType
		company = attributes.Company
		if company == "" {
			company = attributes.Organization
		}
	}
	return "[" + cleanField(strings.ToUpper(country)) + "][" + cleanField(kind) + "][" + cleanField(company) + "]"
}

func decode(raw json.RawMessage) (map[string]any, error) {
	var options map[string]any
	err := json.Unmarshal(raw, &options)
	return options, err
}
func text(options map[string]any, key string) string { value, _ := options[key].(string); return value }
func object(options map[string]any, key string) map[string]any {
	value, _ := options[key].(map[string]any)
	if value == nil {
		return map[string]any{}
	}
	return value
}
func flag(options map[string]any, key string) bool { value, _ := options[key].(bool); return value }
func number(options map[string]any, key string) int {
	value, _ := options[key].(float64)
	return int(value)
}
func list(options map[string]any, key string) []string {
	values, _ := options[key].([]any)
	result := []string{}
	for _, value := range values {
		if item, ok := value.(string); ok {
			result = append(result, item)
		}
	}
	return result
}
func supported(options map[string]any, keys ...string) error {
	allowed := map[string]bool{}
	for _, key := range keys {
		allowed[key] = true
	}
	for key := range options {
		if !allowed[key] {
			return fmt.Errorf("field %s requires sing-box/JSON export", key)
		}
	}
	return nil
}

func tlsQuery(options map[string]any, query url.Values) error {
	tls := object(options, "tls")
	if err := supported(tls, "enabled", "server_name", "insecure", "alpn", "utls", "reality"); err != nil {
		return err
	}
	if flag(tls, "enabled") {
		query.Set("security", "tls")
	}
	if value := text(tls, "server_name"); value != "" {
		query.Set("sni", value)
	}
	if flag(tls, "insecure") {
		query.Set("allowInsecure", "1")
	}
	if alpn := list(tls, "alpn"); len(alpn) > 0 {
		query.Set("alpn", strings.Join(alpn, ","))
	}
	utls := object(tls, "utls")
	if err := supported(utls, "enabled", "fingerprint"); err != nil {
		return err
	}
	if flag(utls, "enabled") {
		query.Set("fp", text(utls, "fingerprint"))
	}
	reality := object(tls, "reality")
	if err := supported(reality, "enabled", "public_key", "short_id"); err != nil {
		return err
	}
	if flag(reality, "enabled") {
		query.Set("security", "reality")
		query.Set("pbk", text(reality, "public_key"))
		query.Set("sid", text(reality, "short_id"))
	}
	return nil
}

func transportQuery(options map[string]any, query url.Values) error {
	transport := object(options, "transport")
	if len(transport) == 0 {
		return nil
	}
	if err := supported(transport, "type", "path", "headers", "service_name", "host"); err != nil {
		return err
	}
	kind := text(transport, "type")
	switch kind {
	case "ws", "grpc", "http", "httpupgrade", "quic":
	default:
		return fmt.Errorf("transport %s requires sing-box export", kind)
	}
	query.Set("type", kind)
	if value := text(transport, "path"); value != "" {
		query.Set("path", value)
	}
	if value := text(transport, "service_name"); value != "" {
		query.Set("serviceName", value)
	}
	headers := object(transport, "headers")
	if err := supported(headers, "Host", "host"); err != nil {
		return err
	}
	host := text(headers, "Host")
	if host == "" {
		host = text(headers, "host")
	}
	if host == "" {
		host = text(transport, "host")
	}
	if host == "" {
		host = strings.Join(list(transport, "host"), ",")
	}
	if host != "" {
		query.Set("host", host)
	}
	return nil
}

// URI supports the common share-link protocols; unsupported options are
// reported to the caller rather than silently discarded.
func URI(raw json.RawMessage, name string) (string, error) {
	encoded, err := encodeURI(raw, name)
	if err != nil {
		return "", err
	}
	if err = checkRoundTrip(raw, []byte(encoded)); err != nil {
		return "", err
	}
	return encoded, nil
}

func encodeURI(raw json.RawMessage, name string) (string, error) {
	o, err := decode(raw)
	if err != nil {
		return "", err
	}
	kind := text(o, "type")
	if err := supported(o, "type", "tag", "server", "server_port", "username", "password", "version", "method", "plugin", "plugin_opts", "uuid", "security", "alter_id", "flow", "tls", "transport", "network", "up_mbps", "down_mbps", "obfs", "server_ports", "hop_interval"); err != nil {
		return "", err
	}
	server, port := text(o, "server"), number(o, "server_port")
	if server == "" || port < 1 {
		return "", fmt.Errorf("node has no server endpoint")
	}
	u := &url.URL{Scheme: kind, Host: net.JoinHostPort(server, strconv.Itoa(port))}
	query := url.Values{}
	switch kind {
	case "http", "socks":
		if len(object(o, "transport")) > 0 {
			return "", fmt.Errorf("proxy transport requires sing-box export")
		}
		if kind == "socks" {
			if version := text(o, "version"); version != "" && version != "5" {
				return "", fmt.Errorf("SOCKS%s requires sing-box export", version)
			}
			u.Scheme = "socks5"
		}
		if flag(object(o, "tls"), "enabled") {
			if kind != "http" {
				return "", fmt.Errorf("SOCKS TLS requires sing-box export")
			}
			u.Scheme = "https"
		}
		if text(o, "username") != "" || text(o, "password") != "" {
			u.User = url.UserPassword(text(o, "username"), text(o, "password"))
		}
		if err := tlsQuery(o, query); err != nil {
			return "", err
		}
		query.Del("security")
		if len(object(object(o, "tls"), "reality")) > 0 || len(object(object(o, "tls"), "utls")) > 0 || len(list(object(o, "tls"), "alpn")) > 0 {
			return "", fmt.Errorf("advanced proxy TLS requires sing-box export")
		}
	case "shadowsocks":
		if len(object(o, "tls")) > 0 || len(object(o, "transport")) > 0 {
			return "", fmt.Errorf("Shadowsocks TLS/transport requires sing-box export")
		}
		u.Scheme = "ss"
		u.User = url.User(base64.RawURLEncoding.EncodeToString([]byte(text(o, "method") + ":" + text(o, "password"))))
		if plugin := text(o, "plugin"); plugin != "" {
			if opts := text(o, "plugin_opts"); opts != "" {
				plugin += ";" + opts
			}
			query.Set("plugin", plugin)
		}
	case "vless", "trojan", "hysteria2":
		credential := text(o, "uuid")
		if kind != "vless" {
			credential = text(o, "password")
		}
		u.User = url.User(credential)
		if err := tlsQuery(o, query); err != nil {
			return "", err
		}
		if kind == "vless" {
			if flow := text(o, "flow"); flow != "" {
				query.Set("flow", flow)
			}
		}
		if err := transportQuery(o, query); err != nil {
			return "", err
		}
		if kind == "hysteria2" {
			if len(object(o, "transport")) > 0 {
				return "", fmt.Errorf("Hysteria2 transport requires sing-box export")
			}
			obfs := object(o, "obfs")
			if err := supported(obfs, "type", "password"); err != nil {
				return "", err
			}
			if text(obfs, "type") != "" {
				query.Set("obfs", text(obfs, "type"))
				query.Set("obfs-password", text(obfs, "password"))
			}
			for _, field := range []string{"up_mbps", "down_mbps"} {
				if value := number(o, field); value > 0 {
					query.Set(field, strconv.Itoa(value))
				}
			}
			if ports := list(o, "server_ports"); len(ports) > 0 {
				query.Set("ports", strings.Join(ports, ","))
			}
			if value := text(o, "hop_interval"); value != "" {
				query.Set("hopInterval", value)
			}
		}
	case "vmess":
		if err := tlsQuery(o, query); err != nil {
			return "", err
		}
		if query.Get("security") == "reality" {
			return "", fmt.Errorf("VMess Reality requires sing-box export")
		}
		if err := transportQuery(o, query); err != nil {
			return "", err
		}
		payload := map[string]any{"v": "2", "ps": name, "add": server, "port": port, "id": text(o, "uuid"), "aid": number(o, "alter_id"), "scy": text(o, "security"), "net": "tcp"}
		if query.Get("security") == "tls" {
			payload["tls"] = "tls"
		}
		for _, key := range []string{"sni", "alpn", "fp", "allowInsecure"} {
			if query.Get(key) != "" {
				payload[key] = query.Get(key)
			}
		}
		if query.Get("type") != "" {
			payload["net"] = query.Get("type")
		}
		payload["host"] = query.Get("host")
		payload["path"] = query.Get("path")
		if query.Get("type") == "grpc" {
			payload["path"] = query.Get("serviceName")
		}
		encoded, _ := json.Marshal(payload)
		return "vmess://" + base64.StdEncoding.EncodeToString(encoded), nil
	default:
		return "", fmt.Errorf("protocol %s has no supported URI encoder; use sing-box/JSON", kind)
	}
	u.RawQuery = query.Encode()
	return u.String() + "#" + name, nil
}

func Tagged(raw json.RawMessage, name string) (map[string]any, error) {
	o, err := decode(raw)
	if err == nil {
		o["tag"] = name
	}
	return o, err
}

func Clash(raw json.RawMessage, name string) (map[string]any, error) {
	proxy, err := encodeClash(raw, name)
	if err != nil {
		return nil, err
	}
	data, _ := json.Marshal(map[string]any{"proxies": []any{proxy}})
	if err = checkRoundTrip(raw, data); err != nil {
		return nil, err
	}
	return proxy, nil
}

func encodeClash(raw json.RawMessage, name string) (map[string]any, error) {
	o, err := decode(raw)
	if err != nil {
		return nil, err
	}
	if err := supported(o, "type", "tag", "server", "server_port", "username", "password", "version", "method", "plugin", "plugin_opts", "uuid", "security", "alter_id", "flow", "tls", "transport", "network", "up_mbps", "down_mbps", "obfs"); err != nil {
		return nil, err
	}
	kind := text(o, "type")
	proxy := map[string]any{"name": name, "type": kind, "server": o["server"], "port": o["server_port"]}
	switch kind {
	case "http":
	case "socks":
		if version := text(o, "version"); version != "" && version != "5" {
			return nil, fmt.Errorf("SOCKS%s requires sing-box export", version)
		}
		proxy["type"] = "socks5"
	case "shadowsocks":
		proxy["type"] = "ss"
		proxy["cipher"] = o["method"]
	case "vmess":
		proxy["uuid"] = o["uuid"]
		proxy["alterId"] = number(o, "alter_id")
		proxy["cipher"] = text(o, "security")
	case "vless":
		proxy["uuid"] = o["uuid"]
		if value := text(o, "flow"); value != "" {
			proxy["flow"] = value
		}
	case "trojan", "hysteria2", "anytls":
	case "tuic":
		proxy["uuid"] = o["uuid"]
	default:
		return nil, fmt.Errorf("protocol %s requires sing-box export", kind)
	}
	for _, field := range []string{"username", "password"} {
		if value := text(o, field); value != "" {
			proxy[field] = value
		}
	}
	if kind == "shadowsocks" && text(o, "plugin") != "" {
		return nil, fmt.Errorf("Shadowsocks plugin export requires sing-box")
	}
	tls := object(o, "tls")
	if err := supported(tls, "enabled", "server_name", "insecure", "alpn", "utls", "reality"); err != nil {
		return nil, err
	}
	if flag(tls, "enabled") {
		proxy["tls"] = true
	}
	if value := text(tls, "server_name"); value != "" {
		proxy["servername"] = value
		proxy["sni"] = value
	}
	if flag(tls, "insecure") {
		proxy["skip-cert-verify"] = true
	}
	if values := list(tls, "alpn"); len(values) > 0 {
		proxy["alpn"] = values
	}
	utls := object(tls, "utls")
	if err := supported(utls, "enabled", "fingerprint"); err != nil {
		return nil, err
	}
	if flag(utls, "enabled") {
		proxy["client-fingerprint"] = text(utls, "fingerprint")
	}
	reality := object(tls, "reality")
	if err := supported(reality, "enabled", "public_key", "short_id"); err != nil {
		return nil, err
	}
	if flag(reality, "enabled") {
		proxy["reality-opts"] = map[string]any{"public-key": text(reality, "public_key"), "short-id": text(reality, "short_id")}
	}
	transport := object(o, "transport")
	if err := supported(transport, "type", "path", "headers", "service_name", "host", "max_early_data", "early_data_header_name"); err != nil {
		return nil, err
	}
	if len(transport) > 0 {
		network := text(transport, "type")
		proxy["network"] = network
		switch network {
		case "ws":
			opts := map[string]any{}
			for _, field := range []string{"path", "headers"} {
				if value, ok := transport[field]; ok {
					opts[field] = value
				}
			}
			if value, ok := transport["max_early_data"]; ok {
				opts["max-early-data"] = value
			}
			if value, ok := transport["early_data_header_name"]; ok {
				opts["early-data-header-name"] = value
			}
			proxy["ws-opts"] = opts
		case "grpc":
			proxy["grpc-opts"] = map[string]any{"grpc-service-name": text(transport, "service_name")}
		case "http":
			proxy["http-opts"] = map[string]any{"path": text(transport, "path"), "host": transport["host"]}
		case "quic":
		default:
			return nil, fmt.Errorf("transport %s requires sing-box export", network)
		}
	}
	if kind == "hysteria2" {
		obfs := object(o, "obfs")
		if err := supported(obfs, "type", "password"); err != nil {
			return nil, err
		}
		if text(obfs, "type") != "" {
			proxy["obfs"] = text(obfs, "type")
			proxy["obfs-password"] = text(obfs, "password")
		}
		for _, field := range []string{"up_mbps", "down_mbps"} {
			if value, ok := o[field]; ok {
				proxy[strings.TrimSuffix(field, "_mbps")] = value
			}
		}
	}
	return proxy, nil
}

func checkRoundTrip(original json.RawMessage, encoded []byte) error {
	parsed, err := subscription.ParseGeneralSubscription(encoded)
	if err != nil || len(parsed) != 1 {
		return fmt.Errorf("export cannot be re-imported; use sing-box/JSON")
	}
	source, err := decode(original)
	if err != nil {
		return err
	}
	target, err := decode(parsed[0].RawOptions)
	if err != nil {
		return err
	}
	delete(source, "tag")
	if text(target, "type") == "socks" && text(target, "version") == "" {
		target["version"] = "5"
	}
	return preservedFields(source, target, "")
}

func preservedFields(source, target map[string]any, path string) error {
	for key, value := range source {
		field := path + key
		if object, ok := value.(map[string]any); ok {
			other, _ := target[key].(map[string]any)
			if err := preservedFields(object, other, field+"."); err != nil {
				return err
			}
			continue
		}
		other, exists := target[key]
		if !exists && (value == nil || reflect.DeepEqual(value, false) || reflect.DeepEqual(value, "") || reflect.DeepEqual(value, float64(0))) {
			continue
		}
		if !reflect.DeepEqual(value, other) {
			return fmt.Errorf("option %s cannot be preserved in this format; use sing-box/JSON", field)
		}
	}
	return nil
}
