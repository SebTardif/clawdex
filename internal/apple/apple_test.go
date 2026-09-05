package apple

import (
	"bytes"
	"encoding/base64"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

type repeatByteReader struct{ b byte }

func (r repeatByteReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = r.b
	}
	return len(p), nil
}

func TestDecodeJSONArrayAndNDJSON(t *testing.T) {
	for _, input := range []string{
		`[{"identifier":"a1","full_name":"Ada Lovelace","emails":["ada@example.com"],"phones":["+1 555 0100"]}]`,
		"{\"identifier\":\"a1\",\"first_name\":\"Ada\",\"last_name\":\"Lovelace\",\"emails\":[\"ada@example.com\"]}\n",
	} {
		contacts, err := Decode(strings.NewReader(input))
		if err != nil {
			t.Fatal(err)
		}
		if len(contacts) != 1 || contacts[0].Name() != "Ada Lovelace" {
			t.Fatalf("contacts = %#v", contacts)
		}
		src := contacts[0].SourceContact(false)
		if src.Source != "apple" || src.ExternalID != "a1" || src.Name != "Ada Lovelace" {
			t.Fatalf("source = %#v", src)
		}
	}
}

func TestReadFileAndToSourceContacts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "contacts.ndjson")
	encoded := base64.StdEncoding.EncodeToString([]byte("avatar"))
	if err := os.WriteFile(path, []byte("{\"full_name\":\"Ada\",\"emails\":[\"ada@example.com\"],\"avatar_data\":\""+encoded+"\"}\n{\"phones\":[\"+1\"]}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	contacts, err := ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sources := ToSourceContacts(contacts, true)
	if len(sources) != 1 || sources[0].Name != "Ada" {
		t.Fatalf("sources = %#v", sources)
	}
	if sources[0].Avatar == nil || string(sources[0].Avatar.Data) != "avatar" {
		t.Fatalf("avatar source = %#v", sources[0].Avatar)
	}
}

func TestDecodeEmptyAndInvalid(t *testing.T) {
	contacts, err := Decode(strings.NewReader(" \n"))
	if err != nil || len(contacts) != 0 {
		t.Fatalf("contacts=%#v err=%v", contacts, err)
	}
	if _, err := Decode(strings.NewReader("{bad")); err == nil {
		t.Fatal("expected invalid json error")
	}
	if _, err := ReadFile(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("expected read file error")
	}
}

func TestDecodeLargeAvatarLine(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 128*1024))
	input := "{\"identifier\":\"a1\",\"full_name\":\"Ada\",\"emails\":[\"ada@example.com\"],\"avatar_data\":\"" + encoded + "\"}\n"
	contacts, err := Decode(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if len(contacts) != 1 || len(contacts[0].AvatarData) != 128*1024 {
		t.Fatalf("contacts = %#v", contacts)
	}
}

func TestDecodeRejectsOversizedExport(t *testing.T) {
	if maxAppleExportBytes != 64<<20 {
		t.Fatalf("maxAppleExportBytes = %d, want 64 MiB", maxAppleExportBytes)
	}
	contact := `{"identifier":"a1","full_name":"Ada Lovelace","emails":["ada@example.com"]}`
	array := `[{"identifier":"a1","full_name":"Ada Lovelace","emails":["ada@example.com"]}]`
	capName := strconv.FormatInt(maxAppleExportBytes, 10)
	for _, suffix := range []string{contact + "\n", array} {
		cr := &countingReader{r: io.MultiReader(
			io.LimitReader(repeatByteReader{b: ' '}, maxAppleExportBytes+1),
			strings.NewReader(suffix),
		)}
		_, err := Decode(cr)
		if err == nil {
			t.Fatal("expected oversized export error")
		}
		if !strings.Contains(err.Error(), capName) {
			t.Fatalf("error %q should name the %d-byte cap", err, maxAppleExportBytes)
		}
		if cr.n > maxAppleExportBytes+1 {
			t.Fatalf("read %d bytes; Decode slurped past the cap", cr.n)
		}
	}

	ws := &countingReader{r: io.LimitReader(repeatByteReader{b: ' '}, maxAppleExportBytes+1+(1<<20))}
	_, err := Decode(ws)
	if err == nil || !strings.Contains(err.Error(), capName) {
		t.Fatalf("expected whitespace-only over-cap error, got %v", err)
	}
	if ws.n > maxAppleExportBytes+1 {
		t.Fatalf("read %d bytes; Decode slurped past the cap", ws.n)
	}
}

