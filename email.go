package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/resend/resend-go/v2"
	"github.com/rs/zerolog/log"
)

func SendMail(receipientEmail, fullName string) error {
	log.Info().Str("recipient", receipientEmail).Msg("Preparing to send email")
	resendApiKey := os.Getenv("RESEND_API_KEY")
	if resendApiKey == "" {
		log.Error().Msg("RESEND_API_KEY is empty")
		return errors.New("RESEND_API_KEY is empty")
	}
	client := resend.NewClient(resendApiKey)

	blueprintBytes, err := os.ReadFile("./assets/Blueprint.pdf")

	if err != nil {
		log.Error().Err(err).Msg("Failed to read attachment file")
		return err
	}

	attachment := resend.Attachment{Content: blueprintBytes, Filename: "Blueprint.pdf"}

	htmlBody := fmt.Sprintf("Hallo %v, <br><br> viel Erfolg bei der Automatisierung deiner Kanzlei! <br><br> Beste Grüße <br><br> Furkan Asani <br><br>", fullName)
	params := &resend.SendEmailRequest{
		From:        "AF Automation<info@af-automation-systems.com>",
		To:          []string{receipientEmail},
		Html:        htmlBody,
		Subject:     "Ihr Blueprint für Ihre Kanzlei",
		Cc:          []string{},
		Bcc:         []string{"info@af-automation-systems.com"},
		ReplyTo:     "info@af-automation-systems.com",
		Attachments: []*resend.Attachment{&attachment},
	}

	log.Info().Msg("Sending email via Resend")
	sent, err := client.Emails.Send(params)
	if err != nil {
		log.Error().Err(err).Msg("Failed to send email")
		return err
	}

	log.Info().Str("message_id", sent.Id).Str("recipient", receipientEmail).Msg("Email sent successfully")
	return nil
}