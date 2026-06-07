package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/aalejandrofer/grubdrops/internal/helper"
)

// runInteractive is the no-args path: a Windows/macOS user double-clicked
// the binary, so there's no terminal command line. Instead of printing
// usage and exiting (which makes the console window flash and vanish), we
// prompt for everything and keep the window open until they press Enter.
func runInteractive() error {
	in := bufio.NewReader(os.Stdin)

	fmt.Println("GrubDrops cookie helper")
	fmt.Println("-----------------------")
	fmt.Println("Pushes your local browser's kick.com / twitch.tv cookies to your miner.")
	fmt.Println()

	platform := prompt(in, "Platform (kick / twitch)", "kick")
	platform = strings.ToLower(strings.TrimSpace(platform))
	if platform != "kick" && platform != "twitch" {
		return fmt.Errorf("platform must be kick or twitch, got %q", platform)
	}

	accountID := prompt(in, "Account ID (from the miner's login page URL)", "")
	if strings.TrimSpace(accountID) == "" {
		return fmt.Errorf("account ID is required")
	}

	minerURL := prompt(in, "Miner URL", "https://drops.ryuzec.dev")
	password := prompt(in, "Admin password", os.Getenv("MINER_PASSWORD"))
	if strings.TrimSpace(password) == "" {
		return fmt.Errorf("admin password is required")
	}

	cfg := helper.Config{
		MinerURL: strings.TrimSpace(minerURL),
		Password: password,
	}

	switch platform {
	case "twitch":
		res, err := helper.PushTwitch(context.Background(), helper.TwitchRequest{
			Config: cfg, AccountID: strings.TrimSpace(accountID),
		})
		if err != nil {
			return err
		}
		fmt.Printf("\n✓ %s (%v)\n", res.Message, res.UploadedCookies)
	case "kick":
		raw := prompt(in, "Kick channel(s) to mine (comma-separated)", "")
		channels := splitChannels(raw)
		if len(channels) == 0 {
			return fmt.Errorf("at least one channel is required")
		}
		res, err := helper.PushKick(context.Background(), helper.KickRequest{
			Config: cfg, AccountID: strings.TrimSpace(accountID), Channels: channels,
		})
		if err != nil {
			return err
		}
		fmt.Printf("\n✓ %s\n", res.Message)
	}
	return nil
}

// prompt reads one line, showing a default in [brackets] when present.
// An empty answer keeps the default.
func prompt(in *bufio.Reader, label, def string) string {
	if def != "" {
		fmt.Printf("%s [%s]: ", label, def)
	} else {
		fmt.Printf("%s: ", label)
	}
	line, _ := in.ReadString('\n')
	line = strings.TrimRight(line, "\r\n")
	if strings.TrimSpace(line) == "" {
		return def
	}
	return line
}

// pause waits for Enter so a double-clicked console window stays open long
// enough to read the result (or the error). No-op when stdin isn't a TTY.
func pause() {
	fmt.Print("\nPress Enter to close...")
	bufio.NewReader(os.Stdin).ReadString('\n')
}
