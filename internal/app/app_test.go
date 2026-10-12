package app

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"gopkg.in/yaml.v3"
)

func example(t *testing.T, name string) string {
	t.Helper()
	p, e := filepath.Abs(filepath.Join("..", "..", "examples", name))
	if e != nil {
		t.Fatal(e)
	}
	return p
}
func convertTest(t *testing.T, o Options) *Result {
	t.Helper()
	e, err := NewEngine(o, strings.NewReader(""))
	if err != nil {
		t.Fatal(err)
	}
	r, err := e.Convert(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func object(t *testing.T, r *Result) map[string]any {
	t.Helper()
	m, e := decodeBase(r.Data, r.Target)
	if e != nil {
		t.Fatal(e)
	}
	return m
}
func TestAllOutputFormats(t *testing.T) {
	for _, input := range []string{"clash.yaml", "sing-box.json"} {
		for _, target := range []string{"clash", "sing-box", "surge", "shadowrocket", "quanx", "loon", "mixed", "raw"} {
			t.Run(input+"/"+target, func(t *testing.T) {
				o := Defaults()
				o.Files = List{example(t, input)}
				o.OutputType = target
				r := convertTest(t, o)
				if len(r.Data) == 0 {
					t.Fatal("empty output")
				}
				switch r.Target {
				case "clash", "singbox":
					if err := validateReferences(r.Target, r.Data); err != nil {
						t.Fatal(err)
					}
				case "mixed":
					b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(r.Data)))
					if err != nil || !bytes.Contains(b, []byte("ss://")) {
						t.Fatal("not a mixed subscription")
					}
				case "raw":
					if !bytes.Contains(r.Data, []byte("trojan://")) {
						t.Fatal("missing raw links")
					}
				}
			})
		}
	}
}
func TestSameFormatPreservesSettingsAndRenames(t *testing.T) {
	for _, input := range []string{"clash.yaml", "sing-box.json"} {
		t.Run(input, func(t *testing.T) {
			o := Defaults()
			o.Files = List{example(t, input)}
			o.Rename = List{"香港@HK"}
			r := convertTest(t, o)
			m := object(t, r)
			if !strings.Contains(string(r.Data), "HK SS") || strings.Contains(string(r.Data), "香港 SS") {
				t.Fatalf("old references survived: %s", r.Data)
			}
			if r.Target == "clash" {
				if number(m["mixed-port"]) != 7890 || m["allow-lan"] != false {
					t.Fatal("native settings lost")
				}
				rules := stringsList(m["rules"])
				if len(rules) != 3 || rules[2] != "MATCH,PROXY" {
					t.Fatal(rules)
				}
			} else {
				a := m["outbounds"].([]any)
				for _, v := range a {
					g := v.(map[string]any)
					if g["tag"] == "PROXY" && g["default"] != "HK SS" {
						t.Fatal("selector default not rewritten")
					}
				}
				if len(m["inbounds"].([]any)) != 1 {
					t.Fatal("native inbounds lost")
				}
			}
		})
	}
}
func TestFilterFixesMembershipAndRejectsEmpty(t *testing.T) {
	o := Defaults()
	o.Files = List{example(t, "clash.yaml")}
	o.Exclude = List{"日本"}
	r := convertTest(t, o)
	if strings.Contains(string(r.Data), "日本") {
		t.Fatal("filtered node reference retained")
	}
	o.Exclude = List{".*"}
	e, _ := NewEngine(o, strings.NewReader(""))
	if _, err := e.Convert(context.Background(), o); err == nil {
		t.Fatal("empty conversion should fail")
	}
}
func TestMergeSourcesAndDedup(t *testing.T) {
	o := Defaults()
	o.Files = List{example(t, "clash.yaml"), example(t, "clash.yaml")}
	o.Dedup = Tri{true, true}
	r := convertTest(t, o)
	if r.Diagnostics.Duplicates < 1 {
		t.Fatal("expected connection deduplication")
	}
	if r.Diagnostics.NodeCount != 2 {
		t.Fatalf("count=%d", r.Diagnostics.NodeCount)
	}
}
func TestStdinTemplateAndList(t *testing.T) {
	o := Defaults()
	o.Files = List{"-"}
	o.OutputType = "sing-box"
	o.Template = example(t, "sing-box-template.json")
	input, _ := os.ReadFile(example(t, "clash.yaml"))
	e, err := NewEngine(o, bytes.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	r, err := e.Convert(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	m := object(t, r)
	a := m["inbounds"].([]any)
	if number(a[0].(map[string]any)["listen_port"]) != 3080 {
		t.Fatal("template ignored")
	}
	o.ListOnly = Tri{true, true}
	r, err = e.Convert(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	if len(object(t, r)) != 1 {
		t.Fatal("list should only include outbounds")
	}
}
func TestExplicitOptionsAndINI(t *testing.T) {
	o := Defaults()
	o.Files = List{example(t, "clash.yaml")}
	o.External = example(t, "rules.ini")
	o.OutputType = "sing-box"
	o.SCV = Tri{true, true}
	o.TFO = Tri{true, true}
	o.Tailscale = Tri{true, true}
	r := convertTest(t, o)
	if !bytes.Contains(r.Data, []byte("100.64.0.0/10")) || !bytes.Contains(r.Data, []byte(`"insecure": true`)) {
		t.Fatal("options missing")
	}
}
func TestStrictReportsLoss(t *testing.T) {
	o := Defaults()
	o.Files = List{example(t, "clash.yaml")}
	o.OutputType = "sing-box"
	o.Strict = Tri{true, true}
	e, _ := NewEngine(o, strings.NewReader(""))
	if _, err := e.Convert(context.Background(), o); err == nil || !strings.Contains(err.Error(), "native settings") {
		t.Fatal(err)
	}
}
func TestUnitParsingAndHeaderOverrides(t *testing.T) {
	for s, want := range map[string]uint64{"0": 0, "1GiB": 1 << 30, "1.5MiB": 1572864, "2 GB": 2000000000, "18446744073709551615": ^uint64(0)} {
		n, e := Bytes(s)
		if e != nil || n != want {
			t.Fatalf("%s: %d %v", s, n, e)
		}
	}
	for _, s := range []string{"-1", "NaN", "1PiB", "0.1B", "18446744073709551616"} {
		if _, e := Bytes(s); e == nil {
			t.Fatalf("accepted %s", s)
		}
	}
	o := Defaults()
	o.Total = "2GiB"
	o.Used = "1MiB"
	o.Expire = "2030-01-01"
	h, e := Userinfo(o, "upload=100; download=200; total=300; expire=400")
	if e != nil || h != "upload=0; download=1048576; total=2147483648; expire=1893456000" {
		t.Fatal(h, e)
	}
}
func TestFlagsAliasesAndValidation(t *testing.T) {
	o, e := ParseOptions([]string{"-c", "a.yaml", "--url", "https://example.com", "-t", "sing-box", "-s", "abc", "--sort", "yes", "--dedup=1", "--expand=false", "--total", "1GiB"}, io.Discard)
	if e != nil {
		t.Fatal(e)
	}
	if !o.Sort.Value || !o.Dedup.Value || o.Expand.Value || o.Secret != "abc" {
		t.Fatal(o)
	}
	for _, args := range [][]string{{"--sort=maybe"}, {"--total=-1"}, {"--timeout=0s"}, {"-p", "80", "-o", "x"}, {"-p", "-1"}, {"--target", "unknown"}, {"--used", "1", "--download", "2"}, {"surprise"}} {
		if _, e := ParseOptions(args, io.Discard); e == nil {
			t.Fatalf("accepted %v", args)
		}
	}
}
func TestQueryIsolationAndStartupOnlyFlags(t *testing.T) {
	o := Defaults()
	o.Files = List{"test"}
	o.Include = List{"old"}
	o.Secret = "abc"
	q := url.Values{"include": {"new"}, "dedup": {"1"}, "srcret": {"abc"}}
	got, e := QueryOptions(o, q)
	if e != nil {
		t.Fatal(e)
	}
	if got.Include[0] != "new" || o.Include[0] != "old" {
		t.Fatal("request mutation leaks")
	}
	for _, q := range []url.Values{{"c": {"/etc/passwd"}}, {"public": {"true"}}, {"secret-file": {"/etc/passwd"}}, {"url": {"https://example.com"}}, {"unknown": {"1"}}, {"t": {"clash"}, "target": {"sing-box"}}, {"include": {"new"}, "i": {"invalid"}}, {"sort": {"garbage"}}} {
		_, e := QueryOptions(o, q)
		if e == nil {
			t.Fatal("accepted unsafe/invalid query", q)
		}
	}
	o.AllowURL = Tri{true, true}
	for _, s := range []string{"file:///etc/passwd", "/etc/passwd", "ftp://example.com"} {
		if _, e := QueryOptions(o, url.Values{"url": {s}}); e == nil {
			t.Fatal("accepted non-HTTP input", s)
		}
	}
}
func makeHandler(t *testing.T, o Options) http.Handler {
	t.Helper()
	e, err := NewEngine(o, strings.NewReader(""))
	if err != nil {
		t.Fatal(err)
	}
	return Handler(e, o, "test", io.Discard)
}
func TestHTTPAuthDetectionAndMetadata(t *testing.T) {
	o := Defaults()
	o.Files = List{example(t, "clash.yaml")}
	o.Secret = "key"
	o.Total = "100GiB"
	o.Used = "2GiB"
	o.Expire = "2030-01-01"
	h := makeHandler(t, o)
	for _, tc := range []struct {
		path, auth, ua, accept string
		code                   int
		typ                    string
	}{{"/sub?t=clash", "", "", "", 401, ""}, {"/sub?srcret=key&t=clash", "", "sing-box/1.14", "", 200, "clash"}, {"/sub", "Bearer key", "sing-box/1.14", "", 200, "singbox"}, {"/sub?secret=key", "", "Quantumult X", "", 200, "quanx"}, {"/sub?secret=key", "", "", "application/json", 200, "singbox"}, {"/sub?secret=bad", "Bearer key", "", "", 401, ""}, {"/sub?secret=key&t=unknown", "", "", "", 400, ""}, {"/healthz", "", "", "", 200, ""}, {"/missing?secret=key", "", "", "", 404, ""}} {
		r := httptest.NewRequest("GET", tc.path, nil)
		r.Header.Set("Authorization", tc.auth)
		r.Header.Set("User-Agent", tc.ua)
		r.Header.Set("Accept", tc.accept)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.code {
			t.Fatalf("%s: %d %s", tc.path, w.Code, w.Body.String())
		}
		if tc.typ != "" {
			if w.Header().Get("X-Output-Type") != tc.typ {
				t.Fatal(w.Header())
			}
			if w.Header().Get("Subscription-Userinfo") != "upload=0; download=2147483648; total=107374182400; expire=1893456000" {
				t.Fatal(w.Header())
			}
		}
	}
	r := httptest.NewRequest("HEAD", "/sub?secret=key", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 || w.Body.Len() != 0 || w.Header().Get("Content-Length") == "" {
		t.Fatal(w.Code, w.Body.String())
	}
}
func TestHTTPRateLimitAndMethod(t *testing.T) {
	o := Defaults()
	o.Secret = "key"
	o.Files = List{example(t, "clash.yaml")}
	o.Burst = 1
	h := makeHandler(t, o)
	for i := 0; i < 2; i++ {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", "/sub?s=key", nil))
		want := 200
		if i == 1 {
			want = 429
		}
		if w.Code != want {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/sub?s=key", nil))
	if w.Code != 405 {
		t.Fatal(w.Code)
	}
}
func TestHTTPRemoteInputUpstreamHeaderAndCache(t *testing.T) {
	var hits atomic.Int32
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.Header.Get("X-Test") != "yes" || r.Header.Get("User-Agent") != "custom-client" {
			http.Error(w, "bad headers", 400)
			return
		}
		w.Header().Set("Subscription-Userinfo", "upload=1; download=2; total=3; expire=4")
		data, _ := os.ReadFile(example(t, "clash.yaml"))
		w.Write(data)
	}))
	defer up.Close()
	o := Defaults()
	o.Secret = "key"
	o.UA = "custom-client"
	o.Headers = List{"X-Test: yes"}
	h := makeHandler(t, o)
	path := "/sub?s=key&t=clash&url=" + url.QueryEscape(up.URL)
	for i := 0; i < 2; i++ {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 200 || w.Header().Get("Subscription-Userinfo") != "upload=1; download=2; total=3; expire=4" {
			t.Fatal(w.Code, w.Body.String(), w.Header())
		}
	}
	if hits.Load() != 1 {
		t.Fatal("cache missed", hits.Load())
	}
}
func TestHTTPConcurrentRequests(t *testing.T) {
	o := Defaults()
	o.Files = List{example(t, "sing-box.json")}
	o.Secret = "key"
	o.RPM = 0
	h := makeHandler(t, o)
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			target := "clash"
			if i%2 == 0 {
				target = "sing-box"
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest("GET", "/sub?s=key&t="+target+"&rename="+url.QueryEscape("香港@HK"), nil))
			if w.Code != 200 {
				t.Errorf("concurrent: %d %s", w.Code, w.Body.String())
			}
		}(i)
	}
	wg.Wait()
}
func TestInputLimitAndFailedRemoteRules(t *testing.T) {
	o := Defaults()
	o.Files = List{"-"}
	o.MaxBytes = 4
	if _, e := NewEngine(o, strings.NewReader("12345")); e == nil {
		t.Fatal("limit ignored")
	}
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "not found", 404) }))
	defer up.Close()
	ini := filepath.Join(t.TempDir(), "rules.ini")
	os.WriteFile(ini, []byte("ruleset=PROXY,"+up.URL), 0600)
	o = Defaults()
	o.Files = List{example(t, "clash.yaml")}
	o.External = ini
	e, _ := NewEngine(o, strings.NewReader(""))
	if _, err := e.Convert(context.Background(), o); err == nil || !strings.Contains(err.Error(), "external resources") {
		t.Fatal(err)
	}
}
func TestJSONInputAndOutputInference(t *testing.T) {
	data, _ := os.ReadFile(example(t, "clash.yaml"))
	var m map[string]any
	if err := yaml.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(m)
	path := filepath.Join(t.TempDir(), "clash.json")
	os.WriteFile(path, b, 0600)
	o := Defaults()
	o.Files = List{path}
	o.Output = "result.json"
	if r := convertTest(t, o); r.Target != "singbox" {
		t.Fatal(r.Target)
	}
}

