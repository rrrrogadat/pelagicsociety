package mail

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sesv2"
	sestypes "github.com/aws/aws-sdk-go-v2/service/sesv2/types"
)

// Mailer sends transactional email via AWS SESv2. If Enabled is false
// (constructor couldn't load AWS credentials), Send logs and returns nil —
// useful for local dev.
type Mailer struct {
	client  *sesv2.Client
	from    string
	replyTo string
	enabled bool
}

// New builds a Mailer. If AWS credentials cannot be resolved (no IAM role,
// no env/shared creds), it returns a log-only Mailer — Send never errors in
// that case. Region is picked up from AWS_REGION, the shared config, or EC2
// instance metadata, in that order.
func New(ctx context.Context, from, replyTo string) *Mailer {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	cfg, err := awsconfig.LoadDefaultConfig(ctx)
	if err != nil {
		log.Printf("mail: AWS config load failed, running in log-only mode: %v", err)
		return &Mailer{from: from, replyTo: replyTo}
	}
	// Probe credentials so we can warn early and fall back to log-only
	// instead of failing every request.
	if _, err := cfg.Credentials.Retrieve(ctx); err != nil {
		log.Printf("mail: no AWS credentials available, running in log-only mode: %v", err)
		return &Mailer{from: from, replyTo: replyTo}
	}
	return &Mailer{
		client:  sesv2.NewFromConfig(cfg),
		from:    from,
		replyTo: replyTo,
		enabled: true,
	}
}

type Message struct {
	To      []string
	Subject string
	Text    string
	HTML    string
	ReplyTo string
}

func (m *Mailer) Send(ctx context.Context, msg Message) error {
	if !m.enabled {
		log.Printf("mail (log-only): to=%v subject=%q text_len=%d", msg.To, msg.Subject, len(msg.Text))
		return nil
	}
	if m.from == "" {
		return errors.New("mail: From address not configured")
	}
	if len(msg.To) == 0 {
		return errors.New("mail: no recipients")
	}

	replyTo := msg.ReplyTo
	if replyTo == "" {
		replyTo = m.replyTo
	}

	body := &sestypes.Body{}
	if msg.Text != "" {
		body.Text = &sestypes.Content{Data: aws.String(msg.Text), Charset: aws.String("UTF-8")}
	}
	if msg.HTML != "" {
		body.Html = &sestypes.Content{Data: aws.String(msg.HTML), Charset: aws.String("UTF-8")}
	}

	input := &sesv2.SendEmailInput{
		FromEmailAddress: aws.String(m.from),
		Destination:      &sestypes.Destination{ToAddresses: msg.To},
		Content: &sestypes.EmailContent{
			Simple: &sestypes.Message{
				Subject: &sestypes.Content{Data: aws.String(msg.Subject), Charset: aws.String("UTF-8")},
				Body:    body,
			},
		},
	}
	if replyTo != "" {
		input.ReplyToAddresses = []string{replyTo}
	}

	_, err := m.client.SendEmail(ctx, input)
	return err
}
