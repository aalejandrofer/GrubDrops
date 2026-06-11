// grubdrops-helper is a small CLI that copies your local browser's
// kick.com cookies into a grubdrops deployment so it can mine on your
// behalf. (Twitch needs no helper — it authorizes via device code on the
// miner itself.)
//
// Usage:
//
//	grubdrops-helper kick <account-id> [flags]
//
// Flags:
//
//	--miner    URL    Base URL of the miner (default https://drops.ryuzec.dev)
//	--browser  NAME   Limit cookie search to a specific browser.
//	--channel  NAMES  Optional Kick channels to pin (comma/space-separated; repeatable).
//	                  Omit to auto-discover channels from each campaign's game.
//	--insecure        Skip TLS verification (debug only).
//
// No password: the unguessable account ID is the only credential.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/aalejandrofer/grubdrops/internal/helper"
)

func main() {
	// No args = launched by double-click (no terminal). Go interactive and
	// keep the window open, instead of flashing usage and vanishing.
	if len(os.Args) < 2 {
		err := runInteractive()
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
		}
		pause()
		if err != nil {
			os.Exit(1)
		}
		return
	}
	cmd := os.Args[1]
	args := os.Args[2:]

	var err error
	switch cmd {
	case "kick":
		err = runKick(args)
	case "twitch":
		fmt.Fprintln(os.Stderr, "Twitch needs no helper — authorize it via device code on the miner's account page.")
		os.Exit(2)
	case "-h", "--help", "help":
		usage(os.Stdout)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", cmd)
		usage(os.Stderr)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func usage(w io.Writer) {
	fmt.Fprint(w, `grubdrops-helper — push your kick.com cookies to a grubdrops deployment

Usage:
  grubdrops-helper kick <account-id> [--miner URL] [--browser NAME] [--channel NAMES]

Flags:
  --miner     base URL of the miner (default https://drops.ryuzec.dev)
  --browser   limit cookie search to this browser (chrome, firefox, safari, operagx, ...)
  --channel   optional Kick channels to pin (comma/space-separated; repeatable).
              Omit to auto-discover channels from each campaign's game.
  --insecure  skip TLS verification

No password needed — the account ID is the only credential.
Twitch authorizes via device code on the miner; it needs no helper.

`)
}

type commonFlags struct {
	helper.Config
}

// boolFlagsByName lists the flag names that take no value, so
// reorderArgs knows not to consume the following arg as a value.
// Keep in sync with the BoolVar registrations below.
var boolFlagsByName = map[string]bool{"insecure": true}

func parseCommon(fs *flag.FlagSet, args []string, extra func(*flag.FlagSet)) (commonFlags, []string, error) {
	cf := commonFlags{Config: helper.Config{
		MinerURL: helper.DefaultMinerURL,
	}}
	fs.StringVar(&cf.MinerURL, "miner", cf.MinerURL, "base URL of the miner")
	fs.StringVar(&cf.Browser, "browser", "", "limit cookie search to this browser")
	fs.BoolVar(&cf.Insecure, "insecure", false, "skip TLS verification")
	if extra != nil {
		extra(fs)
	}
	// Allow operators to write positional args before flags (e.g.
	// `kick acc_id --channel oilrats`). Std flag.Parse stops at the
	// first non-flag arg, so without reordering that command silently
	// produces three positional args. Reorder shuffles all -flag/--flag
	// tokens (with their values) to the front before parsing.
	if err := fs.Parse(reorderArgs(args, boolFlagsByName)); err != nil {
		return cf, nil, err
	}
	return cf, fs.Args(), nil
}

// reorderArgs walks args once and emits all flag tokens before any
// positional tokens. A flag is any arg starting with "-"; if it lacks
// "=" and isn't a known bool flag, the next arg is treated as its
// value. The "--" sentinel ends flag scanning — everything after is
// positional.
func reorderArgs(args []string, bools map[string]bool) []string {
	flagToks := make([]string, 0, len(args))
	posToks := make([]string, 0, len(args))
	i := 0
	for i < len(args) {
		a := args[i]
		if a == "--" {
			posToks = append(posToks, args[i+1:]...)
			break
		}
		if strings.HasPrefix(a, "-") && a != "-" {
			flagToks = append(flagToks, a)
			name := strings.TrimLeft(a, "-")
			if eq := strings.Index(name, "="); eq >= 0 {
				i++
				continue
			}
			if !bools[name] && i+1 < len(args) {
				flagToks = append(flagToks, args[i+1])
				i += 2
				continue
			}
			i++
			continue
		}
		posToks = append(posToks, a)
		i++
	}
	return append(flagToks, posToks...)
}

// channelList implements flag.Value so --channel can be repeated or
// passed comma/space-separated.
type channelList []string

func (c *channelList) String() string { return strings.Join(*c, ",") }

func (c *channelList) Set(v string) error {
	for _, part := range splitChannels(v) {
		*c = append(*c, part)
	}
	return nil
}

func splitChannels(raw string) []string {
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

func runKick(args []string) error {
	fs := flag.NewFlagSet("kick", flag.ContinueOnError)
	var channels channelList
	cf, rest, err := parseCommon(fs, args, func(fs *flag.FlagSet) {
		fs.Var(&channels, "channel", "Kick channel(s) to mine — repeat or comma-separate for multiple")
	})
	if err != nil {
		return err
	}
	if len(rest) != 1 {
		return fmt.Errorf("kick requires exactly one account-id argument")
	}
	// --channel is optional: channels auto-discover from each campaign's game.
	// Pass it only to pin specific channels.
	res, err := helper.PushKick(context.Background(), helper.KickRequest{Config: cf.Config, AccountID: rest[0], Channels: channels})
	if err != nil {
		return err
	}
	fmt.Printf("✓ %s\n", res.Message)
	return nil
}