func TestNativeExtraRulesPreserveCatchAllOrder(t *testing.T) {
	o := Defaults()
	o.Files = List{example(t, "clash.yaml")}
	o.Rules = List{"DOMAIN,extra.example.com,PROXY", "MATCH,DIRECT"}
	r := convertTest(t, o)
	rules := stringsList(object(t, r)["rules"])
	if len(rules) != 4 || rules[0] != "DOMAIN,extra.example.com,PROXY" || rules[len(rules)-1] != "MATCH,DIRECT" {
		t.Fatal(rules)
	}
}
func TestNativeCustomDirectAndUnsupportedOutbounds(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.json")
	data := `{"outbounds":[{"type":"shadowsocks","tag":"node","server":"127.0.0.1","server_port":443,"method":"aes-256-gcm","password":"test"},{"type":"direct","tag":"bypass"},{"type":"naive","tag":"native-only","server":"127.0.0.2","server_port":443},{"type":"selector","tag":"PROXY","outbounds":["node","bypass","native-only"]}],"route":{"final":"PROXY","rules":[{"domain_suffix":["example.cn"],"outbound":"bypass"}]}}`
	os.WriteFile(p, []byte(data), 0600)
	o := Defaults()
	o.Files = List{p}
	r := convertTest(t, o)
	m := object(t, r)
	for _, v := range m["outbounds"].([]any) {
		g := v.(map[string]any)
		if g["tag"] == "PROXY" && len(stringsList(g["outbounds"])) != 3 {
			t.Fatal("native outbound membership lost", g)
		}
	}
	o.OutputType = "clash"
	r = convertTest(t, o)
	rules := stringsList(object(t, r)["rules"])
	if rules[0] != "DOMAIN-SUFFIX,example.cn,DIRECT" {
		t.Fatal(rules)
	}
}
func TestLogicalClashRulesAndExplicitFalseOptions(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.yaml")
	data, _ := os.ReadFile(example(t, "clash.yaml"))
	data = append(data, []byte("  - AND,((DOMAIN,example.com),(NETWORK,TCP)),PROXY\n")...)
	os.WriteFile(p, data, 0600)
	o := Defaults()
	o.Files = List{p}
	_ = convertTest(t, o)
	p = filepath.Join(t.TempDir(), "config.json")
	data, _ = os.ReadFile(example(t, "sing-box.json"))
	var m map[string]any
	json.Unmarshal(data, &m)
	a := m["outbounds"].([]any)
	ob := a[1].(map[string]any)
	ob["tcp_fast_open"] = true
	ob["tls"].(map[string]any)["insecure"] = true
	data, _ = json.Marshal(m)
	os.WriteFile(p, data, 0600)
	o = Defaults()
	o.Files = List{p}
	o.SCV = Tri{true, false}
	o.TFO = Tri{true, false}
	r := convertTest(t, o)
	for _, v := range object(t, r)["outbounds"].([]any) {
		ob := v.(map[string]any)
		if ob["type"] == "trojan" {
			if ob["tcp_fast_open"] != false || ob["tls"].(map[string]any)["insecure"] != false {
				t.Fatal(ob)
			}
		}
	}
}

func TestLiteralPipesInOptionsAndRepeatedURLQueries(t *testing.T) {
	o, err := ParseOptions([]string{"--include", "(香港|日本) [0-9]+", "--rename", "香港|日本@Asia", "--group", "REGION`select`香港|日本", "--header", "X-Test: a|b", "--url", "https://one.example/sub|https://two.example/sub"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if len(o.Include) != 1 || len(o.Rename) != 1 || len(o.Groups) != 1 || len(o.Headers) != 1 || len(o.URLs) != 2 {
		t.Fatal(o)
	}
	base := Defaults()
	got, err := QueryOptions(base, url.Values{"url": {"https://one.example/sub", "https://two.example/sub"}, "include": {"(香港|日本)"}})
	if err != nil || len(got.URLs) != 2 || len(got.Include) != 1 {
		t.Fatal(got, err)
	}
}
