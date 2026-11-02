package contacts

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
)

// Contact represents a contact with a name and phone number.
type Contact struct {
	Name   string `json:"name"`
	Number string `json:"number"`
}

// LoadContacts reads contacts from a JSON file and deduplicates them based on phone number.
func LoadContacts(filePath string) []Contact {
	data, err := os.ReadFile(filePath)
	if err != nil {
		log.Fatalf("❌ Error reading contacts: %v", err)
	}
	var contacts []Contact
	err = json.Unmarshal(data, &contacts)
	if err != nil {
		log.Fatalf("❌ Error parsing contacts: %v", err)
	}
	if len(contacts) == 0 {
		log.Fatalf("❌ No contacts found in %s", filePath)
	}
	// Deduplicate contacts based on number
	seen := make(map[string]bool)
	deduplicated := []Contact{}
	for _, c := range contacts {
		if !seen[c.Number] {
			seen[c.Number] = true
			deduplicated = append(deduplicated, c)
		}
	}
	// Debug: Print loaded contacts to confirm deduplication
	for i, c := range deduplicated {
		fmt.Printf("Contact %d: %s (%s)\n", i, c.Name, c.Number)
	}
	return deduplicated
}
