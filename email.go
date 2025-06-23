package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/resend/resend-go/v2"
)

func SendMail(receipientEmail string) error {
	resendApiKey := os.Getenv("RESEND_API_KEY")
	if resendApiKey == ""{
		return errors.New("RESEND_API_KEY is empty")
	}
	client := resend.NewClient(resendApiKey)
	
	 blueprintBytes, err := os.ReadFile("./assets/Blueprint.pdf")

	 if err != nil {
		fmt.Println("Failed to read the file")
		return err
	 }

	attachment := resend.Attachment{Content: blueprintBytes, Filename: "Blueprint.pdf"}

	 params := &resend.SendEmailRequest{
			From:    "Acme <info@af-automation-system.com>",
			To:      []string{receipientEmail},
			Html:    "<strong>Ihr Blueprint für Ihre Kanzlei</strong>",
			Subject: "Ihr Blueprint für Ihre Kanzlei",
			Cc:      []string{},
			Bcc:     []string{"info@af-automation-systems.com"},
			ReplyTo: "info@af-automation-systems.com",
			Attachments: []*resend.Attachment{&attachment},
		}
	
		sent, err := client.Emails.Send(params)
		if err != nil {
			fmt.Println(err.Error())
			return err
		}

		fmt.Println(sent.Id)
		return nil
}