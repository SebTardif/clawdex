package apple

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/openclaw/clawdex/internal/model"
)

// 64 MiB ceiling for import apple --input. Must exceed the old 16 MiB per-line max.
const maxAppleExportBytes = 64 << 20

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
	return decodeWithLimit(r, maxAppleExportBytes)
}

func decodeWithLimit(r io.Reader, maxBytes int64) ([]Contact, error) {
	limited := &io.LimitedReader{R: r, N: maxBytes + 1}
	br := bufio.NewReader(limited)
	first, err := peekNonSpace(br)
	if limited.N == 0 {
		return nil, errExportTooLarge(maxBytes)
	}
	if errors.Is(err, io.EOF) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	dec := json.NewDecoder(br)
	if first == '[' {
		var contacts []Contact
		if err := dec.Decode(&contacts); err != nil {
			return nil, limitOr(err, limited, maxBytes)
		}
		var extra json.RawMessage
		extraErr := dec.Decode(&extra)
		if limited.N == 0 {
			return nil, errExportTooLarge(maxBytes)
		}
		if extraErr == nil {
			return nil, errors.New("invalid data after JSON array")
		}
		if !errors.Is(extraErr, io.EOF) {
			return nil, extraErr
		}
		return contacts, nil
	}

	var contacts []Contact
	for {
		var c Contact
		if err := dec.Decode(&c); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, limitOr(err, limited, maxBytes)
		}
		contacts = append(contacts, c)
		if limited.N == 0 {
			return nil, errExportTooLarge(maxBytes)
		}
	}
	if limited.N == 0 {
		return nil, errExportTooLarge(maxBytes)
	}
	return contacts, nil
}

func peekNonSpace(br *bufio.Reader) (byte, error) {
	for {
		buf, err := br.Peek(1)
		if err != nil {
			return 0, err
		}
		switch buf[0] {
		case ' ', '\t', '\n', '\r':
			if _, err := br.ReadByte(); err != nil {
				return 0, err
			}
		default:
			return buf[0], nil
		}
	}
}

func errExportTooLarge(maxBytes int64) error {
	return fmt.Errorf("apple export exceeds %d-byte limit", maxBytes)
}

func limitOr(err error, limited *io.LimitedReader, maxBytes int64) error {
	if limited.N == 0 {
		return errExportTooLarge(maxBytes)
	}
	return err
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
