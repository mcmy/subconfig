package generator

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"

	"subconfig/internal/extconfig"
	"subconfig/internal/proxy"
)

// GenerateSingbox renders a sing-box JSON config: one outbound per node, plus
// selector/urltest outbounds for the proxy groups and route rules translated
// from the rulesets. Protocols with no sing-box outbound form are skipped and
// reported in Result.SkippedNodes. A built-in base (log/dns/inbounds) is used.
func GenerateSingbox(ctx context.Context, nodes []*proxy.Proxy, cfg *extconfig.Config, f Fetcher, opts Options) (*Result, error) {
	res := &Result{ContentType: ctJSON}
	nodes = prepareNodes(nodes, cfg, opts, res)

	// Node outbounds.
	var outbounds []map[string]any
	emitted := map[string]bool{}
	allNames := make([]string, 0, len(nodes))
	for _, n := range nodes {
		ob, ok := nodeSingboxOutbound(n)
		if !ok {
			res.SkippedNodes = append(res.SkippedNodes, n.Name+" ("+n.Type+": unsupported by sing-box generator)")
			continue
		}
		outbounds = append(outbounds, ob)
		emitted[n.Name] = true
		allNames = append(allNames, n.Name)
	}

	for _, ob := range opts.NativeOutbounds {
		tag, _ := ob["tag"].(string)
		if !emitted[tag] {
			outbounds = append(outbounds, ob)
			emitted[tag] = true
		}
	}

	// Group outbounds (selector / urltest). Names of all groups so member refs to
	// other groups survive the emitted-filter.
	groupNames := map[string]bool{}
	for _, d := range cfg.ProxyGroups {
		groupNames[d.Name] = true
	}
	groups := buildGroups(cfg.ProxyGroups, nodes, allNames, res)
	for _, g := range groups {
		if g.Type != "select" && g.Type != "url-test" {
			res.SkippedRules = append(res.SkippedRules, "group "+g.Name+": "+g.Type+" converted to urltest")
		}
		for _, m := range g.Proxies {
			if m == "REJECT" || m == "REJECT-DROP" {
				res.SkippedRules = append(res.SkippedRules, "group "+g.Name+": selectable reject member omitted")
			}
		}
		outbounds = append(outbounds, singboxGroupOutbound(g, emitted, groupNames))
	}

	// Built-in tail outbounds.
	if !emitted["direct"] {
		outbounds = append(outbounds, map[string]any{"type": "direct", "tag": "direct"})
	}

	// Route rules from neutral rules.
	neutral, skippedRules := collectRules(ctx, cfg, f, opts)
	routeRules := []map[string]any{{"protocol": "dns", "action": "hijack-dns"}}
	final := "direct"
	if len(groups) > 0 {
		final = groups[0].Name
	}
	for _, r := range neutral {
		if r.IsMatch() {
			if r.Group == "REJECT" || r.Group == "REJECT-DROP" {
				routeRules = append(routeRules, map[string]any{"action": "reject"})
			} else {
				final = singboxOutboundRef(r.Group, emitted, groupNames)
			}
			continue
		}
		if rule, ok := singboxRouteRule(r, emitted, groupNames); ok {
			routeRules = append(routeRules, rule)
		} else {
			skippedRules = append(skippedRules, r.Type+","+r.Value)
		}
	}
	res.SkippedRules = append(res.SkippedRules, skippedRules...)

	conf := map[string]any{
		"log": map[string]any{"level": "info", "timestamp": true},
		"dns": map[string]any{
			"servers": []map[string]any{
				{"type": "tls", "tag": "remote", "server": "8.8.8.8"},
				{"type": "udp", "tag": "local", "server": "223.5.5.5"},
			},
			"final": "remote",
		},
		"inbounds": []map[string]any{
			{"type": "mixed", "tag": "mixed-in", "listen": "127.0.0.1", "listen_port": 2080},
		},
		"outbounds": outbounds,
		"route": map[string]any{
			"rules":                   routeRules,
			"final":                   final,
			"auto_detect_interface":   true,
			"default_domain_resolver": "local",
		},
	}

	b, err := json.MarshalIndent(conf, "", "  ")
	if err != nil {
		return nil, err
	}
	res.Output = b
	return res, nil
}

// singboxOutboundRef maps a selector member / rule group to a sing-box outbound
// tag (DIRECT->direct, REJECT->block; node tags kept only when emitted).
func singboxOutboundRef(m string, emitted, groupNames map[string]bool) string {
	switch m {
	case "DIRECT":
		return "direct"
	case "REJECT", "REJECT-DROP":
		return "block"
	}
	if groupNames[m] || emitted[m] {
		return m
	}
	return m // group/rule target we can't classify: keep verbatim
}

