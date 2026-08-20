// Package mail sends outbound email over the shared SMTP relay settings
// (Settings.Smtp*/EmailSender, edited on the admin UI's "Email" destination
// tab, projected into util.CommonConfig by util.NewCommonConfig). Extracted
// from cmd/icinga-notifications, the first consumer, so the web package's
// password-reset flow can send mail without duplicating this logic.
package mail

import (
	"fmt"

	"github.com/abundo/factum2/internal/util"
	"github.com/wneessen/go-mail"
)

// Send sends body (already-rendered HTML) via the shared SMTP relay
// settings in smtp.
func Send(smtp util.CommonConfig, from, to, subject, body string) error {
	if smtp.SmtpHost == "" {
		return fmt.Errorf("no SMTP host configured (Settings.SmtpHost, admin UI Destinations > Email tab)")
	}

	m := mail.NewMsg()
	if err := m.From(from); err != nil {
		return fmt.Errorf("invalid from address %q: %w", from, err)
	}
	if err := m.To(to); err != nil {
		return fmt.Errorf("invalid to address %q: %w", to, err)
	}
	m.Subject(subject)
	m.SetBodyString(mail.TypeTextHTML, body)

	var opts []mail.Option
	if smtp.SmtpPort != 0 {
		opts = append(opts, mail.WithPort(int(smtp.SmtpPort)))
	}
	switch smtp.SmtpTLSMode {
	case "tls":
		opts = append(opts, mail.WithSSL())
	case "starttls":
		opts = append(opts, mail.WithTLSPolicy(mail.TLSMandatory))
	default:
		opts = append(opts, mail.WithTLSPolicy(mail.NoTLS))
	}
	if smtp.SmtpUser != "" {
		opts = append(opts, mail.WithSMTPAuth(mail.SMTPAuthAutoDiscover),
			mail.WithUsername(smtp.SmtpUser), mail.WithPassword(smtp.SmtpPass))
	}

	client, err := mail.NewClient(smtp.SmtpHost, opts...)
	if err != nil {
		return fmt.Errorf("creating smtp client: %w", err)
	}
	return client.DialAndSend(m)
}
