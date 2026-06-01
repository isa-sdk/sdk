// send_and_await is a runnable end-to-end example for the rapidsign Go
// SDK. It creates a signing envelope, polls until the signature is
// captured, and writes the resulting PDF to disk.
//
//	RAPIDSIGN_TOKEN=isa_live_... go run ./examples/send_and_await
//
// The packet URL and recipient email are hard-coded for demonstration;
// adapt them to your own staging fixtures before running.
package main

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/isa-sdk/sdk/rapidsign"
)

func main() {
	token := os.Getenv("RAPIDSIGN_TOKEN")
	if len(token) == 0 {
		log.Fatal("RAPIDSIGN_TOKEN must be set")
	}

	rs, err := rapidsign.New(token)
	if err != nil {
		log.Fatal("rapidsign.New failed")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Hour)
	defer cancel()

	env, err := rs.Documents.Send(ctx, &rapidsign.SendRequest{
		Packet: []rapidsign.PdfSource{
			{URL: "https://docs.example.com/contract.pdf"},
		},
		Recipient: rapidsign.Recipient{
			Email: "signer@example.com",
			Name:  "Jane Signer",
		},
		LegalText: "I agree to the terms above.",
		Metadata:  map[string]string{"applicationId": "app_1234"},
	})
	if err != nil {
		log.Fatal("Send failed")
	}
	log.Println("envelope created")

	if _, err := rs.Documents.AwaitSignature(ctx, env.SignID, rapidsign.AwaitOpts{Timeout: 24 * time.Hour}); err != nil {
		log.Fatal("AwaitSignature failed")
	}
	log.Println("signature captured")

	pdf, err := rs.Documents.Download(ctx, env.SignID)
	if err != nil {
		log.Fatal("Download failed")
	}
	const out = "signed-output.pdf"
	if err := os.WriteFile(out, pdf, 0o600); err != nil {
		log.Fatal("WriteFile failed")
	}
	log.Println("wrote signed-output.pdf")
}