// singboxGroupOutbound renders a proxy group as a selector or urltest outbound.
func singboxGroupOutbound(g groupOut, emitted, groupNames map[string]bool) map[string]any {
	members := make([]string, 0, len(g.Proxies))
	for _, m := range g.Proxies {
		switch m {
		case "DIRECT":
			members = append(members, "direct")
		case "REJECT", "REJECT-DROP":
			// Reject is a route action in modern sing-box, not an outbound.
			continue
		default:
			if groupNames[m] || emitted[m] {
				members = append(members, m)
			}
		}
	}
	if len(members) == 0 {
		members = []string{"direct"}
	}
	if g.Type == "select" {
		return map[string]any{"type": "selector", "tag": g.Name, "outbounds": members}
	}
	// url-test / fallback / load-balance / relay -> urltest
	ob := map[string]any{
		"type":      "urltest",
		"tag":       g.Name,
		"outbounds": members,
		"url":       firstNonEmptyStr(g.URL, "https://www.gstatic.com/generate_204"),
	}
	interval := g.Interval
	if interval <= 0 {
		interval = 300
	}
	ob["interval"] = strconv.Itoa(interval) + "s"
	if g.Tolerance > 0 {
		ob["tolerance"] = g.Tolerance
	}
	return ob
}

// singboxRuleField maps a neutral rule type to its sing-box route-rule field
// name. The empty string marks an unsupported type.
var singboxRuleField = map[string]string{
	"DOMAIN": "domain", "DOMAIN-SUFFIX": "domain_suffix", "DOMAIN-KEYWORD": "domain_keyword",
	"DOMAIN-REGEX": "domain_regex",
	"IP-CIDR":      "ip_cidr", "IP-CIDR6": "ip_cidr", "DST-PORT": "port", "SRC-PORT": "source_port",
	"PROCESS-NAME": "process_name",
}

// singboxRouteRule builds a single route rule object. ok is false for rule types
// sing-box has no field for.
func singboxRouteRule(r Rule, emitted, groupNames map[string]bool) (map[string]any, bool) {
	field, ok := singboxRuleField[strings.ToUpper(r.Type)]
	if !ok {
		return nil, false
	}
	rule := map[string]any{"outbound": singboxOutboundRef(r.Group, emitted, groupNames)}
	if r.Group == "REJECT" || r.Group == "REJECT-DROP" {
		delete(rule, "outbound")
		rule["action"] = "reject"
	}
	switch field {
	case "port", "source_port":
		if p, err := strconv.Atoi(r.Value); err == nil {
			rule[field] = []int{p}
		} else {
			return nil, false
		}
	case "geoip", "geosite":
		rule[field] = []string{strings.ToLower(r.Value)}
	default:
		rule[field] = []string{r.Value}
	}
	return rule, true
}

