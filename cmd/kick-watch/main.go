// Command kick-watch is a spike for the Kick viewer-presence websocket — the
// mechanism that accrues drops watch-time. Flow (reverse-engineered from the
// Next.js bundle):
//  1. GET https://websockets.kick.com/viewer/v1/token  (header X-CLIENT-TOKEN,
//     cookies, NO Bearer) -> {data:{token}}
//  2. wss://websockets.kick.com/viewer/v1/connect?token=<token>  (Chrome TLS via
//     utls; WS is HTTP/1.1 Upgrade)
//  3. send {type:"init", data:{...}} naming the livestream, then {type:"ping"}.
//
// This tool connects, prints every server frame, and tries a couple init shapes
// so we can nail the protocol, then bake it into the kick backend.
//
//	kick-watch cookies.json <channel-slug>
package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	utls "github.com/refraction-networking/utls"
	xhttp2 "golang.org/x/net/http2"
)

const (
	clientToken = "e1393935a959b4020a4491574f6490129f678acdaa92760471263db43487f823"
	chromeUA    = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36"
)

func main() {
	if len(os.Args) < 3 {
		die("usage: kick-watch cookies.json <channel-slug>")
	}
	cookieHeader := loadCookies(os.Args[1])
	slug := os.Args[2]

	// 1) viewer token (NO Bearer — that 403s)
	tok := getToken(cookieHeader)
	fmt.Println("[1] viewer token:", tok)

	// 2) resolve livestream id for the channel
	lsID := livestreamID(cookieHeader, slug)
	fmt.Printf("[2] %s livestream id: %s\n", slug, lsID)
	if lsID == "" {
		die(slug + " is offline — pick a live channel")
	}

	// 3) connect the viewer WS over a Chrome-fingerprinted (utls) HTTP/1.1 conn
	dialer := websocket.Dialer{
		TLSClientConfig:  &tls.Config{NextProtos: []string{"http/1.1"}},
		HandshakeTimeout: 15 * time.Second,
	}
	if os.Getenv("KICK_WS_UTLS") == "1" {
		// utls path forcing ALPN http/1.1 via a custom spec (HelloChrome_Auto
		// bakes h2 into ALPN, which breaks the WS upgrade).
		dialer.NetDialTLSContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			tcp, err := net.Dial("tcp", addr)
			if err != nil {
				return nil, err
			}
			uc := utls.UClient(tcp, &utls.Config{ServerName: "websockets.kick.com", NextProtos: []string{"http/1.1"}}, utls.HelloChrome_106_Shuffle)
			if err := uc.Handshake(); err != nil {
				return nil, err
			}
			return uc, nil
		}
	}
	hdr := http.Header{}
	hdr.Set("User-Agent", chromeUA)
	hdr.Set("Origin", "https://kick.com")
	hdr.Set("Cookie", cookieHeader)
	wsURL := "wss://websockets.kick.com/viewer/v1/connect?token=" + tok
	conn, resp, err := dialer.Dial(wsURL, hdr)
	if err != nil {
		st := 0
		if resp != nil {
			st = resp.StatusCode
		}
		die(fmt.Sprintf("ws dial: %v (status %d)", err, st))
	}
	defer conn.Close()
	fmt.Println("[3] WS connected")

	// reader
	go func() {
		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				fmt.Println("  <read closed>", err)
				return
			}
			fmt.Println("  <<", string(msg))
		}
	}()

	time.Sleep(1 * time.Second)
	// video_id as a NUMBER (string likely caused "Invalid message received").
	var lsNum int64
	fmt.Sscanf(lsID, "%d", &lsNum)
	shapes := []map[string]any{
		{"type": "init", "data": map[string]any{"video_id": lsNum}},
		{"type": "init", "data": map[string]any{"livestream_id": lsNum}},
		{"type": "livestream", "data": map[string]any{"id": lsNum}},
		{"type": "init", "data": map[string]any{"video_id": lsNum, "channel_id": lsNum}},
	}
	idx := 0
	if v := os.Getenv("KICK_SHAPE"); v != "" {
		fmt.Sscanf(v, "%d", &idx)
	}
	if idx >= len(shapes) {
		idx = 0
	}
	_ = shapes
	_ = idx
	chanID := channelID(cookieHeader, slug)
	fmt.Println("[2b] channel id:", chanID)
	hs := map[string]any{"type": "channel_handshake", "data": map[string]any{"message": map[string]any{"channelId": chanID}}}
	send(conn, hs)

	// keepalive ping loop; watch for ~40s
	end := time.After(40 * time.Second)
	tick := time.NewTicker(10 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-end:
			fmt.Println("[done]")
			return
		case <-tick.C:
			send(conn, hs)
			send(conn, map[string]any{"type": "ping"})
		}
	}
}

