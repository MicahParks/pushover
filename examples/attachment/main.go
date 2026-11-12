// Command attachment sends a message with an image attachment.
package main

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/MicahParks/pushover"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	if len(os.Args) != 2 {
		log.Fatalf("Usage: %s image.jpg", os.Args[0])
	}
	data, err := os.ReadFile(os.Args[1])
	if err != nil {
		log.Fatalf("Failed to read image.\nError: %s", err)
	}

	client, err := pushover.New(os.Getenv("PUSHOVER_TOKEN"), pushover.Options{})
	if err != nil {
		log.Fatalf("Failed to create Pushover client.\nError: %s", err)
	}

	// Attachments are limited to pushover.MaxAttachmentSize bytes. The content type is detected if not given.
	msg := pushover.Message{
		User:    os.Getenv("PUSHOVER_USER"),
		Message: "Motion detected at the front door.",
		Attachment: &pushover.Attachment{
			Data:     data,
			Filename: "front-door.jpg",
		},
	}
	_, err = client.Send(ctx, msg)
	if err != nil {
		log.Fatalf("Failed to send Pushover message.\nError: %s", err)
	}
	log.Println("Sent message with attachment.")
}
