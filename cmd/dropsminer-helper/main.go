// dropsminer-helper is a small CLI that copies cookies from the
// user's local browser into the dropsminer deployment.
//
// Usage:
//
//	dropsminer-helper twitch <account-id> [flags]
//	dropsminer-helper kick   <account-id> --channel STREAMER[,STREAMER2,...] [flags]
//
// Flags:
//
//	--miner    URL    Base URL of the miner (default https://drops.ryuzec.dev)
//	--password STR    Admin password. Falls back to MINER_PASSWORD env.
//	--browser  NAME   Limit cookie search to a specific browser.
//	--channel  NAMES  One or more Kick channels (comma/space-separated; repeatable).
//	--insecure        Skip TLS verification (debug only).
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/aalejandrofer/dropsminer/internal/helper"
)

func main() {
	if len(os.Args) < 2 {
		usage(os.Stderr)
		os.Exit(2)
	}
	cmd := os.Args[1]
	args := os.Args[2:]

	var err error
	switch cmd {
	case "twitch":
		err = runTwitch(args)
	case "kick":
		err = runKick(args)
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
	fmt.Fprint(w, `dropsminer-helper — push browser cookies to a dropsminer deployment

Usage:
  dropsminer-helper twitch <account-id> [--miner URL] [--password PW] [--browser NAME]
  dropsminer-helper kick   <account-id> [--miner URL] [--password PW] [--browser NAME] --channel NAMES

Flags:
  --miner     base URL of the miner (default https://drops.ryuzec.dev)
  --password  admin password (or set MINER_PASSWORD)
  --browser   limit cookie search to this browser (chrome, firefox, safari, ...)
  --channel   one or more Kick channels (comma/space-separated; repeatable)
  --insecure  skip TLS verification

`)
}

type commonFlags struct {
	helper.Config
}

func parseCommon(fs *flag.FlagSet, args []string, extra func(*flag.FlagSet)) (commonFlags, []string, error) {
	cf := commonFlags{Config: helper.Config{
		MinerURL: "https://drops.ryuzec.dev",
		Password: os.Getenv("MINER_PASSWORD"),
	}}
	fs.StringVar(&cf.MinerURL, "miner", cf.MinerURL, "base URL of the miner")
	fs.StringVar(&cf.Password, "password", cf.Password, "admin password")
	fs.StringVar(&cf.Browser, "browser", "", "limit cookie search to this browser")
	fs.BoolVar(&cf.Insecure, "insecure", false, "skip TLS verification")
	if extra != nil {
		extra(fs)
	}
	if err := fs.Parse(args); err != nil {
		return cf, nil, err
	}
	if cf.Password == "" {
		return cf, nil, fmt.Errorf("missing --password (or MINER_PASSWORD env)")
	}
	return cf, fs.Args(), nil
}

func runTwitch(args []string) error {
	fs := flag.NewFlagSet("twitch", flag.ContinueOnError)
	cf, rest, err := parseCommon(fs, args, nil)
	if err != nil {
		return err
	}
	if len(rest) != 1 {
		return fmt.Errorf("twitch requires exactly one account-id argument")
	}
	res, err := helper.PushTwitch(context.Background(), helper.TwitchRequest{Config: cf.Config, AccountID: rest[0]})
	if err != nil {
		return err
	}
	fmt.Printf("✓ %s (%v)\n", res.Message, res.UploadedCookies)
	return nil
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
	if len(channels) == 0 {
		return fmt.Errorf("--channel is required (one or more, e.g. --channel a,b or --channel a --channel b)")
	}
	res, err := helper.PushKick(context.Background(), helper.KickRequest{Config: cf.Config, AccountID: rest[0], Channels: channels})
	if err != nil {
		return err
	}
	fmt.Printf("✓ %s\n", res.Message)
	return nil
}