// nodeSingboxOutbound builds the sing-box outbound object for one node.
func nodeSingboxOutbound(n *proxy.Proxy) (map[string]any, bool) {
	if n.Singbox != nil {
		b, _ := json.Marshal(n.Singbox)
		var ob map[string]any
		_ = json.Unmarshal(b, &ob)
		ob["tag"] = n.Name
		if _, ok := n.Clash["tfo"]; ok {
			ob["tcp_fast_open"] = cb(n.Clash, "tfo")
		}
		if _, ok := n.Clash["skip-cert-verify"]; ok {
			if t := cmap(ob, "tls"); t != nil {
				t["insecure"] = cb(n.Clash, "skip-cert-verify")
			}
		}
		return ob, true
	}
	c := n.Clash
	base := map[string]any{
		"tag":         n.Name,
		"server":      cs(c, "server"),
		"server_port": ci(c, "port"),
	}
	if _, ok := c["tfo"]; ok {
		base["tcp_fast_open"] = cb(c, "tfo")
	}
	switch n.Type {
	case "http":
		base["type"] = "http"
		for _, k := range []string{"username", "password"} {
			if v := cs(c, k); v != "" {
				base[k] = v
			}
		}
		if tls := singboxTLS(c, cs(c, "sni"), false); tls != nil {
			base["tls"] = tls
		}
		return base, true
	case "anytls":
		base["type"] = "anytls"
		base["password"] = cs(c, "password")
		base["tls"] = singboxTLS(c, cs(c, "sni"), true)
		return base, true
	case "ss":
		base["type"] = "shadowsocks"
		base["method"] = cs(c, "cipher")
		base["password"] = cs(c, "password")
		if plugin := cs(c, "plugin"); plugin != "" {
			q, _ := url.ParseQuery(ssPluginQuery(c))
			parts := strings.SplitN(q.Get("plugin"), ";", 2)
			if len(parts) > 0 && parts[0] != "" {
				base["plugin"] = parts[0]
				if len(parts) > 1 {
					base["plugin_opts"] = parts[1]
				}
			} else {
				return nil, false
			}
		}
		return base, true

	case "vmess":
		base["type"] = "vmess"
		base["uuid"] = cs(c, "uuid")
		base["alter_id"] = ci(c, "alterId")
		base["security"] = firstNonEmptyStr(cs(c, "cipher"), "auto")
		if t := singboxTransport(c); t != nil {
			base["transport"] = t
		}
		if tls := singboxTLS(c, cs(c, "servername"), false); tls != nil {
			base["tls"] = tls
		}
		return base, true

	case "vless":
		base["type"] = "vless"
		base["uuid"] = cs(c, "uuid")
		if flow := cs(c, "flow"); flow != "" {
			base["flow"] = flow
		}
		if t := singboxTransport(c); t != nil {
			base["transport"] = t
		}
		if tls := singboxTLS(c, cs(c, "servername"), false); tls != nil {
			base["tls"] = tls
		}
		return base, true

	case "trojan":
		base["type"] = "trojan"
		base["password"] = cs(c, "password")
		if t := singboxTransport(c); t != nil {
			base["transport"] = t
		}
		base["tls"] = singboxTLS(c, cs(c, "sni"), true)
		return base, true

	case "hysteria2":
		base["type"] = "hysteria2"
		base["password"] = cs(c, "password")
		if obfs := cs(c, "obfs"); obfs != "" {
			base["obfs"] = map[string]any{"type": obfs, "password": cs(c, "obfs-password")}
		}
		base["tls"] = singboxTLS(c, cs(c, "sni"), true)
		return base, true

	case "tuic":
		base["type"] = "tuic"
		base["uuid"] = cs(c, "uuid")
		base["password"] = cs(c, "password")
		if cc := cs(c, "congestion-controller"); cc != "" {
			base["congestion_control"] = cc
		}
		base["tls"] = singboxTLS(c, cs(c, "sni"), true)
		return base, true

	case "socks5", "socks":
		base["type"] = "socks"
		base["version"] = "5"
		if u := cs(c, "username"); u != "" {
			base["username"] = u
		}
		if pw := cs(c, "password"); pw != "" {
			base["password"] = pw
		}
		return base, true
	}
	return nil, false
}

// singboxTransport builds the transport object for ws/grpc/http networks, or nil
// for plain tcp.
func singboxTransport(c map[string]any) map[string]any {
	switch cs(c, "network") {
	case "ws":
		ws := cmap(c, "ws-opts")
		t := map[string]any{"type": "ws"}
		if p := cs(ws, "path"); p != "" {
			t["path"] = p
		}
		if hdr := cmap(ws, "headers"); hdr != nil {
			if h := cs(hdr, "Host"); h != "" {
				t["headers"] = map[string]any{"Host": h}
			}
		}
		return t
	case "grpc":
		return map[string]any{"type": "grpc", "service_name": cs(cmap(c, "grpc-opts"), "grpc-service-name")}
	case "h2":
		h2 := cmap(c, "h2-opts")
		t := map[string]any{"type": "http"}
		if p := cs(h2, "path"); p != "" {
			t["path"] = p
		}
		if h := joinAny(h2["host"]); h != "" {
			t["host"] = []string{h}
		}
		return t
	}
	return nil
}

// singboxTLS builds a tls object. always forces enabled for protocols that are
// inherently TLS (trojan/hysteria2/tuic); for vmess/vless it returns nil unless
// the node enables TLS.
func singboxTLS(c map[string]any, serverName string, always bool) map[string]any {
	if !always && !cb(c, "tls") {
		return nil
	}
	tls := map[string]any{"enabled": true}
	if serverName != "" {
		tls["server_name"] = serverName
	}
	if cb(c, "skip-cert-verify") {
		tls["insecure"] = true
	}
	if alpn := splitAny(c["alpn"]); len(alpn) > 0 {
		tls["alpn"] = alpn
	}
	if fp := cs(c, "client-fingerprint"); fp != "" {
		tls["utls"] = map[string]any{"enabled": true, "fingerprint": fp}
	}
	if ro := cmap(c, "reality-opts"); len(ro) > 0 {
		reality := map[string]any{"enabled": true}
		if pbk := cs(ro, "public-key"); pbk != "" {
			reality["public_key"] = pbk
		}
		if sid := cs(ro, "short-id"); sid != "" {
			reality["short_id"] = sid
		}
		tls["reality"] = reality
	}
	return tls
}

// splitAny renders a string or []string/[]any field as a []string (for alpn).
func splitAny(v any) []string {
	switch t := v.(type) {
	case string:
		if t == "" {
			return nil
		}
		return []string{t}
	case []string:
		return t
	case []any:
		var out []string
		for _, e := range t {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}
