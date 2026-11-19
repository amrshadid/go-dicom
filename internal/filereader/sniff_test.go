package filereader

import "testing"

// TestSniffDataSetEncoding covers the decision directly.
func TestSniffDataSetEncoding(t *testing.T) {
	tests := []struct {
		name         string
		head         []byte
		explicitVR   bool
		littleEndian bool
		ok           bool
	}{
		{
			// (0008,0005) CS length 10, explicit VR little endian.
			name:       "explicit little endian",
			head:       []byte{0x08, 0x00, 0x05, 0x00, 'C', 'S', 0x0A, 0x00},
			explicitVR: true, littleEndian: true, ok: true,
		},
		{
			// The same element with both the tag and the length byte-swapped.
			name:       "explicit big endian",
			head:       []byte{0x00, 0x08, 0x00, 0x05, 'C', 'S', 0x00, 0x0A},
			explicitVR: true, littleEndian: false, ok: true,
		},
		{
			// (0008,0005) with a 4-byte length and no VR: 10 little endian.
			name:       "implicit little endian",
			head:       []byte{0x08, 0x00, 0x05, 0x00, 0x0A, 0x00, 0x00, 0x00},
			explicitVR: false, littleEndian: true, ok: true,
		},
		{
			// Implicit VR big endian is not a transfer syntax the standard
			// defines, so an absent VR means little endian and nothing else.
			name:       "no VR means implicit, which is only little endian",
			head:       []byte{0x00, 0x08, 0x00, 0x05, 0x00, 0x00, 0x00, 0x0A},
			explicitVR: false, littleEndian: true, ok: true,
		},
		{
			name: "too short to tell",
			head: []byte{0x08, 0x00, 0x05},
			ok:   false, littleEndian: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			explicit, little, ok := sniffDataSetEncoding(tc.head)
			if ok != tc.ok {
				t.Fatalf("ok is %v, want %v", ok, tc.ok)
			}
			if !ok {
				return
			}
			if explicit != tc.explicitVR || little != tc.littleEndian {
				t.Errorf("read as %s, want %s",
					describeEncoding(explicit, little),
					describeEncoding(tc.explicitVR, tc.littleEndian))
			}
		})
	}
}
