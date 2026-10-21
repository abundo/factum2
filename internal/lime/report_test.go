package lime

import (
	"bytes"
	"strings"
	"testing"

	"github.com/abundo/factum2/internal/jobevent"
	"github.com/abundo/factum2/internal/util"
	"github.com/abundo/factum2/models"
	limetoolmodels "github.com/abundo/limetool/models"
)

func delivery(id uint, cn, status string) models.Service {
	return serviceFromDelivery(limetoolmodels.LimeDelivery{
		ID:               id,
		ConnectionNumber: cn,
		Agreeement:       &limetoolmodels.LimeAgreement{Status: &limetoolmodels.LimeAgreementStatus{Text: status}},
	}, 1, 1)
}

func TestSaveCustomerIgnoresLastSyncOnly(t *testing.T) {
	db := newTestDB(t)
	l := &Lime{DB: db}

	row := models.Customer{
		Source: "lime", SourceID: "3", Name: "Acme", LastSync: 1,
		Postalcity: "Luleå", OrganizationNumber: "556677-8899",
	}
	if err := l.SaveCustomer(&row); err != nil {
		t.Fatalf("create: %v", err)
	}
	if len(l.changes) != 1 || l.changes[0].Action != changeAdded || l.changes[0].Kind != "Customer" {
		t.Fatalf("create changes = %+v", l.changes)
	}
	if !strings.Contains(l.changes[0].Detail, "Luleå") || !strings.Contains(l.changes[0].Detail, "556677-8899") {
		t.Errorf("added detail = %q", l.changes[0].Detail)
	}

	again := row
	again.ID = 0
	again.LastSync = 2
	if err := l.SaveCustomer(&again); err != nil {
		t.Fatalf("resync: %v", err)
	}
	if len(l.changes) != 1 {
		t.Fatalf("last_sync-only resync recorded a change: %+v", l.changes)
	}

	again.Name = "Acme AB"
	again.Postalcity = "Boden"
	if err := l.SaveCustomer(&again); err != nil {
		t.Fatalf("update: %v", err)
	}
	if len(l.changes) != 2 || l.changes[1].Action != changeUpdated {
		t.Fatalf("update changes = %+v", l.changes)
	}
	if !strings.Contains(l.changes[1].Detail, "Name: Acme → Acme AB") || !strings.Contains(l.changes[1].Detail, "City: Luleå → Boden") {
		t.Errorf("update detail = %q", l.changes[1].Detail)
	}
}

func TestSaveDeliveryAndContactRecordRealChanges(t *testing.T) {
	db := newTestDB(t)
	l := &Lime{DB: db, reportCustomer: "Acme"}

	svc := delivery(8, "CN1", "Active")
	svc.LastSync = 1
	if err := l.SaveDelivery(&svc); err != nil {
		t.Fatalf("create service: %v", err)
	}
	same := delivery(8, "CN1", "Active")
	same.LastSync = 2
	if err := l.SaveDelivery(&same); err != nil {
		t.Fatalf("resync service: %v", err)
	}
	if len(l.changes) != 1 {
		t.Fatalf("service resync changes = %+v, want the add only", l.changes)
	}
	changed := delivery(8, "CN1", "Ended")
	changed.LastSync = 3
	if err := l.SaveDelivery(&changed); err != nil {
		t.Fatalf("update service: %v", err)
	}
	if l.changes[1].Action != changeUpdated || l.changes[1].Label != "CN1 (Acme)" {
		t.Fatalf("service update = %+v", l.changes[1])
	}
	if !strings.Contains(l.changes[1].Detail, "Agreement status: Active → Ended") {
		t.Errorf("service detail = %q", l.changes[1].Detail)
	}

	contact := contactFromPerson(limetoolmodels.LimePerson{ID: 4, Name: "Ada", Email: "ada@example.com"}, 1)
	if err := l.SaveContact(&contact); err != nil {
		t.Fatalf("create contact: %v", err)
	}
	contact.ID = 0
	contact.LastSync = 2
	if err := l.SaveContact(&contact); err != nil {
		t.Fatalf("resync contact: %v", err)
	}
	contact.Email = "ada@other.example"
	contact.LastSync = 3
	if err := l.SaveContact(&contact); err != nil {
		t.Fatalf("update contact: %v", err)
	}
	var got []syncChange
	for _, c := range l.changes {
		if c.Kind == "Contact" {
			got = append(got, c)
		}
	}
	if len(got) != 2 || got[0].Action != changeAdded || got[1].Action != changeUpdated {
		t.Fatalf("contact changes = %+v", got)
	}
	if got[0].Label != "Ada (Acme)" || !strings.Contains(got[1].Detail, "Email: ada@example.com → ada@other.example") {
		t.Errorf("contact changes = %+v", got)
	}
}

