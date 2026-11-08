package core

import (
	"net"
	"testing"
)

func TestNativeLANValidateConfigAcceptsProfileIdentity(t *testing.T) {
	config := nativeLANConfig{
		Name:      "D TV",
		Kind:      "computer",
		Account:   "0123456789abcdef0123456789abcdef",
		User:      "管理員",
		ProfileID: "default",
		Sources:   []string{},
	}
	if err := nativeLANValidateConfig(config); err != nil {
		t.Fatalf("expected valid local profile ID to be accepted, got %v", err)
	}
}

func TestNativeLANValidateConfigKeepsLegacyProfileIdentityOptional(t *testing.T) {
	config := nativeLANConfig{
		Name:    "D TV",
		Kind:    "computer",
		Account: "0123456789abcdef0123456789abcdef",
		User:    "管理員",
	}
	if err := nativeLANValidateConfig(config); err != nil {
		t.Fatalf("expected a legacy config without profile ID to remain valid, got %v", err)
	}
}

func TestNativeLANValidateConfigRejectsMalformedProfileIdentity(t *testing.T) {
	config := nativeLANConfig{
		Name:      "D TV",
		Kind:      "computer",
		Account:   "0123456789abcdef0123456789abcdef",
		User:      "管理員",
		ProfileID: "not valid / profile",
	}
	if err := nativeLANValidateConfig(config); err == nil {
		t.Fatal("expected malformed profile ID to be rejected")
	}
}

func TestNativeLANInfoAdvertisesProfileIDOnlyInPrivatePayload(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	link := &nativeLANServer{
		identity:   nativeLANIdentity{ID: "0123456789abcdef0123456789abcdef"},
		pin:        "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		generation: "test-generation",
		config: nativeLANConfig{
			Name:      "D TV",
			Kind:      "computer",
			Account:   "0123456789abcdef0123456789abcdef",
			User:      "管理員",
			ProfileID: "default",
			Sources:   []string{},
		},
		listener: listener,
	}

	publicInfo := link.info(false)
	if _, exists := publicInfo["profileId"]; exists {
		t.Fatal("profile ID must not be exposed in public device discovery")
	}

	privateInfo := link.info(true)
	if got := privateInfo["profileId"]; got != "default" {
		t.Fatalf("private device response profileId = %v, want default", got)
	}
}
