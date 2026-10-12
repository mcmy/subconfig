package app

import (
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"
	"subconfig/internal/parser"
	"time"
)

type List []string

func (v *List) String() string     { return strings.Join(*v, "|") }
func (v *List) Set(s string) error { *v = append(*v, s); return nil }

// Pipe separation is for source URLs only. Regexes, filenames, group syntax,
// rules and HTTP header values must retain literal pipe characters.
type PipeList List

func (v *PipeList) String() string { return strings.Join(*v, "|") }
func (v *PipeList) Set(s string) error {
	for _, part := range strings.Split(s, "|") {
		if part = strings.TrimSpace(part); part != "" {
			*v = append(*v, part)
		}
	}
	return nil
}
func listValue(v flag.Value) (*List, bool) {
	switch v := v.(type) {
	case *List:
		return v, true
	case *PipeList:
		return (*List)(v), true
	}
	return nil, false
}

type Tri struct {
	Specified bool
	Value     bool
}

func (v *Tri) String() string {
	if !v.Specified {
		return "auto"
	}
	return strconv.FormatBool(v.Value)
}
func (v *Tri) IsBoolFlag() bool { return true }
func (v *Tri) Set(s string) error {
	b, e := parseBool(s)
	if e == nil {
		v.Specified = true
		v.Value = b
	}
	return e
}
func parseBool(s string) (bool, error) {
	switch strings.ToLower(s) {
	case "1", "true", "yes", "on":
		return true, nil
	case "0", "false", "no", "off":
		return false, nil
	}
	return false, fmt.Errorf("invalid boolean %q", s)
}
func (v Tri) Or(b bool) bool {
	if v.Specified {
		return v.Value
	}
	return b
}

type Options struct {
	Files, URLs, Insert, Include, Exclude, Rename, Groups, Rules, Headers                                                                                                                                         List
	InputType, OutputType, Output, Listen, Port, Host, Secret, SecretFile, External, Template, Proxy, UA, FlareSolverr, Filename, Title, Userinfo, UserinfoOutput, Upload, Download, Used, Total, Expire          string
	Sort, Dedup, FDN, ListOnly, AppendType, Tailscale, UDP, TFO, SCV, Expand, Emoji, AddEmoji, RemoveEmoji, InsertEnabled, InsertPrepend, Preserve, Strict, NoCache, FlushCache, AllowURL, Public, Quiet, Version Tri
	Timeout, RequestTimeout, CacheTTL                                                                                                                                                                             time.Duration
	MaxBytes                                                                                                                                                                                                      int64
	Interval, RPM, Burst                                                                                                                                                                                          int
}

func Defaults() Options {
	return Options{InputType: "auto", OutputType: "auto", Host: "0.0.0.0", Timeout: 30 * time.Second, RequestTimeout: 2 * time.Minute, CacheTTL: 5 * time.Minute, MaxBytes: 16 << 20, Interval: 24, RPM: 60, Burst: 10}
}

