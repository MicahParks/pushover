// Command callback sends an emergency-priority message and receives the acknowledgment callback.
package main

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"log"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/MicahParks/pushover"
)

func main() {
	ctx := context.Background()

	client, err := pushover.New(os.Getenv("PUSHOVER_TOKEN"), pushover.Options{})
	if err != nil {
		log.Fatalf("Failed to create Pushover client.\nError: %s", err)
	}

	// Pushover does not sign callback requests, so include a secret in the callback URL and check it.
	secret := rand.Text()
	callbackURL, err := url.Parse(os.Getenv("PUBLIC_URL") + "/pushover/callback")
	if err != nil {
		log.Fatalf("Failed to parse callback URL.\nError: %s", err)
	}
	callbackURL.RawQuery = url.Values{"secret": {secret}}.Encode()

	callback := pushover.CallbackHandler(func(ctx context.Context, cb pushover.Callback) error {
		log.Printf("Receipt %s acknowledged by %s on %s at %s.", cb.Receipt, cb.AcknowledgedBy, cb.AcknowledgedByDevice, cb.AcknowledgedAt)
		return nil
	})
	mux := http.NewServeMux()
	mux.HandleFunc("POST /pushover/callback", func(w http.ResponseWriter, r *http.Request) {
		if subtle.ConstantTimeCompare([]byte(r.URL.Query().Get("secret")), []byte(secret)) != 1 {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		callback.ServeHTTP(w, r)
	})
	server := &http.Server{
		Addr:              ":8080",
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
	}
	go func() {
		log.Fatal(server.ListenAndServe())
	}()

	msg := pushover.Message{
		User:     os.Getenv("PUSHOVER_USER"),
		Message:  "Disk usage on db01 is at 95%.",
		Priority: pushover.PriorityEmergency,
		Retry:    5 * time.Minute,
		Expire:   time.Hour,
		Callback: callbackURL.String(),
	}
	resp, err := client.Send(ctx, msg)
	if err != nil {
		log.Fatalf("Failed to send Pushover message.\nError: %s", err)
	}
	log.Printf("Sent emergency message with receipt %s. Waiting for acknowledgment.", resp.Receipt)
	select {}
}
