package parser

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDocumentContentInference(t *testing.T) {
	for _, tc := range []struct{ body, hint, typ string }{{`{"outbounds":[{"type":"shadowsocks","tag":"node","server":"a.test","server_port":443,"method":"aes-256-gcm","password":"test"}]}`, "wrong.yaml", "singbox"}, {`{"proxies":[{"type":"ss","name":"node","server":"a.test","port":443,"cipher":"aes-256-gcm","password":"test"}]}`, "wrong.json", "clash"}, {"proxies:\n  - {name: node, type: ss, server: a.test, port: 443, cipher: aes-256-gcm, password: test}\n", "config.txt", "clash"}, {"trojan://test@example.com:443#node", "file.txt", "mixed"}} {
		d, e := ParseDocument([]byte(tc.body), tc.hint, "auto")
		if e != nil || d.Type != tc.typ || len(d.Nodes) != 1 {
			t.Fatal(d, e)
		}
	}
}
func TestJSONCommentsAndReality(t *testing.T) {
	s := `{
 // URL in string must remain intact
 "outbounds": [{"type":"vless","tag":"node","server":"a.test","server_port":443,"uuid":"00000000-0000-4000-8000-000000000000","flow":"xtls-rprx-vision", "tls":{"enabled":true,"server_name":"test.com","utls":{"enabled":true,"fingerprint":"chrome"},"reality":{"enabled":true,"public_key":"test-key","short_id":"abcd"}},"transport":{"type":"ws","path":"https://path.test/ws"}}] /* comment */
}`
	d, e := ParseDocument([]byte(s), "a.jsonc", "auto")
	if e != nil {
		t.Fatal(e)
	}
	n := d.Nodes[0]
	if n.Clash["flow"] != "xtls-rprx-vision" || n.Clash["client-fingerprint"] != "chrome" {
		t.Fatal(n.Clash)
	}
	ws := n.Clash["ws-opts"].(map[string]any)
	if ws["path"] != "https://path.test/ws" {
		t.Fatal(ws)
	}
	if !json.Valid(stripJSONComments([]byte(s))) {
		t.Fatal("invalid stripped JSON")
	}
}
func TestInvalidDocuments(t *testing.T) {
	for _, tc := range []struct{ body, hint, typ string }{{"{bad}", "config.json", "auto"}, {"{}", "config.json", "sing-box"}, {`{"proxies":"bad"}`, "a.json", "auto"}, {"text", "a.txt", "unknown"}} {
		if _, e := ParseDocument([]byte(tc.body), tc.hint, tc.typ); e == nil {
			t.Fatal("accepted", tc)
		}
	}
}
func TestMissingAndUnsupportedNodes(t *testing.T) {
	d, e := ParseDocument([]byte(`{"outbounds":[{"type":"direct","tag":"direct"},{"type":"unknown","tag":"x"},{"type":"ss","tag":"bad"}]}`), "a.json", "auto")
	if e != nil || len(d.Nodes) != 0 || len(d.Skipped) != 2 {
		t.Fatal(d, e)
	}
	if _, e = ParseDocument([]byte(strings.Repeat(" ", 10)), "x", "auto"); e != nil {
		t.Fatal(e)
	}
}
