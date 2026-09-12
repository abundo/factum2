package drivers

import "testing"

func TestCommitCLI(t *testing.T) {
	cases := []struct {
		comment string
		quoted  bool
		want    string
	}{
		{comment: "", quoted: true, want: "commit"},
		{comment: "   ", quoted: true, want: "commit"},
		{comment: `factum push CN00042 by Alice`, quoted: true, want: `commit comment "factum push CN00042 by Alice"`},
		{comment: `factum push CN00042 by Alice`, quoted: false, want: `commit comment factum push CN00042 by Alice`},
		{comment: `say "hi"; rm`, quoted: true, want: `commit comment "say hi rm"`},
		{comment: "line1\nline2", quoted: true, want: `commit comment "line1 line2"`},
	}
	for _, tc := range cases {
		if got := CommitCLI(tc.comment, tc.quoted); got != tc.want {
			t.Errorf("CommitCLI(%q, %v) = %q, want %q", tc.comment, tc.quoted, got, tc.want)
		}
	}

	long := make([]byte, 100)
	for i := range long {
		long[i] = 'a'
	}
	got := CommitCLI(string(long), true)
	inner := got[len(`commit comment "`):]
	inner = inner[:len(inner)-1]
	if len(inner) > commitCommentMax {
		t.Errorf("truncated comment len = %d, want <= %d", len(inner), commitCommentMax)
	}
}