func TestDecodeLeadingWhitespaceAndTrailingArrayJunk(t *testing.T) {
	contacts, err := Decode(strings.NewReader("  \n\t[{\"identifier\":\"a1\",\"full_name\":\"Ada Lovelace\"}]"))
	if err != nil {
		t.Fatal(err)
	}
	if len(contacts) != 1 || contacts[0].Name() != "Ada Lovelace" {
		t.Fatalf("contacts = %#v", contacts)
	}
	if _, err := Decode(strings.NewReader(`[{"identifier":"a1","full_name":"Ada"}] extra`)); err == nil {
		t.Fatal("expected trailing data after JSON array to fail")
	}
}

func TestDecodeWithLimitAtAndOverCap(t *testing.T) {
	const limit int64 = 256
	base := `[{"identifier":"a1","full_name":"Ada Lovelace"}]`
	if int64(len(base)) > limit {
		t.Fatal("fixture larger than test limit")
	}
	padded := strings.Repeat(" ", int(limit)-len(base)) + base
	contacts, err := decodeWithLimit(strings.NewReader(padded), limit)
	if err != nil {
		t.Fatal(err)
	}
	if len(contacts) != 1 || contacts[0].Name() != "Ada Lovelace" {
		t.Fatalf("contacts = %#v", contacts)
	}

	_, err = decodeWithLimit(strings.NewReader(" "+padded), limit)
	if err == nil || !strings.Contains(err.Error(), strconv.FormatInt(limit, 10)) {
		t.Fatalf("expected %d-byte cap error, got %v", limit, err)
	}

	ndjson := `{"identifier":"a1","full_name":"Ada Lovelace"}`
	cr := &countingReader{r: io.MultiReader(
		io.LimitReader(repeatByteReader{b: ' '}, 4097),
		strings.NewReader(ndjson+"\n"),
	)}
	_, err = decodeWithLimit(cr, 4096)
	if err == nil || !strings.Contains(err.Error(), "4096") {
		t.Fatalf("expected 4096-byte cap error, got %v", err)
	}
	if cr.n > 4097 {
		t.Fatalf("read %d bytes; Decode slurped past the cap", cr.n)
	}
}

type errReader struct{ err error }

func (e errReader) Read([]byte) (int, error) { return 0, e.err }

func TestDecodeErrorAndLimitBranches(t *testing.T) {
	if _, err := Decode(errReader{errors.New("boom")}); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("reader error = %v", err)
	}
	if _, err := Decode(strings.NewReader("[")); err == nil {
		t.Fatal("expected invalid JSON array error")
	}
	if _, err := Decode(strings.NewReader(`[{"identifier":"a1"}]{"identifier":"a2"}`)); err == nil {
		t.Fatal("expected trailing JSON after array to fail")
	}

	const limit int64 = 64
	if _, err := decodeWithLimit(strings.NewReader(`[{"identifier":"`+strings.Repeat("x", 200)), limit); err == nil || !strings.Contains(err.Error(), "64") {
		t.Fatalf("expected truncated array cap error, got %v", err)
	}

	base := `[{"identifier":"a1","full_name":"Ada"}]`
	if _, err := decodeWithLimit(strings.NewReader(base+strings.Repeat(" ", 200)), limit); err == nil || !strings.Contains(err.Error(), "64") {
		t.Fatalf("expected array padding cap error, got %v", err)
	}

	prefix, suffix := `{"identifier":"`, `"}`
	need := int(limit) + 1 - len(prefix) - len(suffix)
	fat := prefix + strings.Repeat("x", need) + suffix
	if _, err := decodeWithLimit(strings.NewReader(fat), limit); err == nil || !strings.Contains(err.Error(), "64") {
		t.Fatalf("expected oversized NDJSON object cap error, got %v", err)
	}

	obj := `{"identifier":"a1"}`
	padded := obj + strings.Repeat(" ", int(limit)+1-len(obj))
	if _, err := decodeWithLimit(strings.NewReader(padded), limit); err == nil || !strings.Contains(err.Error(), "64") {
		t.Fatalf("expected NDJSON whitespace cap error, got %v", err)
	}
}
