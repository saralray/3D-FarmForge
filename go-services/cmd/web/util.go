package main

import (
	"crypto/rand"
	"encoding/base64"
	"math"
	"strconv"
	"strings"
)

// randomBase64URL mirrors randomBytes(n).toString('base64url'): n random bytes,
// URL-safe base64, no padding.
func randomBase64URL(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func itoa(i int) string { return strconv.Itoa(i) }

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func derefFloat(p *float64) float64 {
	if p == nil {
		return 0
	}
	return *p
}

// round2 mirrors JS Math.round(x*100)/100.
func round2(x float64) float64 {
	return math.Round(x*100) / 100
}

// jsParseInt mirrors Number.parseInt(s, 10): an optional sign followed by leading
// decimal digits; returns ok=false when no digits lead (NaN).
func jsParseInt(s string) (int, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	i := 0
	neg := false
	if s[0] == '+' || s[0] == '-' {
		neg = s[0] == '-'
		i = 1
	}
	start := i
	n := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		n = n*10 + int(s[i]-'0')
		i++
	}
	if i == start {
		return 0, false
	}
	if neg {
		n = -n
	}
	return n, true
}

// matchLubric mirrors /lubric/i.test(s).
func matchLubric(s string) bool {
	return strings.Contains(strings.ToLower(s), "lubric")
}

// trimString mirrors String(value ?? ”).trim() for string-ish values.
func trimString(v any) string {
	switch s := v.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(s)
	default:
		return ""
	}
}