func Flags(o *Options, w io.Writer) *flag.FlagSet {
	f := flag.NewFlagSet("subconfig", flag.ContinueOnError)
	f.SetOutput(w)
	f.Usage = func() {
		fmt.Fprint(w, "Usage:\n  subconfig -c config.yaml -t sing-box [-o output.json]\n  subconfig -u URL -t clash\n  subconfig -c config.json -p 8080 --secret KEY\n  subconfig serve [flags]\n\nAliases accept one or two leading dashes; booleans accept --flag=false or --flag false.\n")
		f.PrintDefaults()
	}
	stringFlag := func(p *string, names, help string) {
		for _, n := range strings.Split(names, ",") {
			f.StringVar(p, n, *p, help)
		}
	}
	listFlag := func(p *List, names, help string) {
		for _, n := range strings.Split(names, ",") {
			f.Var(p, n, help+" (repeatable)")
		}
	}
	boolFlag := func(p *Tri, names, help string) {
		for _, n := range strings.Split(names, ",") {
			f.Var(p, n, help+" (true/false/1/0/yes/no)")
		}
	}
	listFlag(&o.Files, "c,config,input,file", "Input config file; - reads stdin")
	sourceFlag := func(p *List, names, help string) {
		for _, n := range strings.Split(names, ",") {
			f.Var((*PipeList)(p), n, help+" (repeatable or | separated)")
		}
	}
	sourceFlag(&o.URLs, "u,url,input-url", "Input subscription URL or share link")
	stringFlag(&o.InputType, "i,in-type,input-type", "Input format: auto/clash/sing-box/mixed")
	stringFlag(&o.OutputType, "t,out-type,output-type,target", "Output: auto/clash/sing-box/surge/shadowrocket/quanx/loon/mixed/raw")
	stringFlag(&o.Output, "o,output,out", "Output file; - or omitted writes stdout")
	stringFlag(&o.Listen, "l,listen", "HTTP listen address, e.g. 127.0.0.1:8080")
	stringFlag(&o.Port, "p,port", "HTTP listen port")
	stringFlag(&o.Host, "H,host", "HTTP listen host (default 0.0.0.0)")
	stringFlag(&o.Secret, "s,secret,srcret", "HTTP secret; also SUBCONFIG_SECRET")
	stringFlag(&o.SecretFile, "secret-file", "Read HTTP secret from file")
	stringFlag(&o.External, "e,external,ext-config", "External subconverter INI file or URL")
	stringFlag(&o.Template, "b,template,base", "Target-native config template file or URL")
	stringFlag(&o.Proxy, "x,proxy", "Upstream HTTP/SOCKS proxy")
	stringFlag(&o.UA, "a,ua,user-agent", "Upstream User-Agent")
	stringFlag(&o.FlareSolverr, "flaresolverr", "FlareSolverr endpoint")
	listFlag(&o.Headers, "header", "Upstream HTTP header Name: Value")
	sourceFlag(&o.Insert, "insert-url", "Additional node source file, URL or share link")
	boolFlag(&o.InsertEnabled, "insert", "Enable insert sources (default true)")
	boolFlag(&o.InsertPrepend, "insert-prepend", "Prepend inserted nodes (default true)")
	listFlag(&o.Include, "include", "Include node names matching regex")
	listFlag(&o.Exclude, "exclude", "Exclude node names matching regex")
	listFlag(&o.Rename, "r,rename", "Rename regex@replacement; supports $1 and \\1")
	listFlag(&o.Groups, "g,group", "Group in subconverter backtick syntax")
	listFlag(&o.Rules, "rule", "Routing rule in Clash syntax, e.g. DOMAIN,x.com,DIRECT")
	boolFlag(&o.Sort, "S,sort", "Sort nodes by name")
	boolFlag(&o.Dedup, "d,dedup", "Deduplicate connection-identical nodes")
	boolFlag(&o.FDN, "fdn,filter-deprecated", "Drop deprecated nodes")
	boolFlag(&o.ListOnly, "L,list,list-only", "Only proxies/outbounds; supported for Clash and sing-box")
	boolFlag(&o.AppendType, "append-type,append_type", "Prepend protocol type to names")
	boolFlag(&o.Tailscale, "tailscale", "Add Tailscale direct routing")
	boolFlag(&o.UDP, "udp", "Force UDP")
	boolFlag(&o.TFO, "tfo", "Enable TCP Fast Open")
	boolFlag(&o.SCV, "scv,skip-cert-verify", "Disable node TLS verification")
	boolFlag(&o.Expand, "expand", "Inline rules; false emits Clash rule-providers")
	boolFlag(&o.Emoji, "emoji", "Normalize emoji; false strips emoji")
	boolFlag(&o.AddEmoji, "add-emoji,add_emoji", "Add country emoji (default false)")
	boolFlag(&o.RemoveEmoji, "remove-emoji,remove_emoji", "Remove existing emoji (default false)")
	boolFlag(&o.Preserve, "preserve", "Preserve native settings for same-format output (default true)")
	boolFlag(&o.Strict, "strict", "Fail on unsupported nodes/rules instead of partial output")
	boolFlag(&o.NoCache, "nocache,no-cache", "Bypass upstream cache")
	boolFlag(&o.FlushCache, "flushcache,flush-cache", "Clear upstream cache before converting")
	boolFlag(&o.AllowURL, "allow-url", "Allow authenticated HTTP URL source overrides")
	boolFlag(&o.Public, "public", "Allow HTTP service without a secret")
	boolFlag(&o.Quiet, "q,quiet", "Suppress diagnostics on stderr")
	boolFlag(&o.Version, "v,version", "Show version")
	stringFlag(&o.Filename, "n,filename", "HTTP download filename")
	stringFlag(&o.Title, "title", "HTTP profile title")
	stringFlag(&o.Userinfo, "userinfo", "Subscription-Userinfo header (manual override)")
	stringFlag(&o.UserinfoOutput, "userinfo-output", "CLI metadata sidecar JSON file")
	stringFlag(&o.Upload, "upload", "Uploaded bytes; supports KiB/MiB/GiB, KB/MB/GB")
	stringFlag(&o.Download, "download", "Downloaded bytes")
	stringFlag(&o.Used, "used", "Total used bytes (sets upload=0, download=used)")
	stringFlag(&o.Total, "total", "Traffic quota bytes")
	stringFlag(&o.Expire, "expire", "Expiry: Unix seconds, YYYY-MM-DD, RFC3339 or 0")
	f.DurationVar(&o.Timeout, "timeout", o.Timeout, "Per-upstream fetch timeout")
	f.DurationVar(&o.RequestTimeout, "request-timeout", o.RequestTimeout, "Total conversion timeout")
	f.DurationVar(&o.CacheTTL, "cache-ttl", o.CacheTTL, "Upstream TTL; negative disables")
	f.Int64Var(&o.MaxBytes, "max-bytes", o.MaxBytes, "Maximum bytes per input or remote resource")
	f.IntVar(&o.Interval, "interval", o.Interval, "Client refresh interval in hours")
	f.IntVar(&o.RPM, "rate-limit", o.RPM, "HTTP requests per minute per IP; 0 disables")
	f.IntVar(&o.Burst, "burst", o.Burst, "HTTP rate limit burst")
	return f
}

