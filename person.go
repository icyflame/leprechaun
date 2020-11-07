package main

import (
	"crypto/sha256"
	"fmt"
	"log"
	"math/rand"
	"os"
	"time"

	sg "github.com/sendgrid/sendgrid-go"
	"github.com/sendgrid/sendgrid-go/helpers/mail"
)

type Person struct {
	// ID bson.ObjectID `bson:"_id,omitempty"`
	Roll             string
	Email            string
	Verifier         string
	EmailToken       string
	LinkSuffix       string
	Step1Complete    bool
	Step1CompletedAt time.Time
	Step2Complete    bool
	Step2CompletedAt time.Time
}

// ENHANCE: Improve the generation of the random seeds
func GetPerson(roll string, email string) Person {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))

	base := fmt.Sprintf("%s %s %v", roll, email, time.Now().UnixNano())

	h := sha256.New()

	h.Write([]byte(base))

	h.Write([]byte(fmt.Sprintf("%d", r.Uint64())))
	link_suffix := fmt.Sprintf("%x", h.Sum(nil))

	h.Write([]byte(fmt.Sprintf("%d", r.Uint64())))
	verifier := fmt.Sprintf("%x", h.Sum(nil))

	h.Write([]byte(fmt.Sprintf("%d", r.Uint64())))
	email_tok := fmt.Sprintf("%x", h.Sum(nil))

	return Person{
		roll,
		email,
		verifier[:HASH_LEN],
		email_tok[:HASH_LEN],
		link_suffix[:HASH_LEN],
		false,
		time.Now(),
		false,
		time.Now(),
	}
}

// ENHANCE: Make the link clickable using HTML content with appropriate markup
func SendVerificationEmail(email string, subject string, suffix string) {
	from := mail.NewEmail(os.Getenv("FROM_NAME"), os.Getenv("FROM_EMAIL"))

	to := mail.NewEmail("", email)

	plainTextContent := fmt.Sprintf("Please visit this URL in a web browser: %s/%s", os.Getenv("BASE_LINK"), suffix)

	message := mail.NewSingleEmail(from, subject, to, plainTextContent, plainTextContent)

	client := sg.NewSendClient(os.Getenv("SENDGRID_API_KEY"))
	response, err := client.Send(message)
	if err != nil {
		log.Println(err)
	} else {
		log.Println(response.StatusCode)
		log.Printf("Email sent to %s successfully!", email)
	}
}
