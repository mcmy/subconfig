package app

import (
	"encoding/json"
	"fmt"
	"gopkg.in/yaml.v3"
	"strconv"
	"strings"
	"subconfig/internal/extconfig"
	"subconfig/internal/generator"
	"subconfig/internal/parser"
	"time"
)

func nativeGroups(d *parser.Document, target string) []extconfig.ProxyGroup {
	var out []extconfig.ProxyGroup
	available := map[string]bool{}
	for _, n := range d.Nodes {
		available[n.Name] = true
	}
	if d.Type == "singbox" && target != "singbox" {
		a, _ := d.Base["outbounds"].([]any)
		for _, v := range a {
			m, _ := v.(map[string]any)
			if m["type"] == "selector" || m["type"] == "urltest" {
				s, _ := m["tag"].(string)
				available[s] = true
			}
		}
		for _, s := range []string{"DIRECT", "REJECT", "REJECT-DROP"} {
			available[s] = true
		}
	}
	key := "proxy-groups"
	if d.Type == "singbox" {
		key = "outbounds"
	}
	items, _ := d.Base[key].([]any)
	for _, item := range items {
		m, _ := item.(map[string]any)
		name, _ := m["name"].(string)
		typ, _ := m["type"].(string)
		members := m["proxies"]
		if d.Type == "singbox" {
			name, _ = m["tag"].(string)
			members = m["outbounds"]
			if typ == "selector" {
				typ = "select"
			} else if typ == "urltest" {
				typ = "url-test"
			} else {
				continue
			}
		}
		if name == "" {
			continue
		}
		g := extconfig.ProxyGroup{Name: name, Type: typ}
		g.TestURL, _ = m["url"].(string)
		g.Tolerance = number(m["tolerance"])
		g.Interval = number(m["interval"])
		if s, ok := m["interval"].(string); ok {
			if dur, e := time.ParseDuration(s); e == nil {
				g.Interval = int(dur.Seconds())
			}
		}
		for _, v := range stringsList(members) {
			if d.Type == "singbox" {
				if target != "singbox" {
					v = nativeBuiltin(d, v)
				} else {
					v = builtin(v)
				}
			}
			if d.Type == "singbox" && target != "singbox" && !available[v] {
				continue
			}
			g.Selectors = append(g.Selectors, "[]"+v)
		}
		out = append(out, g)
	}
	return out
}
func builtin(v string) string {
	switch v {
	case "direct":
		return "DIRECT"
	case "block":
		return "REJECT"
	}
	return v
}
func number(v any) int {
	switch x := v.(type) {
	case int:
		return x
	case float64:
		return int(x)
	case string:
		n, _ := strconv.Atoi(x)
		return n
	}
	return 0
}
func stringsList(v any) []string {
	if s, ok := v.(string); ok {
		return []string{s}
	}
	var out []string
	if a, ok := v.([]any); ok {
		for _, v := range a {
			if s, ok := v.(string); ok {
				out = append(out, s)
			}
		}
	}
	return out
}
func clashRule(s string) (extconfig.Ruleset, error) {
	p := splitRule(s)
	for i := range p {
		p[i] = strings.TrimSpace(p[i])
	}
	if len(p) == 2 && (p[0] == "MATCH" || p[0] == "FINAL") {
		return extconfig.Ruleset{Group: p[1], Inline: "FINAL"}, nil
	}
	if len(p) < 3 || len(p) > 4 || p[0] == "AND" || p[0] == "OR" || p[0] == "NOT" || p[0] == "SUB-RULE" {
		return extconfig.Ruleset{}, fmt.Errorf("unsupported routing rule %q", s)
	}
	inline := p[0] + "," + p[1]
	if len(p) == 4 {
		inline += "," + p[3]
	}
	return extconfig.Ruleset{Group: p[2], Inline: inline}, nil
}
func nativeRules(d *parser.Document, warnings []string) ([]extconfig.Ruleset, []string) {
	var out []extconfig.Ruleset
	if d.Type == "clash" {
		for _, s := range stringsList(d.Base["rules"]) {
			r, e := clashRule(s)
			if e != nil {
				warnings = append(warnings, "unsupported source routing rule")
				continue
			}
			if strings.HasPrefix(r.Inline, "RULE-SET,") {
				warnings = append(warnings, "source rule-provider references cannot be translated; use --external")
				continue
			}
			out = append(out, r)
		}
	}
	if d.Type == "singbox" {
		route, _ := d.Base["route"].(map[string]any)
		rules, _ := route["rules"].([]any)
		fields := map[string]string{"domain": "DOMAIN", "domain_suffix": "DOMAIN-SUFFIX", "domain_keyword": "DOMAIN-KEYWORD", "domain_regex": "DOMAIN-REGEX", "ip_cidr": "IP-CIDR", "port": "DST-PORT", "source_port": "SRC-PORT", "process_name": "PROCESS-NAME", "geoip": "GEOIP", "geosite": "GEOSITE"}
		for i, v := range rules {
			r, _ := v.(map[string]any)
			if r["action"] == "hijack-dns" {
				continue
			}
			group, _ := r["outbound"].(string)
			if r["action"] == "reject" {
				group = "REJECT"
			}
			group = nativeBuiltin(d, group)
			// Different matcher fields are AND in sing-box; splitting would change policy.
			var field, typ string
			valid := group != ""
			for k := range r {
				if k == "outbound" || k == "action" {
					if k == "action" && r[k] != "route" && r[k] != "reject" {
						valid = false
					}
					continue
				}
				if t, ok := fields[k]; ok && field == "" {
					field = k
					typ = t
				} else {
					valid = false
				}
			}
			if !valid || field == "" {
				warnings = append(warnings, fmt.Sprintf("route.rules[%d]: matcher/action cannot be translated", i))
				continue
			}
			values := stringsList(r[field])
			if len(values) == 0 {
				if vs, ok := r[field].([]any); ok {
					for _, v := range vs {
						values = append(values, fmt.Sprint(v))
					}
				} else {
					values = []string{fmt.Sprint(r[field])}
				}
			}
			for _, v := range values {
				out = append(out, extconfig.Ruleset{Group: group, Inline: typ + "," + v})
			}
		}
		if final, ok := route["final"].(string); ok {
			out = append(out, extconfig.Ruleset{Group: nativeBuiltin(d, final), Inline: "FINAL"})
		}
	}
	return out, warnings
}
func decodeBase(data []byte, target string) (map[string]any, error) {
	var m map[string]any
	var err error
	if target == "singbox" {
		err = json.Unmarshal(data, &m)
	} else {
		err = yaml.Unmarshal(data, &m)
	}
	if err != nil || m == nil {
		return nil, fmt.Errorf("invalid %s template", target)
	}
	return m, nil
}
func cloneMap(m map[string]any) map[string]any {
	b, _ := json.Marshal(m)
	var out map[string]any
	_ = json.Unmarshal(b, &out)
	return out
}
func mergeNative(target string, base map[string]any, g *generator.Result, o Options, keepRules bool) ([]byte, error) {
	base = cloneMap(base)
	generated, err := decodeBase(g.Output, target)
	if err != nil {
		return nil, err
	}
	if target == "clash" {
		base["proxies"] = generated["proxies"]
		// Native group properties (lazy, strategy, icon...) survive membership updates.
		base["proxy-groups"] = mergeGroups(base["proxy-groups"], generated["proxy-groups"], "name")
		if keepRules && base["rules"] != nil {
			rules := stringsList(base["rules"])
			for i, s := range rules {
				p := splitRule(s)
				idx := 2
				if len(p) > 0 && (p[0] == "MATCH" || p[0] == "FINAL") {
					idx = 1
				}
				if len(p) > idx && p[0] != "SUB-RULE" {
					if n, ok := g.Renames[p[idx]]; ok {
						if n == "" {
							n = "DIRECT"
						}
						p[idx] = n
						rules[i] = strings.Join(p, ",")
					}
				}
			}
			// Explicit rules go before original MATCH; defaults are not injected.
			extra := stringsList(generated["rules"])
			var prefix, final []string
			for _, s := range extra {
				if strings.HasPrefix(s, "MATCH,") || strings.HasPrefix(s, "FINAL,") {
					final = append(final, s)
				} else {
					prefix = append(prefix, s)
				}
			}
			if len(final) > 0 {
				var retained []string
				for _, s := range rules {
					if !strings.HasPrefix(s, "MATCH,") && !strings.HasPrefix(s, "FINAL,") {
						retained = append(retained, s)
					}
				}
				rules = append(retained, final...)
			}
			rules = append(prefix, rules...)
			base["rules"] = rules
		} else if len(o.Rules) > 0 || o.External != "" || !keepRules || base["rules"] == nil {
			base["rules"] = generated["rules"]
		}
		if p := generated["rule-providers"]; p != nil {
			base["rule-providers"] = p
		}
		return yaml.Marshal(base)
	}
	// Keep native direct/dns/block outbounds, and any unsupported native type, for
	// same-format output. Generated selector membership and transformed nodes win.
	native, _ := base["outbounds"].([]any)
	newOB, _ := generated["outbounds"].([]any)
	emitted := map[string]bool{}
	for _, v := range newOB {
		m, _ := v.(map[string]any)
		tag, _ := m["tag"].(string)
		emitted[tag] = true
	}
	for _, v := range native {
		m, _ := v.(map[string]any)
		tag, _ := m["tag"].(string)
		typ, _ := m["type"].(string)
		if typ == "selector" || typ == "urltest" {
			continue
		}
		if _, node := g.Renames[tag]; node {
			continue
		}
		if !emitted[tag] {
			newOB = append(newOB, v)
			emitted[tag] = true
		}
	}
	base["outbounds"] = mergeGroups(native, newOB, "tag")
	if keepRules && base["route"] != nil {
		route, _ := base["route"].(map[string]any)
		if route == nil {
			route = map[string]any{}
			base["route"] = route
		}
		rewriteRefs(route, g.Renames)
		if len(o.Rules) > 0 || o.Tailscale.Or(false) {
			gr, _ := generated["route"].(map[string]any)
			extra, _ := gr["rules"].([]any)
			if len(extra) > 0 {
				extra = extra[1:]
			} // generated DNS action already in native config
			old, _ := route["rules"].([]any)
			route["rules"] = append(extra, old...)
			for _, s := range o.Rules {
				if strings.HasPrefix(s, "MATCH,") || strings.HasPrefix(s, "FINAL,") {
					route["final"] = gr["final"]
				}
			}
		}
	} else {
		base["route"] = generated["route"]
	}
	// Native DNS rules and detours may also refer to renamed nodes.
	rewriteRefs(base["dns"], g.Renames)
	rewriteRefs(base["outbounds"], g.Renames)
	return json.MarshalIndent(base, "", "  ")
}
func mergeGroups(old, new any, key string) []any {
	prior := map[string]map[string]any{}
	a, _ := old.([]any)
	for _, v := range a {
		m, _ := v.(map[string]any)
		s, _ := m[key].(string)
		prior[s] = m
	}
	a, _ = new.([]any)
	var out []any
	for _, v := range a {
		m, _ := v.(map[string]any)
		s, _ := m[key].(string)
		if p := prior[s]; p != nil {
			merged := cloneMap(p)
			for k, v := range m {
				merged[k] = v
			}
			out = append(out, merged)
		} else {
			out = append(out, v)
		}
	}
	return out
}
func rewriteRefs(v any, names map[string]string) {
	switch x := v.(type) {
	case map[string]any:
		for k, v := range x {
			if k == "outbound" || k == "detour" || k == "final" || k == "default" {
				if s, ok := v.(string); ok {
					if n, found := names[s]; found {
						if n == "" {
							n = "direct"
						}
						x[k] = n
					}
				}
			} else {
				rewriteRefs(v, names)
			}
		}
	case []any:
		for _, v := range x {
			rewriteRefs(v, names)
		}
	}
}
func validateReferences(target string, data []byte) error {
	m, err := decodeBase(data, target)
	if err != nil {
		return err
	}
	valid := map[string]bool{}
	if target == "clash" {
		for _, s := range []string{"DIRECT", "REJECT", "REJECT-DROP", "PASS", "COMPATIBLE", "GLOBAL"} {
			valid[s] = true
		}
		for _, key := range []string{"proxies", "proxy-groups"} {
			a, _ := m[key].([]any)
			for _, v := range a {
				p, _ := v.(map[string]any)
				s, _ := p["name"].(string)
				if valid[s] {
					return fmt.Errorf("duplicate or reserved proxy/group name %q", s)
				}
				valid[s] = true
			}
		}
		a, _ := m["proxy-groups"].([]any)
		for _, v := range a {
			g, _ := v.(map[string]any)
			for _, s := range stringsList(g["proxies"]) {
				if !valid[s] {
					return fmt.Errorf("group refers to missing proxy %q", s)
				}
			}
		}
		for _, s := range stringsList(m["rules"]) {
			p := splitRule(s)
			idx := 2
			if p[0] == "MATCH" || p[0] == "FINAL" {
				idx = 1
			}
			if len(p) > 0 && p[0] == "SUB-RULE" {
				sub, _ := m["sub-rules"].(map[string]any)
				if len(p) <= idx || sub[p[idx]] == nil {
					return fmt.Errorf("missing sub-rule reference")
				}
				continue
			}
			if len(p) > idx && !valid[p[idx]] {
				return fmt.Errorf("rule refers to missing target %q", p[idx])
			}
		}
	} else {
		for _, key := range []string{"outbounds", "endpoints"} {
			a, _ := m[key].([]any)
			for _, v := range a {
				ob, _ := v.(map[string]any)
				s, _ := ob["tag"].(string)
				if s == "" {
					continue
				}
				if valid[s] {
					return fmt.Errorf("duplicate outbound tag %q", s)
				}
				valid[s] = true
			}
		}
		a, _ := m["outbounds"].([]any)
		for _, v := range a {
			ob, _ := v.(map[string]any)
			for _, s := range stringsList(ob["outbounds"]) {
				if !valid[s] {
					return fmt.Errorf("selector refers to missing outbound %q", s)
				}
			}
		}
		route, _ := m["route"].(map[string]any)
		if s, ok := route["final"].(string); ok && !valid[s] {
			return fmt.Errorf("route.final refers to missing outbound %q", s)
		}
		var check func(any) error
		check = func(v any) error {
			switch x := v.(type) {
			case map[string]any:
				if s, ok := x["outbound"].(string); ok && !valid[s] {
					return fmt.Errorf("route refers to missing outbound %q", s)
				}
				for _, v := range x {
					if e := check(v); e != nil {
						return e
					}
				}
			case []any:
				for _, v := range x {
					if e := check(v); e != nil {
						return e
					}
				}
			}
			return nil
		}
		return check(route["rules"])
	}
	return nil
}

func hasNativeRules(d *parser.Document) bool {
	if d.Type == "clash" {
		return d.Base["rules"] != nil
	}
	return d.Base["route"] != nil
}

// Split only at top-level commas; logical Clash rules contain nested tuples.
func splitRule(s string) []string {
	var out []string
	depth, start := 0, 0
	for i, r := range s {
		switch r {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				out = append(out, strings.TrimSpace(s[start:i]))
				start = i + 1
			}
		}
	}
	return append(out, strings.TrimSpace(s[start:]))
}

func nativeBuiltin(d *parser.Document, name string) string {
	items, _ := d.Base["outbounds"].([]any)
	for _, v := range items {
		m, _ := v.(map[string]any)
		if m["tag"] == name {
			switch m["type"] {
			case "direct":
				return "DIRECT"
			case "block":
				return "REJECT"
			}
		}
	}
	return builtin(name)
}
