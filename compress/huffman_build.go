package compress

// Huffman tables built for the data they code, ITU-T T.81 Annex K.2.
//
// The example tables of Annex K are enough for 8-bit baseline JPEG and for
// nothing wider: their DC tables stop at difference category 11, and a
// lossless 16-bit image needs categories up to 16, a 12-bit DCT image up to
// 15. A symbol missing from the table cannot be coded at all, so an encoder
// for either has to count the symbols it is about to write and build a table
// that holds them. This is the procedure of K.2, as libjpeg implements it,
// so a table here is one any decoder has seen many times.

// huffmanMaxSymbols is the 256 symbols a DHT segment can name, plus the one
// K.2 reserves so that no code is all ones.
const huffmanMaxSymbols = 257

// optimalHuffman returns the table that codes symbols at the given
// frequencies in the fewest bits, with no code longer than 16 bits and none
// made only of ones (T.81 K.2, figures K.1 to K.4). A symbol of frequency
// zero gets no code. At least one symbol must have a nonzero frequency.
func optimalHuffman(frequencies *[256]int) huffmanSpec {
	var freq [huffmanMaxSymbols]int
	copy(freq[:], frequencies[:])
	// The reserved symbol takes the all-ones code point of the longest
	// length and is removed at the end. A code of all ones would let a
	// decoder read the 1-bits that pad the last byte as a symbol (F.1.2.3).
	freq[256] = 1

	var codeSize [huffmanMaxSymbols]int
	var others [huffmanMaxSymbols]int
	for i := range others {
		others[i] = -1
	}

	// Figure K.1: merge the two least frequent trees until one is left,
	// counting how deep each symbol ends up. Ties go to the larger symbol,
	// as in libjpeg, so the reserved symbol sinks to the longest length.
	for {
		c1, c2 := -1, -1
		v1, v2 := int(^uint(0)>>1), int(^uint(0)>>1)
		for i := 0; i < huffmanMaxSymbols; i++ {
			if freq[i] == 0 {
				continue
			}
			if freq[i] <= v1 {
				c2, v2 = c1, v1
				c1, v1 = i, freq[i]
			} else if freq[i] <= v2 {
				c2, v2 = i, freq[i]
			}
		}
		if c2 < 0 {
			break
		}
		freq[c1] += freq[c2]
		freq[c2] = 0
		codeSize[c1]++
		for others[c1] >= 0 {
			c1 = others[c1]
			codeSize[c1]++
		}
		others[c1] = c2
		codeSize[c2]++
		for others[c2] >= 0 {
			c2 = others[c2]
			codeSize[c2]++
		}
	}

	// Figure K.2: how many codes of each length. A tree over 257 symbols is
	// at most 256 deep, though in practice the frequencies an image gives
	// keep it far shallower.
	var bits [huffmanMaxSymbols]int
	for i := 0; i < huffmanMaxSymbols; i++ {
		if codeSize[i] > 0 {
			bits[codeSize[i]]++
		}
	}

	// Figure K.3: no code longer than 16 bits. Two codes of the longest
	// length become one a bit shorter, and a shorter code is split to make
	// room for the other, which keeps the prefix property.
	for i := huffmanMaxSymbols - 1; i > 16; i-- {
		for bits[i] > 0 {
			j := i - 2
			for bits[j] == 0 {
				j--
			}
			bits[i] -= 2
			bits[i-1]++
			bits[j+1] += 2
			bits[j]--
		}
	}
	// The reserved symbol held the last code of the longest length; give it
	// back.
	i := 16
	for bits[i] == 0 {
		i--
	}
	bits[i]--

	// Figure K.4: symbols in order of code length, and by value within one.
	var spec huffmanSpec
	for length := 1; length <= 16; length++ {
		spec.counts[length-1] = byte(bits[length])
	}
	// The limiting moved codes between lengths without saying which symbol
	// went where. The order of the symbols by their unlimited lengths is
	// still the right order, and the counts say where each length starts.
	for length := 1; length < huffmanMaxSymbols; length++ {
		for s := 0; s < 256; s++ {
			if codeSize[s] == length {
				spec.symbols = append(spec.symbols, byte(s))
			}
		}
	}
	return spec
}

// huffmanCodes returns the code of each symbol of spec, indexed by symbol,
// derived from the counts as T.81 Annex C does.
func huffmanCodes(spec huffmanSpec) (codes [256]huffmanCode) {
	code, next := uint32(0), 0
	for length := 1; length <= 16; length++ {
		for i := 0; i < int(spec.counts[length-1]); i++ {
			codes[spec.symbols[next]] = huffmanCode{bits: code, length: uint8(length)}
			code++
			next++
		}
		code <<= 1
	}
	return codes
}

// huffmanCost is how many bits coding symbols at these frequencies takes
// with the codes given, the codes alone and not any bits that follow them.
func huffmanCost(frequencies *[256]int, codes *[256]huffmanCode) int {
	cost := 0
	for s, f := range frequencies {
		cost += f * int(codes[s].length)
	}
	return cost
}
