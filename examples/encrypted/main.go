// Command encrypted sends an end-to-end encrypted message.
package main

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/MicahParks/pushover"
)

func main() {
	client, err := pushover.New(os.Getenv("PUSHOVER_TOKEN"), pushover.Options{})
	if err != nil {
		log.Fatalf("Failed to create Pushover client.\nError: %s", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// The end-to-end encryption key is the 64-character hexadecimal key configured in the recipient's Pushover app.
	// Use pushover.NewEncryptionKey to generate one, and key.Hex() to get the value to enter in the app.
	key, err := pushover.ParseEncryptionKey(os.Getenv("PUSHOVER_E2EE_KEY"))
	if err != nil {
		log.Fatalf("Failed to parse end-to-end encryption key.\nError: %s", err)
	}

	// The message, title, URL, and URL title are encrypted locally before they're sent to Pushover.
	msg := pushover.Message{
		User:          os.Getenv("PUSHOVER_USER"),
		Message:       "The vault combination is 12-34-56.",
		Title:         "Top secret",
		EncryptionKey: key,
	}
	_, err = client.Send(ctx, msg)
	if err != nil {
		log.Fatalf("Failed to send Pushover message.\nError: %s", err)
	}
	log.Println("Sent end-to-end encrypted message.")
}
