// Command basic sends a message with common options.
package main

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/MicahParks/pushover"
)

func main() {
	// Create a client with the application's API token.
	client, err := pushover.New(os.Getenv("PUSHOVER_TOKEN"), pushover.Options{})
	if err != nil {
		log.Fatalf("Failed to create Pushover client.\nError: %s", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Send a message to a user or group key.
	msg := pushover.Message{
		User:     os.Getenv("PUSHOVER_USER"),
		Message:  "Backup of database \"example\" finished in 16 minutes.",
		Title:    "Backup finished",
		Priority: pushover.PriorityLow,
		Sound:    pushover.SoundCashRegister,
		URL:      "https://example.com/backups",
		URLTitle: "View backups",
		TTL:      24 * time.Hour,
	}
	resp, err := client.Send(ctx, msg)
	if err != nil {
		log.Fatalf("Failed to send Pushover message.\nError: %s", err)
	}
	log.Printf("Sent message. Request ID: %s", resp.Request)
	if resp.Limits != nil {
		log.Printf("%d of %d messages remaining this month.", resp.Limits.Remaining, resp.Limits.Limit)
	}
}