func send(c *websocket.Conn, m map[string]any) {
	b, _ := json.Marshal(m)
	fmt.Println("  >>", string(b))
	if err := c.WriteMessage(websocket.TextMessage, b); err != nil {
		fmt.Println("  >> write err", err)
	}
}

// --- utls HTTP/2 helper for the token + livestream lookups ---

func httpGet(url, cookieHeader string, clientTok bool) (int, []byte) {
	u := strings.TrimPrefix(strings.TrimPrefix(url, "https://"), "http://")
	host := u[:strings.IndexByte(u, '/')]
	tcp, err := net.DialTimeout("tcp", host+":443", 15*time.Second)
	if err != nil {
		die("dial " + host + ": " + err.Error())
	}
	uc := utls.UClient(tcp, &utls.Config{ServerName: host, NextProtos: []string{"h2", "http/1.1"}}, utls.HelloChrome_Auto)
	if err := uc.Handshake(); err != nil {
		die("tls: " + err.Error())
	}
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("User-Agent", chromeUA)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Cookie", cookieHeader)
	req.Header.Set("Origin", "https://kick.com")
	req.Header.Set("Referer", "https://kick.com/")
	if clientTok {
		req.Header.Set("X-CLIENT-TOKEN", clientToken)
	}
	tr := &xhttp2.Transport{}
	cc, err := tr.NewClientConn(uc)
	if err != nil {
		die("h2: " + err.Error())
	}
	resp, err := cc.RoundTrip(req)
	if err != nil {
		die("rt: " + err.Error())
	}
	defer resp.Body.Close()
	buf := make([]byte, 0, 4096)
	tmp := make([]byte, 4096)
	for {
		n, e := resp.Body.Read(tmp)
		buf = append(buf, tmp[:n]...)
		if e != nil {
			break
		}
	}
	return resp.StatusCode, buf
}

func getToken(cookieHeader string) string {
	st, body := httpGet("https://websockets.kick.com/viewer/v1/token", cookieHeader, true)
	if st != 200 {
		die(fmt.Sprintf("token status %d: %s", st, body))
	}
	var r struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	json.Unmarshal(body, &r)
	return r.Data.Token
}

func channelID(cookieHeader, slug string) string {
	st, body := httpGet("https://kick.com/api/v2/channels/"+slug, cookieHeader, false)
	if st != 200 {
		return ""
	}
	var r struct {
		ID int64 `json:"id"`
	}
	json.Unmarshal(body, &r)
	if r.ID == 0 {
		return ""
	}
	return fmt.Sprintf("%d", r.ID)
}

func livestreamID(cookieHeader, slug string) string {
	st, body := httpGet("https://kick.com/api/v2/channels/"+slug+"/livestream", cookieHeader, false)
	if st != 200 {
		return ""
	}
	var r struct {
		Data *struct {
			ID int64 `json:"id"`
		} `json:"data"`
	}
	json.Unmarshal(body, &r)
	if r.Data == nil {
		return ""
	}
	return fmt.Sprintf("%d", r.Data.ID)
}

func loadCookies(path string) string {
	raw, err := os.ReadFile(path)
	if err != nil {
		die(err.Error())
	}
	var cks []struct{ Name, Value string }
	json.Unmarshal(raw, &cks)
	var pairs []string
	for _, c := range cks {
		if c.Value != "" {
			pairs = append(pairs, c.Name+"="+c.Value)
		}
	}
	return strings.Join(pairs, "; ")
}

var _ = tls.VersionTLS13

func die(m string) { fmt.Fprintln(os.Stderr, "fatal:", m); os.Exit(1) }
