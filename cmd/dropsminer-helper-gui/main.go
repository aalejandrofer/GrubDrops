// dropsminer-helper-gui is a small cross-platform GUI for the
// dropsminer-helper. Designed for non-developer friends on Windows
// who don't want to install Go or use the terminal.
//
// It serves a single HTML form on a random localhost port, opens the
// user's default browser to it, and hands the submitted form off to
// internal/helper. No Fyne, no CGO — cross-compile is trivial.
//
// Usage:
//
//	dropsminer-helper-gui
//
// The window stays open until the user closes the browser tab AND
// dismisses the terminal. Ctrl+C also exits.
package main

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/aalejandrofer/grubdrops/internal/helper"
)

//go:embed index.html
var assetsFS embed.FS

var tmpl = template.Must(template.ParseFS(assetsFS, "index.html"))

type submitResp struct {
	OK              bool     `json:"ok"`
	Message         string   `json:"message"`
	UploadedCookies []string `json:"uploaded_cookies,omitempty"`
	Error           string   `json:"error,omitempty"`
}

func main() {
	// GUI always streams debug logs to the launching terminal so the
	// user (or whoever sent them this binary) can paste them back to
	// us when something breaks.
	if os.Getenv("MINER_HELPER_DEBUG") == "" {
		_ = os.Setenv("MINER_HELPER_DEBUG", "1")
	}

	mux := http.NewServeMux()

	defaultMiner := os.Getenv("MINER_URL")
	if defaultMiner == "" {
		defaultMiner = "http://localhost:8080"
	}

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = tmpl.Execute(w, map[string]any{
			"DefaultMiner": defaultMiner,
		})
	})

	mux.HandleFunc("/submit", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		if err := r.ParseForm(); err != nil {
			writeJSON(w, submitResp{Error: "parse form: " + err.Error()})
			return
		}
		cfg := helper.Config{
			MinerURL: strings.TrimSpace(r.FormValue("miner")),
			Password: r.FormValue("password"),
			Browser:  strings.TrimSpace(r.FormValue("browser")),
			Insecure: r.FormValue("insecure") == "1",
		}
		accountID := strings.TrimSpace(r.FormValue("account_id"))
		channels := splitKickChannels(r.FormValue("channel"))
		platform := r.FormValue("platform")

		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()

		switch platform {
		case "twitch":
			res, err := helper.PushTwitch(ctx, helper.TwitchRequest{Config: cfg, AccountID: accountID})
			if err != nil {
				writeJSON(w, submitResp{Error: err.Error()})
				return
			}
			writeJSON(w, submitResp{OK: true, Message: res.Message, UploadedCookies: res.UploadedCookies})
		case "kick":
			res, err := helper.PushKick(ctx, helper.KickRequest{Config: cfg, AccountID: accountID, Channels: channels})
			if err != nil {
				writeJSON(w, submitResp{Error: err.Error()})
				return
			}
			writeJSON(w, submitResp{OK: true, Message: res.Message, UploadedCookies: res.UploadedCookies})
		default:
			writeJSON(w, submitResp{Error: "platform must be twitch or kick"})
		}
	})

	// Bind to 127.0.0.1 only — no LAN exposure.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Fatalf("listen: %v", err)
	}
	addr := listener.Addr().String()
	url := "http://" + addr + "/"
	fmt.Println("DropsMiner Helper running at", url)
	fmt.Println("(close this terminal to quit)")

	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if err := srv.Serve(listener); err != nil && err != http.ErrServerClosed {
			log.Fatalf("serve: %v", err)
		}
	}()

	// Give the server a beat then open the browser.
	time.Sleep(150 * time.Millisecond)
	if err := openBrowser(url); err != nil {
		fmt.Println("could not auto-open browser — open", url, "manually:", err)
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
}

// splitKickChannels accepts comma/space/semicolon-separated channel
// input and returns a deduped trimmed list. Mirrors the server-side
// parseKickChannels so the GUI tolerates the same paste shapes the
// /accounts login form does.
func splitKickChannels(raw string) []string {
	splitter := func(r rune) bool {
		switch r {
		case ',', ' ', '\t', '\n', '\r', ';':
			return true
		}
		return false
	}
	parts := strings.FieldsFunc(raw, splitter)
	out := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		key := strings.ToLower(p)
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, p)
	}
	return out
}

func writeJSON(w http.ResponseWriter, r submitResp) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(r)
}

// openBrowser opens the URL in the user's default browser. Per-OS:
// macOS uses `open`, Linux `xdg-open`, Windows `rundll32 url.dll,FileProtocolHandler`.
func openBrowser(url string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", url).Start()
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	default:
		return exec.Command("xdg-open", url).Start()
	}
}
