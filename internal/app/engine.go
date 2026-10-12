package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"subconfig/internal/emoji"
	"subconfig/internal/extconfig"
	"subconfig/internal/fetch"
	"subconfig/internal/generator"
	"subconfig/internal/parser"
	"subconfig/internal/proxy"
)

type Engine struct {
	Cache *fetch.Cache
	Stdin []byte
}
type Result struct {
	Data                          []byte
	Target, ContentType, Userinfo string
	Diagnostics                   *generator.Result
	Warnings                      []string
}

func ReadLimited(r io.Reader, max int64) ([]byte, error) {
	b, e := io.ReadAll(io.LimitReader(r, max+1))
	if e != nil {
		return nil, e
	}
	if int64(len(b)) > max {
		return nil, fmt.Errorf("input exceeds max-bytes (%d)", max)
	}
	return b, nil
}
func NewEngine(o Options, stdin io.Reader) (*Engine, error) {
	e := &Engine{Cache: fetch.NewCache(o.CacheTTL)}
	seen := false
	for _, f := range o.Files {
		if f == "-" {
			if seen {
				return nil, fmt.Errorf("stdin may be specified once")
			}
			seen = true
			var err error
			e.Stdin, err = ReadLimited(stdin, o.MaxBytes)
			if err != nil {
				return nil, err
			}
		}
	}
	return e, nil
}

type resources struct {
	e      *Engine
	o      Options
	c      *fetch.Client
	mu     sync.Mutex
	errors int
}

