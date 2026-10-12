package parser

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
	"subconfig/internal/proxy"
)

// Document retains native settings for same-format conversion.
type Document struct {
	Type    string
	Base    map[string]any
	Nodes   []*proxy.Proxy
	Skipped []string
	Lossy   []string
}

func NormalizeInput(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "auto":
		return "auto"
	case "clash", "clashmeta", "clash.meta", "mihomo", "clashr":
		return "clash"
	case "singbox", "sing-box", "sing_box":
		return "singbox"
	case "mixed", "v2ray", "base64", "uri", "raw", "links", "ss":
		return "mixed"
	default:
		return strings.ToLower(s)
	}
}

func ParseDocument(data []byte, hint, inputType string) (*Document, error) {
	typ := NormalizeInput(inputType)
	if typ != "auto" && typ != "clash" && typ != "singbox" && typ != "mixed" {
		return nil, fmt.Errorf("unsupported input type %q", inputType)
	}
	data = []byte(strings.TrimPrefix(strings.TrimSpace(string(data)), "\ufeff"))
	var base map[string]any
	var decodeErr error
	if strings.HasPrefix(string(data), "{") {
		decodeErr = json.Unmarshal(stripJSONComments(data), &base)
	} else {
		decodeErr = yaml.Unmarshal(data, &base)
	}
	if typ == "auto" {
		switch {
		case base != nil && base["outbounds"] != nil:
			typ = "singbox"
		case base != nil && (base["proxies"] != nil || base["proxy-providers"] != nil):
			typ = "clash"
		case strings.EqualFold(filepath.Ext(strings.Split(hint, "?")[0]), ".json") || strings.EqualFold(filepath.Ext(hint), ".jsonc"):
			return nil, fmt.Errorf("cannot infer JSON config: expected outbounds (sing-box) or proxies (Clash)")
		default:
			typ = "mixed"
		}
	}
	d := &Document{Type: typ, Base: base}
	if typ == "mixed" {
		var err error
		d.Base = nil
		d.Nodes, d.Skipped, err = Parse(data)
		return d, err
	}
	if decodeErr != nil {
		return nil, fmt.Errorf("decode %s: %w", typ, decodeErr)
	}
	if base == nil {
		return nil, fmt.Errorf("expected %s configuration object", typ)
	}
	key := "proxies"
	if typ == "singbox" {
		key = "outbounds"
	}
	items, ok := base[key].([]any)
	if !ok {
		return nil, fmt.Errorf("%s: expected %s array (provider-only configs need resolved nodes)", typ, key)
	}
	for i, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			d.Skipped = append(d.Skipped, fmt.Sprintf("%s[%d]: expected object", key, i))
			continue
		}
		if typ == "clash" {
			name, t := str(m, "name"), str(m, "type")
			if name == "" || t == "" || str(m, "server") == "" || anyToInt(m["port"]) < 1 {
				d.Skipped = append(d.Skipped, fmt.Sprintf("proxy[%d]: missing name/type/server/port", i))
				continue
			}
			d.Nodes = append(d.Nodes, &proxy.Proxy{Name: name, Type: t, Server: str(m, "server"), Port: anyToInt(m["port"]), Clash: m})
		} else {
			switch str(m, "type") {
			case "selector", "urltest", "direct", "block", "dns":
				continue
			}
			n, err := singboxNode(m, i)
			if err != nil {
				d.Skipped = append(d.Skipped, err.Error())
				continue
			}
			d.Nodes = append(d.Nodes, n)
			known := map[string]bool{}
			for _, k := range []string{"type", "tag", "server", "server_port", "password", "uuid", "method", "security", "alter_id", "flow", "username", "version", "congestion_control", "udp_relay_mode", "up_mbps", "down_mbps", "auth_str", "auth", "private_key", "peer_public_key", "local_address", "mtu", "reserved", "tcp_fast_open", "tls", "transport", "obfs", "plugin", "plugin_opts"} {
				known[k] = true
			}
			for k := range m {
				if !known[k] {
					d.Lossy = append(d.Lossy, fmt.Sprintf("outbound[%d].%s: native option cannot be translated", i, k))
				}
			}
			for k := range obj(m, "tls") {
				switch k {
				case "enabled", "server_name", "insecure", "alpn", "utls", "reality":
				default:
					d.Lossy = append(d.Lossy, fmt.Sprintf("outbound[%d].tls.%s: native option cannot be translated", i, k))
				}
			}

		}
	}
	if eps, ok := base["endpoints"].([]any); ok && len(eps) > 0 {
		d.Skipped = append(d.Skipped, "sing-box endpoints are preserved for same-format output but cannot be converted to other formats")
	}
	return d, nil
}

