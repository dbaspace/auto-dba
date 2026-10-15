package config

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestPartyIDsUnmarshal(t *testing.T) {
	var a struct {
		PartyIDs PartyIDs `yaml:"party_ids"`
	}
	if err := yaml.Unmarshal([]byte(`party_ids: "15"`), &a); err != nil {
		t.Fatal(err)
	}
	if a.PartyIDs.JoinWecomParty() != "15" {
		t.Fatalf("got %v", a.PartyIDs)
	}
	var b struct {
		PartyIDs PartyIDs `yaml:"party_ids"`
	}
	if err := yaml.Unmarshal([]byte("party_ids:\n  - \"15\"\n  - \"16\"\n"), &b); err != nil {
		t.Fatal(err)
	}
	if b.PartyIDs.JoinWecomParty() != "15|16" {
		t.Fatalf("got %v", b.PartyIDs)
	}
}
