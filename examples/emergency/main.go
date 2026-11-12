// Command emergency sends an emergency-priority message and polls its receipt until it is acknowledged.
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

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	// Emergency-priority messages repeat every Retry until acknowledged or until Expire elapses.
	msg := pushover.Message{
		User:     os.Getenv("PUSHOVER_USER"),
		Message:  "Server mail01 failed to respond to 3 pings.",
		Title:    "mail01 is down",
		Priority: pushover.PriorityEmergency,
		Retry:    time.Minute,
		Expire:   30 * time.Minute,
		Tags:     []string{"s=mail01", "l=chicago"},
	}
	resp, err := client.Send(ctx, msg)
	if err != nil {
		log.Fatalf("Failed to send Pushover message.\nError: %s", err)
	}
	log.Printf("Sent emergency message. Receipt: %s", resp.Receipt)

	// Poll the receipt until the message is acknowledged or expires. Pushover asks that receipts are polled no more
	// than once every 5 seconds. Alternatively, set Message.Callback, see the callback example.
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			// Stop the retries if we're giving up. Canceling by tag also works without storing the receipt.
			err = client.CancelReceipt(context.Background(), resp.Receipt)
			if err != nil {
				log.Fatalf("Failed to cancel emergency message.\nError: %s", err)
			}
			log.Println("Canceled emergency message.")
			return
		case <-ticker.C:
		}
		receipt, err := client.Receipt(ctx, resp.Receipt)
		if err != nil {
			log.Printf("Failed to get receipt.\nError: %s", err)
			continue
		}
		switch {
		case receipt.Acknowledged:
			log.Printf("Acknowledged by %s on %s at %s.", receipt.AcknowledgedBy, receipt.AcknowledgedByDevice, receipt.AcknowledgedAt)
			return
		case receipt.Expired:
			log.Printf("Expired at %s without acknowledgment.", receipt.ExpiresAt)
			return
		}
	}
}