func (r *resources) Get(ctx context.Context, s string) ([]byte, error) {
	b, _, e := r.read(ctx, s)
	if e != nil {
		r.mu.Lock()
		r.errors++
		r.mu.Unlock()
	}
	return b, e
}
func (r *resources) read(ctx context.Context, s string) ([]byte, http.Header, error) {
	if s == "-" {
		return r.e.Stdin, nil, nil
	}
	if parser.IsShareLink(s) {
		return []byte(s), nil, nil
	}
	if u, err := url.Parse(s); err == nil && (u.Scheme == "http" || u.Scheme == "https") {
		return r.c.GetWithMeta(ctx, s)
	}
	f, err := os.Open(s)
	if err != nil {
		return nil, nil, fmt.Errorf("read local input: %w", err)
	}
	defer f.Close()
	b, err := ReadLimited(f, r.o.MaxBytes)
	return b, nil, err
}
func (e *Engine) Convert(ctx context.Context, o Options) (*Result, error) {
	ctx, cancel := context.WithTimeout(ctx, o.RequestTimeout)
	defer cancel()
	headers := http.Header{}
	for _, h := range o.Headers {
		k, v, ok := strings.Cut(h, ":")
		if !ok || strings.TrimSpace(k) == "" {
			return nil, fmt.Errorf("invalid --header, expected Name: Value")
		}
		headers.Add(strings.TrimSpace(k), strings.TrimSpace(v))
	}
	cacheTTL := o.CacheTTL
	if o.NoCache.Or(false) {
		cacheTTL = -1
	}
	if o.FlushCache.Or(false) {
		e.Cache.Flush()
	}
	client, err := fetch.New(fetch.Options{UserAgent: o.UA, Headers: headers, MaxBytes: o.MaxBytes, Proxy: o.Proxy, FlareSolverrURL: o.FlareSolverr, Timeout: o.Timeout, CacheTTL: cacheTTL, Cache: e.Cache})
	if err != nil {
		return nil, fmt.Errorf("invalid upstream settings")
	}
	r := &resources{e: e, o: o, c: client}
	defer client.Close()
	result := &Result{}
	var nodes []*proxy.Proxy
	var primary *parser.Document
	var documents []*parser.Document
	sources := append(append([]string{}, o.Files...), o.URLs...)
	if len(sources) == 0 {
		return nil, fmt.Errorf("provide --config/-c or --url/-u")
	}
	for i, s := range sources {
		data, h, err := r.read(ctx, s)
		if err != nil {
			return nil, fmt.Errorf("input %d failed: %s", i+1, safeError(err))
		}
		d, err := parser.ParseDocument(data, s, o.InputType)
		if err != nil {
			return nil, fmt.Errorf("input %d: %w", i+1, err)
		}
		if i == 0 {
			primary = d
			if h != nil {
				result.Userinfo = h.Get("Subscription-Userinfo")
			}
		}
		documents = append(documents, d)
		nodes = append(nodes, d.Nodes...)
		result.Warnings = append(result.Warnings, safeSkipped(d)...)
		if i > 0 && d.Base != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("input %d: merged nodes; settings/groups/rules come from the first input", i+1))
		}
	}
	if o.InsertEnabled.Or(true) {
		var inserted []*proxy.Proxy
		for i, s := range o.Insert {
			data, _, err := r.read(ctx, s)
			if err != nil {
				return nil, fmt.Errorf("insert source %d failed", i+1)
			}
			d, err := parser.ParseDocument(data, s, "auto")
			if err != nil {
				return nil, err
			}
			documents = append(documents, d)
			inserted = append(inserted, d.Nodes...)
			result.Warnings = append(result.Warnings, safeSkipped(d)...)
		}
		if o.InsertPrepend.Or(true) {
			nodes = append(inserted, nodes...)
		} else {
			nodes = append(nodes, inserted...)
		}
	}
	for _, n := range nodes {
		for _, v := range []struct {
			t Tri
			k string
		}{{o.UDP, "udp"}, {o.TFO, "tfo"}, {o.SCV, "skip-cert-verify"}} {
			if v.t.Specified {
				n.Clash[v.k] = v.t.Value
			}
		}
	}
	if len(nodes) == 0 {
		return nil, fmt.Errorf("no usable proxy nodes; provider-only configs require resolved nodes")
	}
	target, err := NormalizeTarget(o.OutputType)
	if err != nil {
		return nil, err
	}
	if target == "auto" {
		switch strings.ToLower(filepath.Ext(o.Output)) {
		case ".json", ".jsonc":
			target = "singbox"
		case ".yaml", ".yml":
			target = "clash"
		default:
			target = primary.Type
			if target == "mixed" {
				target = "clash"
			}
		}
	}
	result.Target = target
	for _, d := range documents {
		if d.Type != target {
			result.Warnings = append(result.Warnings, d.Lossy...)
		}
	}
	if o.ListOnly.Or(false) && target != "clash" && target != "singbox" {
		return nil, fmt.Errorf("--list only supports clash and sing-box")
	}
	if o.Expand.Specified && !o.Expand.Value && target != "clash" {
		return nil, fmt.Errorf("--expand=false only supports clash")
	}
	cfg := &extconfig.Config{EnableRuleGenerator: true}
	if o.External != "" {
		data, err := r.Get(ctx, o.External)
		if err != nil {
			return nil, fmt.Errorf("external config fetch failed")
		}
		cfg = extconfig.Parse(data)
	} else {
		cfg.ProxyGroups = nativeGroups(primary, target)
		if target != primary.Type || !o.Preserve.Or(true) {
			cfg.Rulesets, result.Warnings = nativeRules(primary, result.Warnings)
		}
	}
	if len(cfg.ProxyGroups) == 0 {
		cfg.ProxyGroups = []extconfig.ProxyGroup{{Name: "PROXY", Type: "select", Selectors: []string{".*", "[]DIRECT"}}}
	}
	for _, g := range o.Groups {
		parsed := extconfig.Parse([]byte("custom_proxy_group=" + g))
		if len(parsed.ProxyGroups) == 0 {
			return nil, fmt.Errorf("invalid --group")
		}
		cfg.ProxyGroups = append(cfg.ProxyGroups, parsed.ProxyGroups...)
	}
	reserved := map[string]bool{"DIRECT": true, "REJECT": true, "REJECT-DROP": true, "PASS": true, "COMPATIBLE": true, "GLOBAL": true, "direct": true, "block": true, "dns-out": true}
	for _, g := range cfg.ProxyGroups {
		if reserved[g.Name] {
			return nil, fmt.Errorf("group name %q conflicts with builtin or another group", g.Name)
		}
		reserved[g.Name] = true
	}

	for _, pattern := range o.Include {
		re, err := regexp.Compile(pattern)
		if err != nil {
			return nil, fmt.Errorf("invalid include regex: %w", err)
		}
		cfg.IncludeRemarks = append(cfg.IncludeRemarks, re)
	}
	for _, pattern := range o.Exclude {
		re, err := regexp.Compile(pattern)
		if err != nil {
			return nil, fmt.Errorf("invalid exclude regex: %w", err)
		}
		cfg.ExcludeRemarks = append(cfg.ExcludeRemarks, re)
	}
	cfg.RenameRules = append(cfg.RenameRules, o.Rename...)
	for _, rule := range cfg.RenameRules {
		p, _, ok := strings.Cut(rule, "@")
		if !ok {
			return nil, fmt.Errorf("rename must be regex@replacement")
		}
		if _, e := regexp.Compile(p); e != nil {
			return nil, e
		}
	}
	for _, line := range o.Rules {
		rs, err := clashRule(line)
		if err != nil {
			return nil, err
		}
		cfg.Rulesets = append(cfg.Rulesets, rs)
	}
	if o.External == "" && !(primary.Type == target && o.Preserve.Or(true) && o.Template == "" && hasNativeRules(primary)) {
		hasFinal := false
		for _, r := range cfg.Rulesets {
			if r.Inline == "FINAL" || r.Inline == "MATCH" {
				hasFinal = true
			}
		}
		if !hasFinal {
			cfg.Rulesets = append(cfg.Rulesets, extconfig.Ruleset{Group: cfg.ProxyGroups[0].Name, Inline: "FINAL"})
		}
	}
	gen := generator.Options{Sort: o.Sort.Or(false), Dedup: o.Dedup.Or(false), FilterDeprecated: o.FDN.Or(false), ListOnly: o.ListOnly.Or(false), AppendType: o.AppendType.Or(false), Tailscale: o.Tailscale.Or(false), UDP: o.UDP.Or(false), TFO: o.TFO.Or(false), SkipCertVerify: o.SCV.Or(false), UseRuleProviders: !o.Expand.Or(true), RenameRules: generator.CompileRenameRules(cfg.RenameRules), AddEmoji: o.AddEmoji.Or(cfg.AddEmoji != nil && *cfg.AddEmoji), RemoveEmoji: o.RemoveEmoji.Or(cfg.RemoveOldEmoji != nil && *cfg.RemoveOldEmoji)}
	for name := range reserved {
		gen.ReservedNames = append(gen.ReservedNames, name)
	}
	if target == "singbox" && primary.Type == target && o.Preserve.Or(true) {
		parsed := map[string]bool{}
		for _, n := range primary.Nodes {
			parsed[n.Name] = true
		}
		a, _ := primary.Base["outbounds"].([]any)
		for _, v := range a {
			ob, _ := v.(map[string]any)
			tag, _ := ob["tag"].(string)
			typ, _ := ob["type"].(string)
			if typ != "selector" && typ != "urltest" && !parsed[tag] {
				gen.NativeOutbounds = append(gen.NativeOutbounds, ob)
			}
		}
	}

	if o.Emoji.Specified {
		gen.RemoveEmoji = true
		gen.AddEmoji = o.Emoji.Value
	}
	if gen.AddEmoji {
		gen.EmojiRules = emoji.Default()
		if len(cfg.EmojiRules) > 0 {
			gen.EmojiRules = emoji.ParseRules(cfg.EmojiRules)
		}
	}
	var generated *generator.Result
	switch target {
	case "clash":
		generated, err = generator.GenerateClash(ctx, nodes, cfg, r, gen)
	case "singbox":
		generated, err = generator.GenerateSingbox(ctx, nodes, cfg, r, gen)
	case "surge":
		generated, err = generator.GenerateSurge(ctx, nodes, cfg, r, gen)
	case "shadowrocket":
		generated, err = generator.GenerateShadowrocket(ctx, nodes, cfg, r, gen)
	case "loon":
		generated, err = generator.GenerateLoon(ctx, nodes, cfg, r, gen)
	case "quanx":
		generated, err = generator.GenerateQuanX(ctx, nodes, cfg, r, gen)
	case "mixed", "raw":
		generated, err = generator.GenerateV2ray(ctx, nodes, cfg, r, gen)
	}
	if err != nil {
		return nil, err
	}
	r.mu.Lock()
	resourceErrors := r.errors
	r.mu.Unlock()
	if resourceErrors > 0 {
		return nil, fmt.Errorf("%d external resources failed to load", resourceErrors)
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	if generated.NodeCount == 0 || len(generated.SkippedNodes) >= generated.NodeCount {
		return nil, fmt.Errorf("no nodes supported by output type %s", target)
	}
	result.Diagnostics = generated
	result.ContentType = generated.ContentType
	result.Data = generated.Output
	if target == "raw" {
		result.Data, err = base64.StdEncoding.DecodeString(string(result.Data))
		if err != nil {
			return nil, err
		}
	}
	if target == "clash" || target == "singbox" {
		var base map[string]any
		if o.Template != "" {
			data, err := r.Get(ctx, o.Template)
			if err != nil {
				return nil, fmt.Errorf("template fetch failed")
			}
			base, err = decodeBase(data, target)
			if err != nil {
				return nil, err
			}
		} else if primary.Type == target && o.Preserve.Or(true) {
			base = primary.Base
		}
		if base != nil && !gen.ListOnly {
			result.Data, err = mergeNative(target, base, generated, o, primary.Type == target && o.Template == "" && o.External == "")
			if err != nil {
				return nil, err
			}
		}
		if target == "singbox" && gen.ListOnly {
			var m map[string]any
			_ = json.Unmarshal(result.Data, &m)
			result.Data, err = json.MarshalIndent(map[string]any{"outbounds": m["outbounds"]}, "", "  ")
			if err != nil {
				return nil, err
			}
		}
		if err = validateReferences(target, result.Data); err != nil {
			return nil, err
		}
	}
	if target != primary.Type && primary.Base != nil {
		for _, k := range []string{"dns", "inbounds", "tun", "experimental", "proxy-providers", "rule-providers"} {
			if _, ok := primary.Base[k]; ok {
				result.Warnings = append(result.Warnings, k+": native settings are not translated across formats; use --template")
			}
		}
	}
	result.Warnings = append(result.Warnings, generated.SkippedNodes...)
	result.Warnings = append(result.Warnings, generated.SkippedRules...)
	if o.Strict.Or(false) && len(result.Warnings) > 0 {
		return nil, fmt.Errorf("strict conversion rejected %d unsupported or lossy entries: %s", len(result.Warnings), strings.Join(result.Warnings, "; "))
	}
	result.Userinfo, err = Userinfo(o, result.Userinfo)
	if err != nil {
		return nil, err
	}
	if len(result.Data) > 0 && result.Data[len(result.Data)-1] != '\n' {
		result.Data = append(result.Data, '\n')
	}
	return result, nil
}
func safeError(err error) string {
	if err == context.Canceled || err == context.DeadlineExceeded {
		return err.Error()
	}
	return "cannot read source (check path/URL, upstream status and connectivity)"
}

func safeSkipped(d *parser.Document) []string {
	if d.Type == "mixed" && len(d.Skipped) > 0 {
		return []string{fmt.Sprintf("%d unparsed subscription entries (content omitted)", len(d.Skipped))}
	}
	return d.Skipped
}