func TestRenderChangeReportEscapesAndGroups(t *testing.T) {
	body := renderChangeReport([]syncChange{
		{Action: changeAdded, Kind: "Customer", Label: "A & B <x>", Detail: "Luleå"},
		{Action: changeRemoved, Kind: "Service", Label: "CN1 (A & B)", Detail: ""},
	})
	if !strings.Contains(body, "A &amp; B &lt;x&gt;") {
		t.Errorf("label was not escaped: %s", body)
	}
	if !strings.Contains(body, "<h2>Added (1)</h2>") || !strings.Contains(body, "<h2>Removed (1)</h2>") {
		t.Errorf("sections missing: %s", body)
	}
	if strings.Contains(body, "<h2>Updated") {
		t.Errorf("empty updated section was rendered: %s", body)
	}
	if reportSubject([]syncChange{
		{Action: changeAdded}, {Action: changeUpdated}, {Action: changeRemoved}, {Action: changeRemoved},
	}) != "Lime sync: 1 added, 1 updated, 2 removed" {
		t.Errorf("subject = %q", reportSubject([]syncChange{{Action: changeAdded}}))
	}
}

func TestReportChangesEmailsOnlyWhenNeeded(t *testing.T) {
	var sent []string
	var subjects []string
	orig := sendReport
	sendReport = func(smtp util.CommonConfig, from, to, subject, body string) error {
		if from != "factum@example.com" {
			t.Errorf("from = %q", from)
		}
		if smtp.SmtpHost != "smtp.example.com" {
			t.Errorf("smtp host = %q", smtp.SmtpHost)
		}
		sent = append(sent, to)
		subjects = append(subjects, subject)
		if !strings.Contains(body, "Acme") {
			t.Errorf("body missing Acme: %s", body)
		}
		return nil
	}
	t.Cleanup(func() { sendReport = orig })

	var log bytes.Buffer
	rep := jobevent.NewConsoleReporter(&log)
	settings := &models.Settings{
		LimeReportTo: "ops@example.com, ops@example.com\nsecond@example.com",
		SmtpHost:     "smtp.example.com",
		EmailSender:  "factum@example.com",
	}

	l := &Lime{}
	if err := l.reportChanges(settings, rep); err != nil {
		t.Fatalf("empty report: %v", err)
	}
	if len(sent) != 0 {
		t.Fatalf("emailed with no changes: %v", sent)
	}

	l.note(changeAdded, "Customer", "Acme", "Luleå")
	l.note(changeRemoved, "Contact", "Ada (Acme)", "unlinked · ada@example.com")
	if err := l.reportChanges(settings, rep); err != nil {
		t.Fatalf("send: %v", err)
	}
	if strings.Join(sent, ",") != "ops@example.com,second@example.com" {
		t.Fatalf("recipients = %v", sent)
	}
	if len(subjects) != 2 || subjects[0] != "Lime sync: 1 added, 0 updated, 1 removed" {
		t.Fatalf("subjects = %v", subjects)
	}
	if !strings.Contains(log.String(), "emailed to ops@example.com, second@example.com") {
		t.Errorf("log = %q", log.String())
	}

	log.Reset()
	noHost := *settings
	noHost.SmtpHost = ""
	l2 := &Lime{changes: []syncChange{{Action: changeAdded, Kind: "Customer", Label: "Acme"}}}
	if err := l2.reportChanges(&noHost, rep); err != nil {
		t.Fatalf("missing smtp should not fail the sync: %v", err)
	}
	if len(sent) != 2 {
		t.Fatalf("sent without smtp: %v", sent)
	}
	if !strings.Contains(log.String(), "WARNING:") || !strings.Contains(log.String(), "SMTP") {
		t.Errorf("expected SMTP warning, log = %q", log.String())
	}

	log.Reset()
	noTo := *settings
	noTo.LimeReportTo = "  \n"
	l3 := &Lime{changes: []syncChange{{Action: changeUpdated, Kind: "Service", Label: "CN1"}}}
	if err := l3.reportChanges(&noTo, rep); err != nil {
		t.Fatalf("missing recipients: %v", err)
	}
	if len(sent) != 2 {
		t.Fatalf("sent without recipients: %v", sent)
	}
	if !strings.Contains(log.String(), "no report recipients") {
		t.Errorf("log = %q", log.String())
	}
}

func TestReportChangesReturnsSendError(t *testing.T) {
	orig := sendReport
	sendReport = func(smtp util.CommonConfig, from, to, subject, body string) error {
		return errString("dial failed")
	}
	t.Cleanup(func() { sendReport = orig })

	l := &Lime{changes: []syncChange{{Action: changeAdded, Kind: "Customer", Label: "Acme"}}}
	settings := &models.Settings{LimeReportTo: "ops@example.com", SmtpHost: "smtp.example.com", EmailSender: "factum@example.com"}
	err := l.reportChanges(settings, jobevent.NewConsoleReporter(&bytes.Buffer{}))
	if err == nil || !strings.Contains(err.Error(), "dial failed") || !strings.Contains(err.Error(), "lime sync saved") {
		t.Fatalf("err = %v", err)
	}
}

type errString string

func (e errString) Error() string { return string(e) }
