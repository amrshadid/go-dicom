package jpeg2000

import (
	"math/rand"
	"slices"
	"testing"
)

// readBlocksOneByOne is the reading readBlocks replaces: every block of the
// precinct asked in turn, in every packet. It is the reference the stepping
// reader must agree with bit for bit.
func readBlocksOneByOne(p *precinctState, r *bitReader, layer int, readBlock func(i int) error) error {
	for i := 0; i < p.w*p.h; i++ {
		if p.blocks[i] != nil {
			bit, err := r.readBit()
			if err != nil {
				return err
			}
			if bit == 1 {
				if err := readBlock(i); err != nil {
					return err
				}
			}
			continue
		}
		value, known, err := p.inclusion.decode(r, i, int32(layer)+1)
		if err != nil {
			return err
		}
		if known && value <= int32(layer) {
			if err := readBlock(i); err != nil {
				return err
			}
		}
	}
	return nil
}

// inclusionTrace reads layers packets of one precinct band of w by h blocks
// from data, with the stepping reader or the reference, and returns which
// block each readBlock call named and the bit offset after each packet. A
// block that contributes reads two more bits, standing in for the rest of
// its header, so that the two readers would drift apart on any difference.
func inclusionTrace(data []byte, w, h, layers int, stepping bool) (calls []int, offsets []int, err error) {
	p := &precinctState{inclusion: newTagTree(w, h), w: w, h: h, blocks: map[int]*blockState{}}
	r := newBitReader(data)
	work := &packetWork{budget: 1 << 30}
	for layer := 0; layer < layers; layer++ {
		readBlock := func(i int) error {
			calls = append(calls, i)
			if p.blocks[i] == nil {
				p.blocks[i] = &blockState{}
			}
			_, err := r.readBits(2)
			return err
		}
		if stepping {
			err = p.readBlocks(r, layer, 0, work, readBlock)
		} else {
			err = readBlocksOneByOne(p, r, layer, readBlock)
		}
		if err != nil {
			return calls, offsets, err
		}
		offsets = append(offsets, r.pos*8-r.bits)
		calls = append(calls, -1) // the end of a packet
	}
	return calls, offsets, nil
}

// checkInclusionReadersAgree requires the stepping reader and the reference
// to read the same bits for the same blocks, and to fail in the same packet.
func checkInclusionReadersAgree(t *testing.T, data []byte, w, h, layers int) {
	t.Helper()
	wantCalls, wantOffsets, wantErr := inclusionTrace(data, w, h, layers, false)
	gotCalls, gotOffsets, gotErr := inclusionTrace(data, w, h, layers, true)
	if (wantErr == nil) != (gotErr == nil) || !slices.Equal(wantCalls, gotCalls) || !slices.Equal(wantOffsets, gotOffsets) {
		t.Fatalf("%dx%d blocks, %d layers, data % x:\nreference: %v %v %v\nstepping:  %v %v %v",
			w, h, layers, data, wantCalls, wantOffsets, wantErr, gotCalls, gotOffsets, gotErr)
	}
}

// TestSteppingOverBlocksReadsWhatAskingEachDoes runs the stepping inclusion
// reader and the reference that asks every block, on random bits over random
// grids of blocks and layers, biased towards 0 bits so that tag tree nodes
// reach their threshold and the stepping reader has something to step over.
// The two must agree on every block named and every bit read.
func TestSteppingOverBlocksReadsWhatAskingEachDoes(t *testing.T) {
	rng := rand.New(rand.NewSource(526))
	for trial := 0; trial < 20000; trial++ {
		data := make([]byte, 1+rng.Intn(64))
		for i := range data {
			// One bit in four set, most of the time, and any byte otherwise.
			if rng.Intn(4) == 0 {
				data[i] = byte(rng.Intn(256))
				continue
			}
			for b := 0; b < 8; b++ {
				if rng.Intn(4) == 0 {
					data[i] |= 1 << b
				}
			}
			if i > 0 && data[i-1] == 0xFF {
				data[i] &= 0x7F
			}
		}
		checkInclusionReadersAgree(t, data, 1+rng.Intn(40), 1+rng.Intn(40), 1+rng.Intn(6))
	}
}

// FuzzSteppingOverBlocks is TestSteppingOverBlocksReadsWhatAskingEachDoes
// driven by the fuzzer.
func FuzzSteppingOverBlocks(f *testing.F) {
	f.Add([]byte{0x00, 0x00}, uint8(16), uint8(16), uint8(3))
	f.Add([]byte{0x80, 0x40, 0x10, 0x00, 0x08}, uint8(37), uint8(5), uint8(4))
	f.Add([]byte{0x11, 0x22, 0x44, 0x88, 0x00, 0x01}, uint8(1), uint8(64), uint8(2))
	f.Fuzz(func(t *testing.T, data []byte, w, h, layers uint8) {
		checkInclusionReadersAgree(t, data, int(w%80)+1, int(h%80)+1, int(layers%8)+1)
	})
}
