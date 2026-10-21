package lime

import (
	"fmt"
	"html"
	"strings"

	"github.com/abundo/factum2/internal/jobevent"
	"github.com/abundo/factum2/internal/mail"
	"github.com/abundo/factum2/internal/util"
	"github.com/abundo/factum2/models"
)

// sendReport is mail.Send, replaced in tests.
var sendReport = mail.Send

type changeAction string

const (
	changeAdded   changeAction = "added"
	changeUpdated changeAction = "updated"
	changeRemoved changeAction = "removed"
)

// syncChange is one Lime-owned row the sync added, edited, or removed.
// A resync that only bumps LastSync is not a change.
type syncChange struct {
	Action changeAction
	Kind   string // Customer, Service, Contact
	Label  string
	Detail string
}

func (lime *Lime) note(action changeAction, kind, label, detail string) {
	lime.changes = append(lime.changes, syncChange{
		Action: action,
		Kind:   kind,
		Label:  label,
		Detail: detail,
	})
	if lime.reporter == nil {
		return
	}
	msg := label
	if detail != "" {
		msg += " — " + detail
	}
	lime.reporter.Emit(jobevent.Info, "Lime %s %s: %s", action, strings.ToLower(kind), msg)
}

type fieldPair struct {
	name string
	old  string
	new  string
}

