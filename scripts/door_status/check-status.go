package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	seamgo "github.com/seamapi/go"
	goclient "github.com/seamapi/go/client"
)

type discordField struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Inline bool   `json:"inline"`
}

type discordEmbed struct {
	Title       string         `json:"title,omitempty"`
	Description string         `json:"description,omitempty"`
	Color       int            `json:"color,omitempty"`
	Fields      []discordField `json:"fields,omitempty"`
}

type discordPayload struct {
	Content  string         `json:"content,omitempty"`
	Username string         `json:"username,omitempty"`
	Embeds   []discordEmbed `json:"embeds,omitempty"`
}

type cachedStatus struct {
	Locked bool `json:"locked"`
}

const cacheFile = ".door_status_cache/last_status.json"

func readCachedStatus() (*cachedStatus, error) {
	f, err := os.Open(cacheFile)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}
	var cs cachedStatus
	if err := json.Unmarshal(b, &cs); err != nil {
		return nil, err
	}
	return &cs, nil
}

func writeCachedStatus(locked bool) error {
	if err := os.MkdirAll(filepath.Dir(cacheFile), 0o755); err != nil {
		return err
	}
	b, err := json.Marshal(cachedStatus{Locked: locked})
	if err != nil {
		return err
	}
	return os.WriteFile(cacheFile, b, 0o644)
}

func main() {
	seamApiKey := os.Getenv("SEAM_API_KEY")
	discordWebhook := os.Getenv("DISCORD_WEBHOOK_URL")
	lockDeviceID := os.Getenv("LOCK_DEVICE_ID")
	if seamApiKey == "" || discordWebhook == "" || lockDeviceID == "" {
		fmt.Fprintln(os.Stderr, "Missing environment variable: SEAM_API_KEY, DISCORD_WEBHOOK_URL, or LOCK_DEVICE_ID")
		os.Exit(1)
	}

	client := goclient.NewClient(goclient.WithApiKey(seamApiKey))
	ctx := context.Background()

	deviceList, err := client.Devices.List(ctx, &seamgo.DevicesListRequest{})
	if err != nil {
		fmt.Fprintln(os.Stderr, "Failed to list devices from Seam API:", err)
		os.Exit(1)
	}
	var target *seamgo.Device
	for _, d := range deviceList {
		if d.DeviceId == lockDeviceID {
			target = d
			break
		}
	}
	if target == nil {
		fmt.Fprintln(os.Stderr, "Device not found for provided LOCK_DEVICE_ID")
		os.Exit(1)
	}
	if target.Properties == nil || target.Properties.Locked == nil {
		fmt.Fprintln(os.Stderr, "Could not determine lock status from device properties.")
		os.Exit(1)
	}
	locked := *target.Properties.Locked
	fmt.Println("Locked:", locked)

	if cs, err := readCachedStatus(); err == nil {
		if cs.Locked == locked {
			os.Exit(0)
		}
	}

	var embed discordEmbed
	if locked {
		embed = discordEmbed{
			Title:       "Space is Closed.",
			Description: "🦊 No one is currently at the place.",
			Color:       0xFF0000,
			Fields: []discordField{
				{Name: "Status", Value: "**CLOSED**", Inline: true},
			},
		}
	} else {
		embed = discordEmbed{
			Title:       "Space is Open!",
			Description: "🦊 Happy Hacking!",
			Color:       0x00FF00,
			Fields: []discordField{
				{Name: "Status", Value: "**OPEN**", Inline: true},
			},
		}
	}
	payload := discordPayload{
		Username: "HackerBell",
		Embeds:   []discordEmbed{embed},
	}

	msg, err := json.Marshal(payload)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Failed to encode Discord payload:", err)
		os.Exit(1)
	}
	dreq, err := http.NewRequest("POST", discordWebhook, bytes.NewBuffer(msg))
	if err != nil {
		fmt.Fprintln(os.Stderr, "Failed to build request to Discord webhook:", err)
		os.Exit(1)
	}
	dreq.Header.Set("Content-Type", "application/json")
	httpClient := &http.Client{}
	dresp, err := httpClient.Do(dreq)
	if err != nil || dresp.StatusCode >= 400 {
		fmt.Fprintln(os.Stderr, "Failed to send message to Discord webhook:", err)
		os.Exit(1)
	}

	if err := writeCachedStatus(locked); err != nil {
		fmt.Fprintln(os.Stderr, "Failed to update cached status:", err)
		os.Exit(1)
	}
}
