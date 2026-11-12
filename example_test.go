package pushover_test

import (
	"context"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/MicahParks/pushover"
)

func Example() {
	ctx := context.Background()

	client, err := pushover.New("azGDORePK8gMaC0QOYAMyEEuzJnyUi", pushover.Options{})
	if err != nil {
		log.Fatalf("Failed to create Pushover client.\nError: %s", err)
	}

	msg := pushover.Message{
		User:    "uQiRzpo4DXghDmr9QzzfQu27cmVRsG",
		Message: "Backup of database \"example\" finished in 16 minutes.",
		Title:   "Backup finished",
	}
	_, err = client.Send(ctx, msg)
	if err != nil {
		log.Fatalf("Failed to send Pushover message.\nError: %s", err)
	}
}

func ExampleClient_Send_emergency() {
	ctx := context.Background()
	client, err := pushover.New("azGDORePK8gMaC0QOYAMyEEuzJnyUi", pushover.Options{})
	if err != nil {
		log.Fatalf("Failed to create Pushover client.\nError: %s", err)
	}

	msg := pushover.Message{
		User:     "uQiRzpo4DXghDmr9QzzfQu27cmVRsG",
		Message:  "Server mail01 failed to respond to 3 pings.",
		Priority: pushover.PriorityEmergency,
		Retry:    time.Minute,
		Expire:   30 * time.Minute,
		Tags:     []string{"s=mail01"},
	}
	resp, err := client.Send(ctx, msg)
	if err != nil {
		log.Fatalf("Failed to send Pushover message.\nError: %s", err)
	}

	// Later, check whether the message was acknowledged, or stop the retries.
	receipt, err := client.Receipt(ctx, resp.Receipt)
	if err != nil {
		log.Fatalf("Failed to get receipt.\nError: %s", err)
	}
	if !receipt.Acknowledged {
		err = client.CancelByTag(ctx, "s=mail01")
		if err != nil {
			log.Fatalf("Failed to cancel emergency messages.\nError: %s", err)
		}
	}
}

func ExampleOptions() {
	// Retry server and network errors twice, waiting 10 seconds between attempts.
	//
	// To also limit the request rate, set Limiter. A *rate.Limiter from golang.org/x/time/rate implements
	// pushover.Limiter, for example: rate.NewLimiter(rate.Every(time.Second), 1)
	options := pushover.Options{
		HTTPClient: &http.Client{Timeout: 30 * time.Second},
		RetryMax:   2,
		RetryWait:  10 * time.Second,
	}
	_, err := pushover.New("azGDORePK8gMaC0QOYAMyEEuzJnyUi", options)
	if err != nil {
		log.Fatalf("Failed to create Pushover client.\nError: %s", err)
	}
}

func ExampleAPIError() {
	client, err := pushover.New("azGDORePK8gMaC0QOYAMyEEuzJnyUi", pushover.Options{})
	if err != nil {
		log.Fatalf("Failed to create Pushover client.\nError: %s", err)
	}
	_, err = client.Send(context.Background(), pushover.Message{User: "uQiRzpo4DXghDmr9QzzfQu27cmVRsG", Message: "Hello"})

	var apiErr *pushover.APIError
	switch {
	case err == nil:
	case errors.Is(err, pushover.ErrInvalidRequest):
		log.Printf("The message is invalid and was not sent: %s", err)
	case errors.Is(err, pushover.ErrQuotaExceeded):
		log.Printf("The monthly message limit was reached: %s", err)
	case errors.As(err, &apiErr) && !apiErr.Temporary():
		log.Printf("Pushover rejected the message: %v. Request ID: %s", apiErr.Errors, apiErr.Request)
	default:
		log.Printf("Temporary failure, try again later: %s", err)
	}
}

func ExampleNewNop() {
	enabled := false

	sender := pushover.NewNop()
	if enabled {
		client, err := pushover.New("azGDORePK8gMaC0QOYAMyEEuzJnyUi", pushover.Options{})
		if err != nil {
			log.Fatalf("Failed to create Pushover client.\nError: %s", err)
		}
		sender = client
	}

	_, err := sender.Send(context.Background(), pushover.Message{User: "uQiRzpo4DXghDmr9QzzfQu27cmVRsG", Message: "Hello"})
	if err != nil {
		log.Fatalf("Failed to send Pushover message.\nError: %s", err)
	}
}
