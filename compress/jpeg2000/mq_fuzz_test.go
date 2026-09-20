package jpeg2000

import "testing"

// FuzzMQDecoder feeds the decoder arbitrary bytes. A code-block's data comes
// from the file, so it can be anything at all: truncated mid-renormalization,
// all 0xFF, or a marker where a byte was expected. The decoder must keep
// answering decisions rather than panic, hang, or read past its slice — tier-1
// asks for as many decisions as the pass header promised, whatever the bytes
// underneath turn out to be.
func FuzzMQDecoder(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte{0xFF})
	f.Add([]byte{0xFF, 0xFF})
	f.Add([]byte{0xFF, 0x7F})
	f.Add([]byte{0x84, 0xC7, 0x3B, 0xFC, 0xE1, 0xA1, 0x43, 0x04, 0x02, 0x20})

	f.Fuzz(func(t *testing.T, data []byte) {
		dec := newMQDecoder(data, 19)
		for i := 0; i < 2000; i++ {
			if bit := dec.decode(i % 19); bit != 0 && bit != 1 {
				t.Fatalf("decode returned %d", bit)
			}
		}
	})
}
