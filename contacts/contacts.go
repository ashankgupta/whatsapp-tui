package contacts

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
)

// Contact represents a contact with a name and phone number.
type Contact struct {
	Name   string `json:"name"`
	Number string `json:"number"`
}

// LoadContacts reads contacts from a JSON file. A missing file is created
// empty so first-time users can add contacts from the app instead of writing
// JSON by hand. Contacts are deduplicated by number and sorted by name.
func LoadContacts(filePath string) ([]Contact, error) {
	data, err := os.ReadFile(filePath)
	if errors.Is(err, os.ErrNotExist) {
		if err := SaveContacts(filePath, nil); err != nil {
			return nil, fmt.Errorf("create %s: %w", filePath, err)
		}
		return []Contact{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", filePath, err)
	}
	if strings.TrimSpace(string(data)) == "" {
		return []Contact{}, nil
	}

	var list []Contact
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, fmt.Errorf("%s is not valid JSON (fix or delete it): %w", filePath, err)
	}

	seen := make(map[string]bool)
	contacts := []Contact{}
	for _, c := range list {
		c.Name = strings.TrimSpace(c.Name)
		number, err := NormalizeNumber(c.Number)
		if err != nil || seen[number] {
			continue
		}
		c.Number = number
		if c.Name == "" {
			c.Name = "+" + number
		}
		seen[number] = true
		contacts = append(contacts, c)
	}
	sortContacts(contacts)
	return contacts, nil
}

// SaveContacts writes contacts to a JSON file atomically, so a crash never
// leaves a half-written file behind.
func SaveContacts(filePath string, list []Contact) error {
	if list == nil {
		list = []Contact{}
	}
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	tmp, err := os.CreateTemp(filepath.Dir(filePath), ".contacts-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), filePath)
}

// Upsert adds a contact or renames the existing contact with the same number.
func Upsert(list []Contact, c Contact) []Contact {
	out := make([]Contact, 0, len(list)+1)
	replaced := false
	for _, existing := range list {
		if existing.Number == c.Number {
			out = append(out, c)
			replaced = true
			continue
		}
		out = append(out, existing)
	}
	if !replaced {
		out = append(out, c)
	}
	sortContacts(out)
	return out
}

// Remove deletes the contact with the given number.
func Remove(list []Contact, number string) []Contact {
	out := make([]Contact, 0, len(list))
	for _, c := range list {
		if c.Number != number {
			out = append(out, c)
		}
	}
	return out
}

// NormalizeNumber turns user input such as "+91 98765-43210" into the
// international digits-only form WhatsApp uses ("919876543210").
func NormalizeNumber(input string) (string, error) {
	var b strings.Builder
	for _, r := range input {
		switch {
		case unicode.IsDigit(r):
			b.WriteRune(r)
		case r == '+' || r == ' ' || r == '-' || r == '(' || r == ')' || r == '.':
		default:
			return "", fmt.Errorf("phone numbers can only contain digits")
		}
	}
	number := strings.TrimLeft(b.String(), "0")
	if len(number) < 7 || len(number) > 15 {
		return "", fmt.Errorf("enter the full number with country code, e.g. +91 98765 43210")
	}
	return number, nil
}

func sortContacts(list []Contact) {
	sort.SliceStable(list, func(i, j int) bool {
		return strings.ToLower(list[i].Name) < strings.ToLower(list[j].Name)
	})
}
