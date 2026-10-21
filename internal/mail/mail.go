// Package mail sends outbound email over the shared SMTP relay settings
// (Settings.Smtp*/EmailSender, edited on the admin UI's Factum > Email
// tab, projected into util.CommonConfig by util.NewCommonConfig). Extracted
// from cmd/icinga-notifications, the first consumer, so the web package's
// password-reset flow can send mail without duplicating this logic.
package mail

import (
	"fmt"
	"strings"

	"github.com/abundo/factum2/internal/util"
	"github.com/wneessen/go-mail"
)

// ParseRecipients splits textarea content into addresses, one per line
// and/or comma-separated within a line. Blank pieces are dropped.
// Repeated addresses are kept once, compared case-insensitively.
func ParseRecipients(raw string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, line := range strings.Split(raw, "\n") {
		for _, addr := range strings.Split(line, ",") {
			addr = strings.TrimSpace(addr)
			if addr == "" {
				continue
			}
			key := strings.ToLower(addr)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, addr)
		}
	}
	return out
}

// Send sends body (already-rendered HTML) via the shared SMTP relay
// settings in smtp.
func Send(smtp util.CommonConfig, from, to, subject, body string) error {
	if smtp.SmtpHost == "" {
		return fmt.Errorf("no SMTP host configured (Settings.SmtpHost, admin UI Factum > Email tab)")
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