// NormalizeArgs permits --sort false and --sort=0, while preserving flag syntax.
func NormalizeArgs(args []string, f *flag.FlagSet) ([]string, error) {
	var out []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			out = append(out, args[i:]...)
			break
		}
		if strings.HasPrefix(a, "-") {
			name, val, eq := strings.Cut(strings.TrimLeft(a, "-"), "=")
			if fl := f.Lookup(name); fl != nil {
				if _, ok := fl.Value.(*Tri); ok {
					if !eq && i+1 < len(args) {
						if _, err := parseBool(args[i+1]); err == nil {
							val = args[i+1]
							eq = true
							i++
						}
					}
					if eq {
						b, err := parseBool(val)
						if err != nil {
							return nil, err
						}
						a = "--" + name + "=" + strconv.FormatBool(b)
					}
				}
			}
		}
		out = append(out, a)
	}
	return out, nil
}
func ParseOptions(args []string, w io.Writer) (Options, error) {
	o := Defaults()
	f := Flags(&o, w)
	args, err := NormalizeArgs(args, f)
	if err != nil {
		return o, err
	}
	if err = f.Parse(args); err != nil {
		return o, err
	}
	if f.NArg() > 0 {
		return o, fmt.Errorf("unexpected arguments: %s", strings.Join(f.Args(), " "))
	}
	return o, Validate(o)
}
func Validate(o Options) error {
	if o.Secret != "" && strings.TrimSpace(o.Secret) == "" {
		return fmt.Errorf("secret cannot be whitespace only")
	}
	if typ := parser.NormalizeInput(o.InputType); typ != "auto" && typ != "clash" && typ != "singbox" && typ != "mixed" {
		return fmt.Errorf("unsupported input type %q", o.InputType)
	}
	if o.Timeout <= 0 || o.RequestTimeout <= 0 || o.MaxBytes <= 0 || o.MaxBytes == int64(^uint64(0)>>1) || o.Interval <= 0 || o.RPM < 0 || o.Burst < 1 {
		return fmt.Errorf("timeouts, max-bytes, interval and burst must be positive; rate-limit must be nonnegative")
	}
	if o.Used != "" && (o.Upload != "" || o.Download != "") {
		return fmt.Errorf("--used cannot be combined with --upload or --download")
	}
	if o.Listen != "" && o.Port != "" {
		return fmt.Errorf("choose either --listen or --port")
	}
	if o.Port != "" {
		p, e := strconv.Atoi(o.Port)
		if e != nil || p < 1 || p > 65535 {
			return fmt.Errorf("port must be 1..65535")
		}
	}
	if o.Output != "" && (o.Listen != "" || o.Port != "") {
		return fmt.Errorf("HTTP service and output file are mutually exclusive")
	}
	if _, err := NormalizeTarget(o.OutputType); err != nil {
		return err
	}
	_, err := Userinfo(o, "")
	return err
}
func NormalizeTarget(t string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(t)) {
	case "", "auto":
		return "auto", nil
	case "clash", "clashmeta", "clash.meta", "clashr", "mihomo":
		return "clash", nil
	case "singbox", "sing-box", "sing_box":
		return "singbox", nil
	case "surge", "loon", "shadowrocket", "quanx":
		return strings.ToLower(t), nil
	case "shadow-rocket":
		return "shadowrocket", nil
	case "quantumultx", "quantumult-x", "quantumult_x":
		return "quanx", nil
	case "v2ray", "v2rayn", "mixed", "base64", "ss":
		return "mixed", nil
	case "raw", "uri", "links":
		return "raw", nil
	default:
		return "", fmt.Errorf("unsupported output type %q", t)
	}
}
