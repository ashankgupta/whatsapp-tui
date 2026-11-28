package tui

import (
	"context"
	"strings"
	"testing"

	"whatsapp-tui/contacts"
)

func TestResolveChat(t *testing.T) {
	saved := []contacts.Contact{
		{Name: "Rahul Sharma", Number: "919876543210"},
		{Name: "Rahul Verma", Number: "919812345678"},
		{Name: "Mom", Number: "919800000001"},
	}
	ctx := context.Background()

	tests := []struct {
		query, key, err string
	}{
		{"Mom", "919800000001", ""},
		{"mom", "919800000001", ""},
		{"verma", "919812345678", ""},
		{"+91 98765 43210", "919876543210", ""},
		{"1203630000000000@g.us", "1203630000000000@g.us", ""},
		{"919876543210@s.whatsapp.net", "919876543210", ""},
		{"Rahul", "", "more than one chat"},
		{"Rahul Verma (+919812345678)", "919812345678", ""},
		{"Anyone (+91 98000 00001)", "919800000001", ""},
		{"Nobody", "", "no contact"},
		{"12", "", ""}, // too short to be a number: any error
	}
	for _, tt := range tests {
		key, _, err := ResolveChat(ctx, nil, nil, saved, tt.query)
		if tt.key != "" {
			if err != nil || key != tt.key {
				t.Errorf("%q: got %q, %v; want %q", tt.query, key, err, tt.key)
			}
			continue
		}
		if err == nil {
			t.Errorf("%q: expected an error, got %q", tt.query, key)
		} else if tt.err != "" && !strings.Contains(err.Error(), tt.err) {
			t.Errorf("%q: error %q, want it to mention %q", tt.query, err, tt.err)
		}
	}
}

func TestSuggestions(t *testing.T) {
	saved := []contacts.Contact{
		{Name: "Ashank Gupta", Number: "919682105347"},
		{Name: "Asha", Number: "919800000002"},
	}
	people := map[string]string{
		"919811111111": "~ Aslam Khan",
		"919800000002": "Asha (phone)", // saved contact wins
		"1234@lid":     "Asif",         // LID keys are skipped
		"919822222222": "Bob Ashford",
		"919833333333": "Twin",
		"919844444444": "Twin",
	}
	groups := map[string]string{"1203630001@g.us": "Ashram Friends", "1203630002@newsletter": "News"}

	words := func(list []Suggestion) []string {
		var out []string
		for _, s := range list {
			out = append(out, s.Word)
		}
		return out
	}

	got := words(Suggestions(saved, people, groups, "As", 0))
	want := []string{"Ashank Gupta", "Asha", "Aslam Khan", "Ashram Friends", "Bob Ashford"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("As: got %q, want %q", got, want)
	}

	if got := words(Suggestions(saved, people, groups, "gu", 0)); len(got) != 1 || got[0] != "Ashank Gupta" {
		t.Errorf("gu (second word): got %q", got)
	}
	if got := words(Suggestions(saved, people, groups, "twin", 0)); strings.Join(got, "|") != "Twin (+919833333333)|Twin (+919844444444)" {
		t.Errorf("shared name: got %q, want names with numbers", got)
	}
	if got := words(Suggestions(saved, people, groups, "+91 96", 0)); len(got) != 1 || got[0] != "+919682105347" {
		t.Errorf("number: got %q", got)
	}
	if got := Suggestions(saved, people, groups, "m", 0); len(got) == 0 || got[0].Word != "me" {
		t.Errorf("m: want me first, got %+v", got)
	}
	if got := Suggestions(saved, people, groups, "", 3); len(got) != 3 {
		t.Errorf("limit: got %d", len(got))
	}
	for _, s := range Suggestions(saved, people, groups, "news", 0) {
		if s.Word == "News" && s.Description != "channel" {
			t.Errorf("channel description: %+v", s)
		}
	}
}
