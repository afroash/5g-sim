// nas_test.go — Tests for NAS message encode/decode.
package nas

import (
	"testing"
)

// TestRegistrationRequestRoundTrip encodes a Registration Request
// then decodes it back and verifies the fields match.
// Ref: TS 24.501 §8.2.6
func TestRegistrationRequestRoundTrip(t *testing.T) {
	supi := SUPI("imsi-001010000000001")
	raw := BuildRegistrationRequest(supi, RegistrationTypeInitialRegistration, true)

	if len(raw) == 0 {
		t.Fatal("BuildRegistrationRequest returned empty bytes")
	}

	// Decode header
	msg, err := Decode(raw)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	if msg.EPD != EPD5GSMobilityManagement {
		t.Errorf("EPD = 0x%02X, want 0x%02X", msg.EPD, EPD5GSMobilityManagement)
	}
	if msg.MessageType != MsgTypeRegistrationRequest {
		t.Errorf("MessageType = 0x%02X, want 0x%02X",
			msg.MessageType, MsgTypeRegistrationRequest)
	}

	// Decode payload
	req, err := DecodeRegistrationRequest(msg.Payload)
	if err != nil {
		t.Fatalf("DecodeRegistrationRequest: %v", err)
	}

	if req.RegistrationType != RegistrationTypeInitialRegistration {
		t.Errorf("RegistrationType = %d, want %d",
			req.RegistrationType, RegistrationTypeInitialRegistration)
	}
	if !req.FollowOnRequest {
		t.Error("FollowOnRequest should be true")
	}
	if len(req.MobileIdentity) == 0 {
		t.Error("MobileIdentity should not be empty")
	}

	t.Logf("RegistrationRequest: %d bytes, type=%d followOn=%v ✓",
		len(raw), req.RegistrationType, req.FollowOnRequest)
}

// TestRegistrationAcceptRoundTrip verifies Registration Accept encode/decode.
// Ref: TS 24.501 §8.2.7
func TestRegistrationAcceptRoundTrip(t *testing.T) {
	guti := &GUTI5G{
		PLMN:      "00101",
		AMFRegion: 1,
		AMFSet:    1,
		AMFPtr:    0,
		TMSI:      0xDEADBEEF,
	}
	nssai := []SNSSAI{{SST: 1, SD: 0xFFFFFF}}

	raw := BuildRegistrationAccept(RegistrationResult3GPP, guti, nssai)
	if len(raw) == 0 {
		t.Fatal("BuildRegistrationAccept returned empty bytes")
	}

	msg, err := Decode(raw)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if msg.MessageType != MsgTypeRegistrationAccept {
		t.Errorf("MessageType = 0x%02X, want 0x%02X",
			msg.MessageType, MsgTypeRegistrationAccept)
	}

	acc, err := DecodeRegistrationAccept(msg.Payload)
	if err != nil {
		t.Fatalf("DecodeRegistrationAccept: %v", err)
	}

	if acc.RegistrationResult != RegistrationResult3GPP {
		t.Errorf("RegistrationResult = %d, want %d",
			acc.RegistrationResult, RegistrationResult3GPP)
	}
	if acc.GUTI5G == nil {
		t.Fatal("GUTI5G should not be nil")
	}
	if acc.GUTI5G.TMSI != guti.TMSI {
		t.Errorf("GUTI TMSI = 0x%08X, want 0x%08X", acc.GUTI5G.TMSI, guti.TMSI)
	}
	if acc.GUTI5G.PLMN != guti.PLMN {
		t.Errorf("GUTI PLMN = %s, want %s", acc.GUTI5G.PLMN, guti.PLMN)
	}
	if len(acc.AllowedNSSAI) != 1 {
		t.Errorf("AllowedNSSAI len = %d, want 1", len(acc.AllowedNSSAI))
	} else if acc.AllowedNSSAI[0].SST != 1 {
		t.Errorf("AllowedNSSAI[0].SST = %d, want 1", acc.AllowedNSSAI[0].SST)
	}

	t.Logf("RegistrationAccept: %d bytes GUTI PLMN=%s TMSI=0x%08X ✓",
		len(raw), acc.GUTI5G.PLMN, acc.GUTI5G.TMSI)
}

// TestRegistrationComplete verifies the minimal Complete message encodes cleanly.
// Ref: TS 24.501 §8.2.9
func TestRegistrationComplete(t *testing.T) {
	raw := BuildRegistrationComplete()
	if len(raw) < 3 {
		t.Fatalf("BuildRegistrationComplete: too short (%d bytes)", len(raw))
	}

	msg, err := Decode(raw)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if msg.MessageType != MsgTypeRegistrationComplete {
		t.Errorf("MessageType = 0x%02X, want 0x%02X",
			msg.MessageType, MsgTypeRegistrationComplete)
	}

	t.Logf("RegistrationComplete: %d bytes ✓", len(raw))
}

// TestRegistrationReject verifies reject message encoding.
// Ref: TS 24.501 §8.2.8
func TestRegistrationReject(t *testing.T) {
	raw := BuildRegistrationReject(CausePLMNNotAllowed)
	msg, err := Decode(raw)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if msg.MessageType != MsgTypeRegistrationReject {
		t.Errorf("MessageType = 0x%02X, want 0x%02X",
			msg.MessageType, MsgTypeRegistrationReject)
	}
	// Cause is first byte of payload
	if len(msg.Payload) < 1 || msg.Payload[0] != CausePLMNNotAllowed {
		t.Errorf("cause = 0x%02X, want 0x%02X", msg.Payload[0], CausePLMNNotAllowed)
	}

	t.Logf("RegistrationReject: cause=0x%02X ✓", CausePLMNNotAllowed)
}

// TestNSSAIRoundTrip verifies NSSAI encode/decode.
func TestNSSAIRoundTrip(t *testing.T) {
	input := []SNSSAI{
		{SST: 1, SD: 0xFFFFFF}, // eMBB, no SD
		{SST: 2, SD: 0x000001}, // URLLC with SD
	}

	encoded := encodeNSSAI(input)
	decoded := decodeNSSAI(encoded)

	if len(decoded) != len(input) {
		t.Fatalf("decoded %d SNSSAIs, want %d", len(decoded), len(input))
	}
	for i, s := range decoded {
		if s.SST != input[i].SST {
			t.Errorf("[%d] SST = %d, want %d", i, s.SST, input[i].SST)
		}
	}
	t.Logf("NSSAI round-trip: %d slices ✓", len(decoded))
}