func joinDiffs(pairs []fieldPair) string {
	var parts []string
	for _, p := range pairs {
		if p.old == p.new {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s: %s → %s", p.name, displayValue(p.old), displayValue(p.new)))
	}
	return strings.Join(parts, "; ")
}

func displayValue(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func joinParts(parts ...string) string {
	var out []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, " · ")
}

func customerDetail(c *models.Customer) string {
	return joinParts(c.OrganizationNumber, c.Postaladdress1, c.Postalzipcode, c.Postalcity, c.Country)
}

func customerDiff(old, row *models.Customer) string {
	return joinDiffs([]fieldPair{
		{"Name", old.Name, row.Name},
		{"Address 1", old.Postaladdress1, row.Postaladdress1},
		{"Address 2", old.Postaladdress2, row.Postaladdress2},
		{"Zip", old.Postalzipcode, row.Postalzipcode},
		{"City", old.Postalcity, row.Postalcity},
		{"Country", old.Country, row.Country},
		{"Organization number", old.OrganizationNumber, row.OrganizationNumber},
	})
}

func serviceChangeLabel(serviceID, sourceID, customer string) string {
	label := serviceID
	if label == "" {
		label = "lime " + sourceID
	}
	if customer == "" {
		return label
	}
	return label + " (" + customer + ")"
}

func serviceDetail(s *models.Service) string {
	points := s.DeliveryPoint1
	if s.DeliveryPoint2 != "" {
		if points != "" {
			points += " — " + s.DeliveryPoint2
		} else {
			points = s.DeliveryPoint2
		}
	}
	return joinParts(s.Product, s.Service, s.AgreementStatus, s.Comment, points)
}

func (lime *Lime) serviceDiff(old, row *models.Service) string {
	pairs := []fieldPair{
		{"Service ID", old.ServiceID, row.ServiceID},
		{"Comment", old.Comment, row.Comment},
		{"Delivery point 1", old.DeliveryPoint1, row.DeliveryPoint1},
		{"Delivery point 2", old.DeliveryPoint2, row.DeliveryPoint2},
		{"Product", old.Product, row.Product},
		{"Service", old.Service, row.Service},
		{"Agreement status", old.AgreementStatus, row.AgreementStatus},
	}
	if old.CustomerID != row.CustomerID {
		pairs = append(pairs, fieldPair{
			"Customer", lime.customerName(old.CustomerID), lime.customerName(row.CustomerID),
		})
	}
	return joinDiffs(pairs)
}

func contactChangeLabel(name, customer string) string {
	if customer == "" {
		return name
	}
	return name + " (" + customer + ")"
}

func contactDetail(c *models.Contact) string {
	return joinParts(c.Email, c.Phone)
}

func contactDiff(old, row *models.Contact) string {
	return joinDiffs([]fieldPair{
		{"Name", old.Name, row.Name},
		{"Email", old.Email, row.Email},
		{"Phone", old.Phone, row.Phone},
	})
}

func unlinkedDetail(email string) string {
	if email == "" {
		return "unlinked"
	}
	return "unlinked · " + email
}

func (lime *Lime) customerName(id uint) string {
	if id == 0 {
		return ""
	}
	var c models.Customer
	if err := lime.DB.Select("name").First(&c, id).Error; err != nil || c.Name == "" {
		return fmt.Sprintf("#%d", id)
	}
	return c.Name
}

func (lime *Lime) companyLabel(customerID uint) string {
	if lime.reportCustomer != "" {
		return lime.reportCustomer
	}
	return lime.customerName(customerID)
}

func countChanges(changes []syncChange) (added, updated, removed int) {
	for _, c := range changes {
		switch c.Action {
		case changeAdded:
			added++
		case changeUpdated:
			updated++
		case changeRemoved:
			removed++
		}
	}
	return added, updated, removed
}

func reportSubject(changes []syncChange) string {
	added, updated, removed := countChanges(changes)
	return fmt.Sprintf("Lime sync: %d added, %d updated, %d removed", added, updated, removed)
}

func renderChangeReport(changes []syncChange) string {
	var b strings.Builder
	added, updated, removed := countChanges(changes)
	fmt.Fprintf(&b, "<p>Lime sync changed Factum: %d added, %d updated, %d removed.</p>", added, updated, removed)
	for _, action := range []changeAction{changeAdded, changeUpdated, changeRemoved} {
		var rows []syncChange
		for _, c := range changes {
			if c.Action == action {
				rows = append(rows, c)
			}
		}
		if len(rows) == 0 {
			continue
		}
		title := string(action)
		if title != "" {
			title = strings.ToUpper(title[:1]) + title[1:]
		}
		fmt.Fprintf(&b, "<h2>%s (%d)</h2>", html.EscapeString(title), len(rows))
		b.WriteString(`<table border="1" cellpadding="4" cellspacing="0"><tr><th>Type</th><th>Entry</th><th>What changed</th></tr>`)
		for _, row := range rows {
			fmt.Fprintf(&b, "<tr><td>%s</td><td>%s</td><td>%s</td></tr>",
				html.EscapeString(row.Kind),
				html.EscapeString(row.Label),
				html.EscapeString(row.Detail),
			)
		}
		b.WriteString("</table>")
	}
	return b.String()
}

// reportChanges emails the collected changes. No mail is sent when nothing
// changed, when no recipients are configured, or when SMTP is not set up.
// A send failure is returned so the job is marked failed; the change lines
// were already emitted on the reporter.
func (lime *Lime) reportChanges(settings *models.Settings, reporter jobevent.Reporter) error {
	if len(lime.changes) == 0 || settings == nil {
		return nil
	}
	emit := func(level jobevent.Level, format string, args ...any) {
		if reporter != nil {
			reporter.Emit(level, format, args...)
		}
	}
	recipients := mail.ParseRecipients(settings.LimeReportTo)
	if len(recipients) == 0 {
		emit(jobevent.Warning, "Lime sync found %d change(s) but no report recipients are set (Admin → Sources → Lime)", len(lime.changes))
		return nil
	}
	if strings.TrimSpace(settings.SmtpHost) == "" || strings.TrimSpace(settings.EmailSender) == "" {
		emit(jobevent.Warning, "Lime sync found %d change(s) but SMTP is not configured (Admin → Settings → Factum → Email)", len(lime.changes))
		return nil
	}
	body := renderChangeReport(lime.changes)
	subject := reportSubject(lime.changes)
	smtp := util.WithSMTP(util.NewCommonConfig(settings), settings)
	var failed []string
	for _, to := range recipients {
		if err := sendReport(smtp, settings.EmailSender, to, subject, body); err != nil {
			failed = append(failed, fmt.Sprintf("%s: %v", to, err))
		}
	}
	if len(failed) > 0 {
		return fmt.Errorf("lime sync saved, but emailing the change report failed (the changes are listed above): %s", strings.Join(failed, "; "))
	}
	emit(jobevent.Info, "Lime change report emailed to %s", strings.Join(recipients, ", "))
	return nil
}
