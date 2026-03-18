package network

import (
	"testing"
)

func TestAsyncOperationsWindowEncodeDecode(t *testing.T) {
	aow := &AsynchronousOperationsWindow{
		MaxOperationsInvoked:   10,
		MaxOperationsPerformed: 5,
	}

	encoded := aow.Encode()
	if encoded[0] != ItemTypeAsyncOperationsWindow {
		t.Errorf("expected item type 0x%02X, got 0x%02X", ItemTypeAsyncOperationsWindow, encoded[0])
	}

	// Decode (skip 4-byte header: type + reserved + length)
	decoded, err := DecodeAsyncOperationsWindow(encoded[4:])
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	if decoded.MaxOperationsInvoked != 10 {
		t.Errorf("expected MaxOperationsInvoked=10, got %d", decoded.MaxOperationsInvoked)
	}
	if decoded.MaxOperationsPerformed != 5 {
		t.Errorf("expected MaxOperationsPerformed=5, got %d", decoded.MaxOperationsPerformed)
	}
}

func TestSCPSCURoleSelectionEncodeDecode(t *testing.T) {
	role := &SCPSCURoleSelection{
		SOPClassUID: VerificationSOPClassUID,
		SCURole:     true,
		SCPRole:     false,
	}

	encoded := role.Encode()
	if encoded[0] != ItemTypeSCPSCURoleSelection {
		t.Errorf("expected item type 0x%02X, got 0x%02X", ItemTypeSCPSCURoleSelection, encoded[0])
	}

	// Decode (skip 4-byte header)
	decoded, err := DecodeSCPSCURoleSelection(encoded[4:])
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	if decoded.SOPClassUID != VerificationSOPClassUID {
		t.Errorf("expected UID %s, got %s", VerificationSOPClassUID, decoded.SOPClassUID)
	}
	if !decoded.SCURole {
		t.Error("expected SCURole=true")
	}
	if decoded.SCPRole {
		t.Error("expected SCPRole=false")
	}
}

func TestUserIdentityNegotiationEncodeDecode(t *testing.T) {
	tests := []struct {
		name     string
		identity *UserIdentityNegotiation
	}{
		{
			name: "username only",
			identity: &UserIdentityNegotiation{
				Type:                      UserIdentityUsername,
				PositiveResponseRequested: true,
				PrimaryField:              []byte("admin"),
				SecondaryField:            nil,
			},
		},
		{
			name: "username password",
			identity: &UserIdentityNegotiation{
				Type:                      UserIdentityUsernamePassword,
				PositiveResponseRequested: true,
				PrimaryField:              []byte("admin"),
				SecondaryField:            []byte("secret123"),
			},
		},
		{
			name: "JWT",
			identity: &UserIdentityNegotiation{
				Type:                      UserIdentityJWT,
				PositiveResponseRequested: false,
				PrimaryField:              []byte("eyJhbGciOiJIUzI1NiJ9.payload.signature"),
				SecondaryField:            nil,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encoded := tt.identity.Encode()
			if encoded[0] != ItemTypeUserIdentity {
				t.Errorf("expected item type 0x%02X, got 0x%02X", ItemTypeUserIdentity, encoded[0])
			}

			// Decode (skip 4-byte header)
			decoded, err := DecodeUserIdentityNegotiation(encoded[4:])
			if err != nil {
				t.Fatalf("decode failed: %v", err)
			}

			if decoded.Type != tt.identity.Type {
				t.Errorf("expected type %d, got %d", tt.identity.Type, decoded.Type)
			}
			if decoded.PositiveResponseRequested != tt.identity.PositiveResponseRequested {
				t.Errorf("expected positiveResponse=%v, got %v",
					tt.identity.PositiveResponseRequested, decoded.PositiveResponseRequested)
			}
			if string(decoded.PrimaryField) != string(tt.identity.PrimaryField) {
				t.Errorf("primary field mismatch: %q vs %q",
					string(decoded.PrimaryField), string(tt.identity.PrimaryField))
			}
		})
	}
}

func TestSOPClassExtendedNegotiationEncode(t *testing.T) {
	ext := &SOPClassExtendedNegotiation{
		SOPClassUID: CTImageStorageUID,
		ServiceData: []byte{0x01, 0x02, 0x03},
	}

	encoded := ext.Encode()
	if encoded[0] != ItemTypeSOPClassExtended {
		t.Errorf("expected item type 0x%02X, got 0x%02X", ItemTypeSOPClassExtended, encoded[0])
	}
	if len(encoded) == 0 {
		t.Error("encoded data should not be empty")
	}
}

func TestUserIdentityTypes(t *testing.T) {
	tests := []struct {
		identType UserIdentityType
		expected  byte
	}{
		{UserIdentityUsername, 1},
		{UserIdentityUsernamePassword, 2},
		{UserIdentityKerberos, 3},
		{UserIdentitySAML, 4},
		{UserIdentityJWT, 5},
	}

	for _, tt := range tests {
		if byte(tt.identType) != tt.expected {
			t.Errorf("expected %d, got %d", tt.expected, byte(tt.identType))
		}
	}
}
