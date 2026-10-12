package app

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"subconfig/internal/ratelimit"
)

func DetectTarget(r *http.Request) string {
	ua := strings.ToLower(r.UserAgent())
	for _, v := range []struct {
		parts  []string
		target string
	}{{[]string{"sing-box", "singbox", "nekobox", "sfa/", "sfi/"}, "singbox"}, {[]string{"clash", "mihomo", "stash"}, "clash"}, {[]string{"shadowrocket"}, "shadowrocket"}, {[]string{"quantumult", "quanx"}, "quanx"}, {[]string{"surge"}, "surge"}, {[]string{"loon"}, "loon"}, {[]string{"v2rayn", "v2rayng", "hiddify"}, "mixed"}} {
		for _, p := range v.parts {
			if strings.Contains(ua, p) {
				return v.target
			}
		}
	}
	if strings.Contains(strings.ToLower(r.Header.Get("Accept")), "application/json") {
		return "singbox"
	}
	return "auto"
}
func authorized(r *http.Request, secret string) bool {
	if secret == "" {
		return true
	}
	token := r.Header.Get("X-Secret")
	if token == "" {
		token = r.Header.Get("X-API-Key")
	}
	if auth := r.Header.Get("Authorization"); auth != "" {
		parts := strings.Fields(auth)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			return false
		}
		token = parts[1]
	}
	q, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return false
	}
	for _, k := range []string{"s", "secret", "srcret"} {
		for _, v := range q[k] {
			if token != "" && token != v {
				return false
			}
			token = v
		}
	}
	a, b := sha256.Sum256([]byte(token)), sha256.Sum256([]byte(secret))
	return subtle.ConstantTimeCompare(a[:], b[:]) == 1
}
func CloneOptions(o Options) Options {
	for _, p := range []*List{&o.Files, &o.URLs, &o.Insert, &o.Include, &o.Exclude, &o.Rename, &o.Groups, &o.Rules, &o.Headers} {
		*p = append(List(nil), (*p)...)
	}
	return o
}
func QueryOptions(base Options, q url.Values) (Options, error) {
	o := CloneOptions(base)
	f := Flags(&o, io.Discard)
	blocked := map[string]bool{}
	for _, n := range []string{"c", "o", "l", "p", "H", "s", "secret-file", "allow-url", "public", "quiet", "version", "userinfo-output", "rate-limit", "burst", "max-bytes", "request-timeout", "cache-ttl"} {
		if fl := f.Lookup(n); fl != nil {
			ptr := reflect.ValueOf(fl.Value).Pointer()
			f.VisitAll(func(v *flag.Flag) {
				if reflect.ValueOf(v.Value).Pointer() == ptr {
					blocked[v.Name] = true
				}
			})
		}
	}
	seen := map[uintptr]string{}
	var args []string
	keys := make([]string, 0, len(q))
	for k := range q {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if k == "secret" || k == "srcret" || k == "s" {
			continue
		}
		name := strings.ReplaceAll(k, "_", "-")
		if f.Lookup(k) != nil {
			name = k
		}
		fl := f.Lookup(name)
		if fl == nil {
			return o, fmt.Errorf("unknown query parameter %q", k)
		}
		if blocked[name] {
			return o, fmt.Errorf("parameter %q is only available at startup", k)
		}
		vals := q[k]
		ptr := reflect.ValueOf(fl.Value).Pointer()
		if list, ok := listValue(fl.Value); ok {
			if _, ok := seen[ptr]; !ok {
				*list = nil
			}
			seen[ptr] = name
		} else {
			if len(vals) != 1 {
				return o, fmt.Errorf("parameter %q must appear once", k)
			}
			if _, ok := seen[ptr]; ok {
				return o, fmt.Errorf("duplicate aliases for parameter %q", k)
			}
			seen[ptr] = name
		}
		for _, v := range vals {
			if _, ok := fl.Value.(*Tri); ok {
				if v == "" {
					v = "true"
				}
				b, e := parseBool(v)
				if e != nil {
					return o, e
				}
				v = strconv.FormatBool(b)
			}
			args = append(args, "--"+name+"="+v)
		}
	}
	if err := f.Parse(args); err != nil {
		return o, fmt.Errorf("invalid request parameters")
	}
	allow := base.AllowURL.Or(len(base.Files)+len(base.URLs) == 0)
	for _, n := range []string{"u", "insert-url", "e", "b"} {
		fl := f.Lookup(n)
		if _, changed := seen[reflect.ValueOf(fl.Value).Pointer()]; !changed {
			continue
		}
		if !allow {
			return o, fmt.Errorf("URL overrides are disabled; start with --allow-url")
		}
		var vals []string
		switch n {
		case "u":
			vals = o.URLs
			o.Files = nil
		case "insert-url":
			vals = o.Insert
		case "e":
			vals = []string{o.External}
		case "b":
			vals = []string{o.Template}
		}
		for _, v := range vals {
			if v == "" {
				continue
			}
			u, err := url.Parse(v)
			if err != nil || !(u.Scheme == "http" || u.Scheme == "https") {
				if n == "u" && isLink(v) {
					continue
				}
				return o, fmt.Errorf("request sources must be HTTP(S) URLs%s", map[bool]string{true: " or share links", false: ""}[n == "u"])
			}
		}
	}
	if _, ok := seen[reflect.ValueOf(f.Lookup("used").Value).Pointer()]; ok {
		o.Upload = ""
		o.Download = ""
	} else {
		for _, n := range []string{"upload", "download"} {
			if _, ok := seen[reflect.ValueOf(f.Lookup(n).Value).Pointer()]; ok {
				o.Used = ""
			}
		}
	}
	return o, Validate(o)
}
func isLink(s string) bool {
	u, e := url.Parse(s)
	if e != nil {
		return false
	}
	switch strings.ToLower(u.Scheme) {
	case "ss", "ssr", "vmess", "vless", "trojan", "hysteria", "hysteria2", "hy", "hy2", "tuic", "anytls", "socks", "socks5", "socks4", "wireguard", "wg":
		return true
	}
	return false
}

