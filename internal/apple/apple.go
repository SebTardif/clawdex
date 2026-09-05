package apple

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"unicode"

	"github.com/openclaw/clawdex/internal/model"
)

type Contact struct {
	Identifier string   `json:"identifier"`
	FirstName  string   `json:"first_name"`
	LastName   string   `json:"last_name"`
	FullName   string   `json:"full_name"`
	Emails     []string `json:"emails"`
	Phones     []string `json:"phones"`
	AvatarData []byte   `json:"avatar_data,omitempty"`
}

func (c Contact) Name() string {
	if strings.TrimSpace(c.FullName) != "" {
		return strings.TrimSpace(c.FullName)
	}
	return strings.TrimSpace(strings.Join([]string{c.FirstName, c.LastName}, " "))
}

func (c Contact) SourceContact(includeAvatar bool) model.SourceContact {
	out := model.SourceContact{Source: "apple", ExternalID: c.Identifier, Name: c.Name()}
	for i, email := range c.Emails {
		if strings.TrimSpace(email) != "" {
			out.Emails = append(out.Emails, model.ContactValue{Value: email, Label: "other", Source: "apple", Primary: i == 0})
		}
	}
	for i, phone := range c.Phones {
		if strings.TrimSpace(phone) != "" {
			out.Phones = append(out.Phones, model.ContactValue{Value: phone, Label: "other", Source: "apple", Primary: i == 0})
		}
	}
	if includeAvatar && len(c.AvatarData) > 0 {
		out.Avatar = &model.SourceAvatar{Data: append([]byte(nil), c.AvatarData...)}
	}
	return out
}

func ReadFile(path string) ([]Contact, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	return Decode(f)
}

func Decode(r io.Reader) ([]Contact, error) {
	br := bufio.NewReader(r)
	first, err := peekNonSpace(br)
	if errors.Is(err, io.EOF) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	if first == '[' {
		return decodeArray(br)
	}

	var contacts []Contact
	scanner := bufio.NewScanner(br)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var c Contact
		if err := json.Unmarshal(line, &c); err != nil {
			return nil, err
		}
		contacts = append(contacts, c)
	}
	return contacts, scanner.Err()
}

func decodeArray(br *bufio.Reader) ([]Contact, error) {
	dec := json.NewDecoder(br)
	if _, err := dec.Token(); err != nil {
		return nil, err
	}
	var contacts []Contact
	for dec.More() {
		var c Contact
		if err := dec.Decode(&c); err != nil {
			return nil, err
		}
		contacts = append(contacts, c)
	}
	if _, err := dec.Token(); err != nil {
		return nil, err
	}
	// Include decoder read-ahead when checking for data after the closing bracket.
	tail := bufio.NewReader(io.MultiReader(dec.Buffered(), br))
	if _, err := peekNonSpace(tail); !errors.Is(err, io.EOF) {
		if err != nil {
			return nil, err
		}
		return nil, errors.New("invalid data after JSON array")
	}
	return contacts, nil
}

func peekNonSpace(br *bufio.Reader) (rune, error) {
	for {
		r, _, err := br.ReadRune()
		if err != nil {
			return 0, err
		}
		if !unicode.IsSpace(r) {
			return r, br.UnreadRune()
		}
	}
}

func ToSourceContacts(contacts []Contact, includeAvatars bool) []model.SourceContact {
	out := make([]model.SourceContact, 0, len(contacts))
	for _, contact := range contacts {
		if strings.TrimSpace(contact.Name()) == "" {
			continue
		}
		out = append(out, contact.SourceContact(includeAvatars))
	}
	return out
}
