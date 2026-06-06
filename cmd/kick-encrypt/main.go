// Command kick-encrypt builds a Kick platform.Session from a flat cookies JSON
// and age-encrypts it with MINER_MASTER_KEY, emitting the ciphertext as hex for
// a direct sessions-table UPSERT. One-shot ops tool (not committed long-term).
//
//	MINER_MASTER_KEY=... kick-encrypt cookies.json > ct.hex
package main

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/aalejandrofer/dropsminer/internal/platform"
	"github.com/aalejandrofer/dropsminer/internal/store"
)

const chromeUA = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36"

func main() {
	if len(os.Args) < 2 {
		die("usage: kick-encrypt cookies.json (MINER_MASTER_KEY env)")
	}
	key := os.Getenv("MINER_MASTER_KEY")
	if key == "" {
		die("MINER_MASTER_KEY unset")
	}
	raw, err := os.ReadFile(os.Args[1])
	if err != nil {
		die(err.Error())
	}
	var flat []struct{ Name, Value string }
	if err := json.Unmarshal(raw, &flat); err != nil {
		die("parse cookies: " + err.Error())
	}
	type kcookie struct {
		Name   string `json:"name"`
		Value  string `json:"value"`
		Domain string `json:"domain"`
		Path   string `json:"path"`
	}
	ks := struct {
		Cookies   []kcookie `json:"cookies"`
		XSRFToken string    `json:"xsrf_token"`
		UserAgent string    `json:"user_agent"`
	}{UserAgent: chromeUA}
	for _, c := range flat {
		ks.Cookies = append(ks.Cookies, kcookie{Name: c.Name, Value: c.Value, Domain: ".kick.com", Path: "/"})
		if c.Name == "XSRF-TOKEN" {
			ks.XSRFToken = c.Value
		}
	}
	ksJSON, _ := json.Marshal(ks)
	sess := platform.Session{
		Cookies:   map[string]string{"kick": string(ksJSON)},
		CSRF:      ks.XSRFToken,
		ExpiresAt: time.Now().Add(30 * 24 * time.Hour),
	}
	plain, _ := json.Marshal(sess)
	cr, err := store.NewCryptor(key)
	if err != nil {
		die(err.Error())
	}
	ct, err := cr.Encrypt(plain)
	if err != nil {
		die(err.Error())
	}
	fmt.Fprintf(os.Stderr, "encrypted %d cookies, expires %s\n", len(ks.Cookies), sess.ExpiresAt.Format(time.RFC3339))
	fmt.Print(hex.EncodeToString(ct))
}

func die(m string) { fmt.Fprintln(os.Stderr, "fatal:", m); os.Exit(1) }
