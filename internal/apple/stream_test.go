package apple

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeLargeContactExport(t *testing.T, array bool) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "contacts.json")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	write := func(value string) {
		t.Helper()
		if _, err := io.WriteString(f, value); err != nil {
			t.Fatal(err)
		}
	}
	if array {
		write("[")
	}
	for i := range 9 {
		prefix := fmt.Sprintf(`{"identifier":"large-%d","full_name":"Contact %d","emails":["large-%d@example.com"],"unused":"`, i, i, i)
		const suffix = "\"}\n"
		const lineBytes = 75_498_408 / 9
		if array && i > 0 {
			write(",")
		}
		write(prefix)
		write(strings.Repeat("x", lineBytes-len(prefix)-len(suffix)))
		write(suffix)
	}
	if array {
		write("]")
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if !array {
		info, err := os.Stat(path)
		if err != nil || info.Size() != 75_498_408 {
			t.Fatalf("large fixture size: info=%v err=%v", info, err)
		}
	}
	return path
}

func checkLargeContacts(t *testing.T, contacts []Contact) {
	t.Helper()
	if len(contacts) != 9 {
		t.Fatalf("decoded %d contacts, want 9", len(contacts))
	}
	for i, c := range contacts {
		if c.Identifier != fmt.Sprintf("large-%d", i) || c.Name() != fmt.Sprintf("Contact %d", i) || len(c.Emails) != 1 || c.Emails[0] != fmt.Sprintf("large-%d@example.com", i) {
			t.Fatalf("contact %d = %#v", i, c)
		}
	}
}

func TestReadFilePreservesLargeExports(t *testing.T) {
	for _, array := range []bool{false, true} {
		t.Run(fmt.Sprintf("array=%v", array), func(t *testing.T) {
			contacts, err := ReadFile(writeLargeContactExport(t, array))
			if err != nil {
				t.Fatal(err)
			}
			checkLargeContacts(t, contacts)
		})
	}
}

func TestDecodePreservesNDJSONRecordLimit(t *testing.T) {
	const maxLine = 16 << 20
	prefix, suffix := `{"unused":"`, `","full_name":"Large Record"}`
	for _, n := range []int{maxLine - 1, maxLine} {
		line := prefix + strings.Repeat("x", n-len(prefix)-len(suffix)) + suffix
		contacts, err := Decode(strings.NewReader(line + "\n"))
		if n < maxLine {
			if err != nil || len(contacts) != 1 {
				t.Fatalf("below-limit record: contacts=%d err=%v", len(contacts), err)
			}
		} else if err == nil || !strings.Contains(err.Error(), "token too long") {
			t.Fatalf("oversized NDJSON record: %v", err)
		}
		// JSON arrays never had the NDJSON scanner's per-record limit.
		if contacts, err := Decode(strings.NewReader("[" + line + "]")); err != nil || len(contacts) != 1 {
			t.Fatalf("array record: contacts=%d err=%v", len(contacts), err)
		}
	}
}

func TestDecodeDoesNotReadPastMalformedRecord(t *testing.T) {
	for _, prefix := range []string{"{bad}\n", "[{bad},"} {
		r := &countingReader{r: io.MultiReader(strings.NewReader(prefix), strings.NewReader(strings.Repeat(" ", 1<<20)))}
		if _, err := Decode(r); err == nil {
			t.Fatal("expected malformed record error")
		}
		if r.n >= 1<<20 {
			t.Fatalf("read whole export before reporting malformed record: %d bytes", r.n)
		}
	}
}

func TestDecodePreservesNDJSONFraming(t *testing.T) {
	for _, input := range []string{"{} {}\n", "{\n\"full_name\":\"Ada\"\n}\n"} {
		if _, err := Decode(strings.NewReader(input)); err == nil {
			t.Fatalf("accepted input that is not one JSON record per line: %q", input)
		}
	}
	for _, input := range []string{"\u2003\n{\"full_name\":\"Ada\"}\n\u00a0", "\u2003[{\"full_name\":\"Ada\"}]\u00a0"} {
		contacts, err := Decode(strings.NewReader(input))
		if err != nil || len(contacts) != 1 || contacts[0].Name() != "Ada" {
			t.Fatalf("outer whitespace: contacts=%#v err=%v", contacts, err)
		}
	}
}
