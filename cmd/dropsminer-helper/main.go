// dropsminer-helper is a small CLI that copies cookies from the
// user's local browser into the dropsminer deployment.
//
// Usage:
//
//	dropsminer-helper twitch <account-id> [flags]
//	dropsminer-helper kick   <account-id> --channel STREAMER [flags]
//
// Flags:
//
//	--miner    URL    Base URL of the miner (default https://rdrops.ryuzec.dev)
//	--password STR    Admin password. Falls back to MINER_PASSWORD env.
//	--browser  NAME   Limit cookie search to a specific browser.
//	--insecure        Skip TLS verification (debug only).
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"

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
  dropsminer-helper kick   <account-id> [--miner URL] [--password PW] [--browser NAME] --channel NAME

Flags:
  --miner     base URL of the miner (default https://rdrops.ryuzec.dev)
  --password  admin password (or set MINER_PASSWORD)
  --browser   limit cookie search to this browser (chrome, firefox, safari, ...)
  --channel   kick channel to mine (kick only, required)
  --insecure  skip TLS verification

`)
}

type commonFlags struct {
	helper.Config
}

func parseCommon(fs *flag.FlagSet, args []string, extra func(*flag.FlagSet)) (commonFlags, []string, error) {
	cf := commonFlags{Config: helper.Config{
		MinerURL: "https://rdrops.ryuzec.dev",
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

func runKick(args []string) error {
	fs := flag.NewFlagSet("kick", flag.ContinueOnError)
	var channel string
	cf, rest, err := parseCommon(fs, args, func(fs *flag.FlagSet) {
		fs.StringVar(&channel, "channel", "", "kick channel to mine (required)")
	})
	if err != nil {
		return err
	}
	if len(rest) != 1 {
		return fmt.Errorf("kick requires exactly one account-id argument")
	}
	res, err := helper.PushKick(context.Background(), helper.KickRequest{Config: cf.Config, AccountID: rest[0], Channel: channel})
	if err != nil {
		return err
	}
	fmt.Printf("✓ %s\n", res.Message)
	return nil
}