func str(m map[string]any, k string) string         { s, _ := m[k].(string); return s }
func obj(m map[string]any, k string) map[string]any { v, _ := m[k].(map[string]any); return v }
func singboxNode(m map[string]any, i int) (*proxy.Proxy, error) {
	t := str(m, "type")
	name := str(m, "tag")
	if name == "" {
		name = fmt.Sprintf("%s-%d", t, i+1)
	}
	switch t {
	case "shadowsocks":
		t = "ss"
	case "socks":
		t = "socks5"
	case "http", "vmess", "vless", "trojan", "hysteria", "hysteria2", "tuic", "anytls", "wireguard", "shadowsocksr":
		if t == "shadowsocksr" {
			t = "ssr"
		}
	default:
		return nil, fmt.Errorf("outbound[%d]: unsupported type %s", i, t)
	}
	port := anyToInt(m["server_port"])
	if str(m, "server") == "" || port < 1 || port > 65535 {
		return nil, fmt.Errorf("outbound[%d]: invalid server/server_port", i)
	}
	n := proxy.New(t, name, str(m, "server"), port)
	n.Singbox = m
	pairs := map[string]string{"password": "password", "uuid": "uuid", "method": "cipher", "security": "cipher", "alter_id": "alterId", "flow": "flow", "username": "username", "congestion_control": "congestion-controller", "udp_relay_mode": "udp-relay-mode", "up_mbps": "up", "down_mbps": "down", "auth_str": "auth-str", "auth": "auth", "private_key": "private-key", "peer_public_key": "public-key", "local_address": "ip", "mtu": "mtu", "reserved": "reserved", "tcp_fast_open": "tfo"}
	for a, b := range pairs {
		if v, ok := m[a]; ok {
			n.SetRaw(b, v)
		}
	}
	if t == "socks5" && str(m, "version") == "4" {
		n.Type = "socks4"
		n.Clash["type"] = "socks4"
	}
	tls := obj(m, "tls")
	if len(tls) > 0 {
		n.SetRaw("tls", tls["enabled"])
		sniKey := "sni"
		if t == "vmess" || t == "vless" {
			sniKey = "servername"
		}
		n.Set(sniKey, str(tls, "server_name"))
		if v, ok := tls["insecure"]; ok {
			n.SetRaw("skip-cert-verify", v)
		}
		if v, ok := tls["alpn"]; ok {
			n.SetRaw("alpn", v)
		}
		n.Set("client-fingerprint", str(obj(tls, "utls"), "fingerprint"))
		if r := obj(tls, "reality"); len(r) > 0 {
			n.SetRaw("reality-opts", map[string]any{"public-key": str(r, "public_key"), "short-id": str(r, "short_id")})
		}
	}
	if tr := obj(m, "transport"); len(tr) > 0 {
		switch str(tr, "type") {
		case "ws":
			n.Set("network", "ws")
			n.SetRaw("ws-opts", map[string]any{"path": str(tr, "path"), "headers": tr["headers"], "max-early-data": tr["max_early_data"], "early-data-header-name": tr["early_data_header_name"]})
		case "grpc":
			n.Set("network", "grpc")
			n.SetRaw("grpc-opts", map[string]any{"grpc-service-name": str(tr, "service_name")})
		case "http":
			n.Set("network", "h2")
			n.SetRaw("h2-opts", map[string]any{"path": str(tr, "path"), "host": tr["host"]})
		default:
			return nil, fmt.Errorf("outbound[%d]: transport %s cannot be converted", i, str(tr, "type"))
		}
	}
	if plugin := str(m, "plugin"); plugin != "" {
		opts := map[string]any{}
		for _, part := range strings.Split(str(m, "plugin_opts"), ";") {
			k, v, ok := strings.Cut(part, "=")
			if !ok {
				opts[k] = true
			} else {
				opts[k] = v
			}
		}
		switch plugin {
		case "obfs-local":
			n.Set("plugin", "obfs")
			n.SetRaw("plugin-opts", map[string]any{"mode": opts["obfs"], "host": opts["obfs-host"]})
		case "v2ray-plugin":
			n.Set("plugin", plugin)
			n.SetRaw("plugin-opts", opts)
		default:
			return nil, fmt.Errorf("outbound[%d]: plugin cannot be converted", i)
		}
	}
	if ob := obj(m, "obfs"); len(ob) > 0 {
		n.Set("obfs", str(ob, "type"))
		n.Set("obfs-password", str(ob, "password"))
	}
	return n, nil
}

// Comments are removed without touching URLs or escaped quotes inside strings.
func stripJSONComments(src []byte) []byte {
	out := append([]byte(nil), src...)
	inString, escape := false, false
	for i := 0; i < len(out); i++ {
		if inString {
			if escape {
				escape = false
			} else if out[i] == '\\' {
				escape = true
			} else if out[i] == '"' {
				inString = false
			}
			continue
		}
		if out[i] == '"' {
			inString = true
			continue
		}
		if out[i] == '/' && i+1 < len(out) {
			if out[i+1] == '/' {
				out[i] = ' '
				out[i+1] = ' '
				i += 2
				for i < len(out) && out[i] != '\n' {
					out[i] = ' '
					i++
				}
				i--
			} else if out[i+1] == '*' {
				out[i] = ' '
				out[i+1] = ' '
				i += 2
				for i+1 < len(out) && !(out[i] == '*' && out[i+1] == '/') {
					if out[i] != '\n' {
						out[i] = ' '
					}
					i++
				}
				if i+1 < len(out) {
					out[i] = ' '
					out[i+1] = ' '
					i++
				}
			}
		}
	}
	return out
}