func Handler(e *Engine, base Options, version string, log io.Writer) http.Handler {
	limiter := ratelimit.New(base.RPM, base.Burst)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if r.URL.Path == "/healthz" {
			w.Header().Set("Content-Type", "application/json")
			if r.Method != http.MethodHead {
				io.WriteString(w, "{\"status\":\"ok\"}\n")
			}
			return
		}
		if !authorized(r, base.Secret) {
			w.Header().Set("WWW-Authenticate", `Bearer realm="subconfig"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if r.URL.Path == "/version" {
			w.Header().Set("Content-Type", "text/plain")
			if r.Method != http.MethodHead {
				fmt.Fprintln(w, "subconfig", version)
			}
			return
		}
		if r.URL.Path == "/formats" {
			w.Header().Set("Content-Type", "application/json")
			if r.Method != http.MethodHead {
				io.WriteString(w, `{"input":["auto","clash","sing-box","mixed"],"output":["clash","sing-box","surge","shadowrocket","quanx","loon","mixed","raw"]}`)
			}
			return
		}
		if r.URL.Path != "/" && r.URL.Path != "/sub" && r.URL.Path != "/convert" && r.URL.Path != "/flushcache" {
			http.NotFound(w, r)
			return
		}
		ip, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			ip = r.RemoteAddr
		}
		if base.RPM > 0 && !limiter.Allow(ip) {
			w.Header().Set("Retry-After", "60")
			http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
			return
		}
		if r.URL.Path == "/flushcache" {
			e.Cache.Flush()
			if r.Method != http.MethodHead {
				io.WriteString(w, "cache cleared\n")
			}
			return
		}
		q, err := url.ParseQuery(r.URL.RawQuery)
		if err != nil {
			http.Error(w, "invalid query encoding", http.StatusBadRequest)
			return
		}
		o, err := QueryOptions(base, q)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		t, _ := NormalizeTarget(o.OutputType)
		if t == "auto" {
			o.OutputType = DetectTarget(r)
		}
		result, err := e.Convert(r.Context(), o)
		if err != nil {
			http.Error(w, err.Error(), http.StatusUnprocessableEntity)
			if !o.Quiet.Or(false) {
				fmt.Fprintln(log, "conversion failed")
			}
			return
		}
		w.Header().Set("Content-Type", result.ContentType)
		w.Header().Set("X-Output-Type", result.Target)
		w.Header().Set("Vary", "User-Agent, Accept")
		w.Header().Set("Profile-Update-Interval", strconv.Itoa(o.Interval))
		if result.Userinfo != "" {
			w.Header().Set("Subscription-Userinfo", result.Userinfo)
		}
		if o.Filename != "" {
			w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": o.Filename}))
		}
		if o.Title != "" {
			w.Header().Set("Profile-Title", "base64:"+base64.StdEncoding.EncodeToString([]byte(o.Title)))
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(result.Data)))
		if r.Method != http.MethodHead {
			_, _ = w.Write(result.Data)
		}
		Report(log, o, result)
	})
}
func Report(w io.Writer, o Options, r *Result) {
	if o.Quiet.Or(false) {
		return
	}
	fmt.Fprintf(w, "converted %s: %d nodes, %d duplicates removed, %d warnings\n", r.Target, r.Diagnostics.NodeCount-len(r.Diagnostics.SkippedNodes), r.Diagnostics.Duplicates, len(r.Warnings))
	for _, s := range r.Warnings {
		if strings.Contains(s, "://") {
			s = "unparsed or unsupported source entry (content omitted)"
		}
		fmt.Fprintln(w, "warning:", s)
	}
}
func WriteMetadata(w io.Writer, r *Result) error {
	return json.NewEncoder(w).Encode(map[string]any{"target": r.Target, "subscription_userinfo": r.Userinfo, "nodes": r.Diagnostics.NodeCount - len(r.Diagnostics.SkippedNodes), "warning_count": len(r.Warnings)})
}
