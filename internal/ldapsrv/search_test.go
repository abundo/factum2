package ldapsrv

import "testing"

func TestProjectMailAlias(t *testing.T) {
	e := &Entry{Attrs: map[string][]string{
		"mail": {"ada@example.com"},
		"cn":   {"Ada"},
	}}
	got := project(e, []string{"rfc822Mailbox"}, false)
	if len(got) != 1 || got[0][0] != "mail" || got[0][1] != "ada@example.com" {
		t.Fatalf("rfc822Mailbox projection = %#v", got)
	}
	tagged := project(e, []string{"mail;lang-en"}, false)
	if len(tagged) != 1 || tagged[0][1] != "ada@example.com" {
		t.Fatalf("mail;lang-en projection = %#v", tagged)
	}
}
