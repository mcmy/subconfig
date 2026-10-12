package app

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var byteRE = regexp.MustCompile(`(?i)^([0-9]+(?:\.[0-9]+)?)\s*(B|KB|MB|GB|TB|KIB|MIB|GIB|TIB)?$`)

func Bytes(s string) (uint64, error) {
	m := byteRE.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return 0, fmt.Errorf("invalid traffic quantity %q", s)
	}
	if m[2] == "" || strings.EqualFold(m[2], "B") {
		if !strings.Contains(m[1], ".") {
			return strconv.ParseUint(m[1], 10, 64)
		}
	}
	n, _ := strconv.ParseFloat(m[1], 64)
	unit := strings.ToUpper(m[2])
	mult := map[string]float64{"": 1, "B": 1, "KB": 1e3, "MB": 1e6, "GB": 1e9, "TB": 1e12, "KIB": 1024, "MIB": 1 << 20, "GIB": 1 << 30, "TIB": 1 << 40}[unit]
	n *= mult
	if n < 0 || n >= math.Exp2(64) || math.Trunc(n) != n {
		return 0, fmt.Errorf("traffic quantity must fit uint64 whole bytes: %q", s)
	}
	return uint64(n), nil
}
func expiry(s string) (uint64, error) {
	if n, e := strconv.ParseUint(s, 10, 64); e == nil {
		return n, nil
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02"} {
		if t, e := time.Parse(layout, s); e == nil && t.Unix() >= 0 {
			return uint64(t.Unix()), nil
		}
	}
	return 0, fmt.Errorf("invalid expiry %q", s)
}

// Explicit fields override upstream metadata; unspecified fields remain intact.
func Userinfo(o Options, upstream string) (string, error) {
	vals := map[string]uint64{}
	parse := func(s string) error {
		for _, f := range strings.Split(s, ";") {
			if strings.TrimSpace(f) == "" {
				continue
			}
			k, v, ok := strings.Cut(f, "=")
			k = strings.TrimSpace(strings.ToLower(k))
			if !ok || (k != "upload" && k != "download" && k != "total" && k != "expire") {
				return fmt.Errorf("invalid Subscription-Userinfo field")
			}
			n, e := strconv.ParseUint(strings.TrimSpace(v), 10, 64)
			if e != nil {
				return fmt.Errorf("invalid Subscription-Userinfo number")
			}
			vals[k] = n
		}
		return nil
	}
	if upstream != "" {
		if e := parse(upstream); e != nil {
			vals = map[string]uint64{}
		}
	}
	if o.Userinfo != "" {
		vals = map[string]uint64{}
		if e := parse(o.Userinfo); e != nil {
			return "", e
		}
	}
	for k, v := range map[string]string{"upload": o.Upload, "download": o.Download, "total": o.Total} {
		if v != "" {
			n, e := Bytes(v)
			if e != nil {
				return "", e
			}
			vals[k] = n
		}
	}
	if o.Used != "" {
		n, e := Bytes(o.Used)
		if e != nil {
			return "", e
		}
		vals["upload"] = 0
		vals["download"] = n
	}
	if o.Expire != "" {
		n, e := expiry(o.Expire)
		if e != nil {
			return "", e
		}
		vals["expire"] = n
	}
	if len(vals) == 0 {
		return "", nil
	}
	var out []string
	for _, k := range []string{"upload", "download", "total", "expire"} {
		if n, ok := vals[k]; ok || k != "expire" {
			out = append(out, fmt.Sprintf("%s=%d", k, n))
		}
	}
	return strings.Join(out, "; "), nil
}
