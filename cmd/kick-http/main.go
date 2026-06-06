// Command kick-http spikes the NON-CDP path for Kick: a pure-HTTP client that
// mimics a real Chrome TLS (JA3) + HTTP/2 fingerprint via utls, replaying the
// user's session cookies. No browser, no CDP → nothing flags automation. If this
// returns 200 on /api/v1/user, Kick is reachable without a browser and we build
// the Kick backend on this transport. If it 403s like everything else, the block
// needs more than TLS (IP-bound cf_clearance or an API token) → fall to a
// browser-extension architecture (see kick_issues.md).
//
//	go run ./cmd/kick-http path/to/cookies.json [url]
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	neturl "net/url"
	"os"
	"strings"
	"time"

	utls "github.com/refraction-networking/utls"
	"golang.org/x/net/http2"
)

type cookie struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

const chromeUA = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36"

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: kick-http cookies.json [url]")
		os.Exit(1)
	}
	url := "https://kick.com/api/v1/user"
	if len(os.Args) >= 3 {
		url = os.Args[2]
	}
	raw, err := os.ReadFile(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, "read cookies:", err)
		os.Exit(1)
	}
	// `drops` suite: hit every real Kick drops endpoint so one run with fresh
	// cookies reveals all response shapes for the backend structs.
	if url == "drops" {
		for _, u := range []string{
			"https://kick.com/api/v1/user",
			"https://kick.com/api/v1/drops/enabled",
			"https://kick.com/api/v1/drops/campaigns",
			"https://kick.com/api/v1/drops/progress",
			"https://kick.com/api/v1/drops/progress/summary",
		} {
			fmt.Printf("\n########## %s\n", u)
			if err := runOne(raw, u); err != nil {
				fmt.Fprintln(os.Stderr, "  err:", err)
			}
		}
		return
	}
	if err := runOne(raw, url); err != nil {
		fmt.Fprintln(os.Stderr, "fatal:", err)
		os.Exit(1)
	}
}

func runOne(raw []byte, url string) error {
	var cks []cookie
	json.Unmarshal(raw, &cks)
	var pairs []string
	var xsrf, sessionToken string
	for _, c := range cks {
		if c.Value == "" {
			continue
		}
		pairs = append(pairs, c.Name+"="+c.Value) // forward ALL cookies (browser-faithful)
		switch c.Name {
		case "XSRF-TOKEN":
			xsrf = c.Value
		case "session_token":
			// Laravel Sanctum "id|token" (URL-encoded). Bearer wants the raw
			// token (after the | / %7C).
			st := strings.ReplaceAll(c.Value, "%7C", "|")
			if i := strings.IndexByte(st, '|'); i >= 0 {
				sessionToken = st[i+1:]
			} else {
				sessionToken = st
			}
		}
	}
	cookieHeader := strings.Join(pairs, "; ")
	fmt.Printf("GET %s\ncookies: %s\n\n", url, redact(pairs))

	host := "kick.com"
	if u, e := neturl.Parse(url); e == nil && u.Host != "" {
		host = u.Host
	}
	tcp, err := net.DialTimeout("tcp", host+":443", 15*time.Second)
	if err != nil {
		fmt.Fprintln(os.Stderr, "dial:", err)
		os.Exit(1)
	}
	// Chrome ClientHello (JA3) via utls; negotiate h2.
	uconn := utls.UClient(tcp, &utls.Config{ServerName: host, NextProtos: []string{"h2", "http/1.1"}}, utls.HelloChrome_Auto)
	if err := uconn.Handshake(); err != nil {
		fmt.Fprintln(os.Stderr, "tls handshake:", err)
		os.Exit(1)
	}
	cs := uconn.ConnectionState()
	fmt.Printf("TLS ok: proto=%q cipher=%x\n", cs.NegotiatedProtocol, cs.CipherSuite)

	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("User-Agent", chromeUA)
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Cookie", cookieHeader)
	req.Header.Set("Referer", "https://kick.com/")
	req.Header.Set("Origin", "https://kick.com")
	if xsrf != "" {
		req.Header.Set("X-XSRF-TOKEN", xsrf)
	}
	if sessionToken != "" {
		req.Header.Set("Authorization", "Bearer "+sessionToken)
	}
	if ct := os.Getenv("KICK_CLIENT_TOKEN"); ct != "" {
		req.Header.Set("X-CLIENT-TOKEN", ct)
	}
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("Sec-Fetch-Mode", "cors")
	req.Header.Set("Sec-Fetch-Dest", "empty")

	var resp *http.Response
	if cs.NegotiatedProtocol == "h2" {
		tr := &http2.Transport{}
		cc, err := tr.NewClientConn(uconn)
		if err != nil {
			fmt.Fprintln(os.Stderr, "h2 clientconn:", err)
			os.Exit(1)
		}
		resp, err = cc.RoundTrip(req)
		if err != nil {
			fmt.Fprintln(os.Stderr, "h2 roundtrip:", err)
			os.Exit(1)
		}
	} else {
		// HTTP/1.1 fallback: write the request manually over the utls conn.
		fmt.Fprintf(uconn, "GET %s HTTP/1.1\r\nHost: %s\r\nUser-Agent: %s\r\nAccept: application/json\r\nCookie: %s\r\nConnection: close\r\n\r\n", req.URL.RequestURI(), host, chromeUA, cookieHeader)
		b, _ := io.ReadAll(uconn)
		fmt.Println(truncate(string(b), 800))
		return nil
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if loc := resp.Header.Get("Location"); loc != "" {
		fmt.Println("Location:", loc)
	}
	if os.Getenv("KICK_FULL") == "1" {
		fmt.Printf("\nHTTP %d\n%s\n", resp.StatusCode, string(body))
		return nil
	}
	fmt.Printf("\nHTTP %d\n%s\n", resp.StatusCode, truncate(string(body), 800))
	switch {
	case resp.StatusCode == 200:
		fmt.Println("\nRESULT: PASS — non-CDP HTTP works. Build Kick backend on utls.")
	case resp.StatusCode == 401:
		fmt.Println("\nRESULT: 401 — TLS passed CF but session invalid/expired (re-export cookies).")
	case resp.StatusCode == 403:
		fmt.Println("\nRESULT: 403 — still blocked. TLS alone insufficient (IP-bound cf_clearance / API token). Lean to extension.")
	}
	return nil
}

func redact(pairs []string) string {
	out := make([]string, 0, len(pairs))
	for _, p := range pairs {
		k := strings.SplitN(p, "=", 2)[0]
		out = append(out, k+"=…")
	}
	return strings.Join(out, "; ")
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
