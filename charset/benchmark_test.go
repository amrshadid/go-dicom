package charset_test

import (
	"testing"

	"github.com/amrshadid/go-dicom/charset"
)

// Benchmark encoding conversion
func BenchmarkConvertEncodings_Single(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_, _ = charset.ConvertEncodings([]string{"ISO_IR 192"})
	}
}

func BenchmarkConvertEncodings_Multi(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_, _ = charset.ConvertEncodings([]string{"ISO 2022 IR 87", "ISO 2022 IR 13"})
	}
}

// Benchmark simple text decoding
func BenchmarkDecodeBytes_ASCII_Small(b *testing.B) {
	data := []byte("Hello World")
	encodings := []string{"ISO-8859-1"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = charset.DecodeBytes(data, encodings, charset.DefaultTextDelimiters)
	}
}

func BenchmarkDecodeBytes_ASCII_Large(b *testing.B) {
	data := make([]byte, 1000)
	for i := range data {
		data[i] = byte('A' + (i % 26))
	}
	encodings := []string{"ISO-8859-1"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = charset.DecodeBytes(data, encodings, charset.DefaultTextDelimiters)
	}
}

func BenchmarkDecodeBytes_UTF8_Small(b *testing.B) {
	data := []byte("Hello 世界")
	encodings := []string{"UTF-8"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = charset.DecodeBytes(data, encodings, charset.DefaultTextDelimiters)
	}
}

func BenchmarkDecodeBytes_UTF8_Large(b *testing.B) {
	// Mix of ASCII and multi-byte characters
	data := []byte("Hello World こんにちは 世界 Καλημέρα κόσμε Здравствуй мир مرحبا بالعالم ")
	// Repeat to make it larger
	for i := 0; i < 10; i++ {
		data = append(data, data...)
	}
	encodings := []string{"UTF-8"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = charset.DecodeBytes(data, encodings, charset.DefaultTextDelimiters)
	}
}

// Benchmark encoding
func BenchmarkEncodeString_ASCII_Small(b *testing.B) {
	text := "Hello World"
	encodings := []string{"ISO-8859-1"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = charset.EncodeString(text, encodings)
	}
}

func BenchmarkEncodeString_ASCII_Large(b *testing.B) {
	text := ""
	for i := 0; i < 1000; i++ {
		text += string(rune('A' + (i % 26)))
	}
	encodings := []string{"ISO-8859-1"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = charset.EncodeString(text, encodings)
	}
}

func BenchmarkEncodeString_UTF8_Small(b *testing.B) {
	text := "Hello 世界"
	encodings := []string{"UTF-8"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = charset.EncodeString(text, encodings)
	}
}

func BenchmarkEncodeString_UTF8_Large(b *testing.B) {
	text := "Hello World こんにちは 世界 Καλημέρα κόσμε Здравствуй мир مرحبا بالعالم "
	// Repeat to make it larger
	for i := 0; i < 10; i++ {
		text += text
	}
	encodings := []string{"UTF-8"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = charset.EncodeString(text, encodings)
	}
}

// Benchmark PersonName operations
func BenchmarkDecodePersonName_Simple(b *testing.B) {
	data := []byte("Doe^John")
	encodings := []string{"ISO-8859-1"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = charset.DecodePersonName(data, encodings)
	}
}

func BenchmarkDecodePersonName_ThreeGroups(b *testing.B) {
	data := []byte("Yamada^Tarou=山田^太郎=やまだ^たろう")
	encodings := []string{"UTF-8"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = charset.DecodePersonName(data, encodings)
	}
}

func BenchmarkEncodePersonName_Simple(b *testing.B) {
	pn := charset.NewPersonName("Doe^John", "", "")
	encodings := []string{"ISO-8859-1"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = charset.EncodePersonName(pn, encodings)
	}
}

func BenchmarkEncodePersonName_ThreeGroups(b *testing.B) {
	pn := charset.NewPersonName("Yamada^Tarou", "山田^太郎", "やまだ^たろう")
	encodings := []string{"UTF-8"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = charset.EncodePersonName(pn, encodings)
	}
}

// Benchmark escape sequence handling
func BenchmarkFindEscapeSequences_NoEscape(b *testing.B) {
	data := []byte("Hello World this is a test string with no escape sequences")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = charset.FindEscapeSequences(data)
	}
}

func BenchmarkFindEscapeSequences_MultipleEscape(b *testing.B) {
	data := []byte{
		0x1B, '(', 'B', 'H', 'e', 'l', 'l', 'o',
		0x1B, '$', 'B', 'W', 'o', 'r', 'l', 'd',
		0x1B, '(', 'B', 'T', 'e', 's', 't',
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = charset.FindEscapeSequences(data)
	}
}

func BenchmarkSplitByEscapeSequences_NoEscape(b *testing.B) {
	data := []byte("Hello World this is a test string with no escape sequences")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = charset.SplitByEscapeSequences(data)
	}
}

func BenchmarkSplitByEscapeSequences_MultipleEscape(b *testing.B) {
	data := []byte{
		0x1B, '(', 'B', 'H', 'e', 'l', 'l', 'o',
		0x1B, '$', 'B', 'W', 'o', 'r', 'l', 'd',
		0x1B, '(', 'B', 'T', 'e', 's', 't',
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = charset.SplitByEscapeSequences(data)
	}
}

// Benchmark helper functions
func BenchmarkDecodeString(b *testing.B) {
	data := []byte("Hello World")
	encoding := "ISO_IR 192"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = charset.DecodeString(data, encoding)
	}
}

func BenchmarkEncodeToBytes(b *testing.B) {
	text := "Hello World"
	encoding := "ISO_IR 192"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = charset.EncodeToBytes(text, encoding)
	}
}

func BenchmarkValidateEncoding(b *testing.B) {
	encoding := "ISO_IR 192"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = charset.ValidateEncoding(encoding)
	}
}

func BenchmarkGetEncodingInfo(b *testing.B) {
	encoding := "ISO_IR 192"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = charset.GetEncodingInfo(encoding)
	}
}

func BenchmarkNewCharacterSet(b *testing.B) {
	values := []string{"ISO_IR 192"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = charset.NewCharacterSet(values)
	}
}

// Benchmark round-trip operations
func BenchmarkRoundTrip_ASCII(b *testing.B) {
	text := "Hello World"
	encodings := []string{"ISO-8859-1"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		encoded, _ := charset.EncodeString(text, encodings)
		_, _ = charset.DecodeBytes(encoded, encodings, charset.DefaultTextDelimiters)
	}
}

func BenchmarkRoundTrip_UTF8(b *testing.B) {
	text := "Hello 世界"
	encodings := []string{"UTF-8"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		encoded, _ := charset.EncodeString(text, encodings)
		_, _ = charset.DecodeBytes(encoded, encodings, charset.DefaultTextDelimiters)
	}
}

func BenchmarkRoundTrip_PersonName(b *testing.B) {
	pn := charset.NewPersonName("Yamada^Tarou", "山田^太郎", "やまだ^たろう")
	encodings := []string{"UTF-8"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		encoded, _ := charset.EncodePersonName(pn, encodings)
		_, _ = charset.DecodePersonName(encoded, encodings)
	}
}

// Benchmark multi-valued operations
func BenchmarkSplitMultiValue_Single(b *testing.B) {
	value := "value1"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = charset.SplitMultiValue(value)
	}
}

func BenchmarkSplitMultiValue_Multiple(b *testing.B) {
	value := "value1\\value2\\value3\\value4\\value5"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = charset.SplitMultiValue(value)
	}
}

func BenchmarkJoinMultiValue(b *testing.B) {
	values := []string{"value1", "value2", "value3", "value4", "value5"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = charset.JoinMultiValue(values)
	}
}

// Benchmark different character set types
func BenchmarkDecode_Latin1(b *testing.B) {
	data := []byte{0xE9, 0xE8, 0xE0, 0xF1, 0xFC} // éèàñü
	encodings := []string{"ISO-8859-1"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = charset.DecodeBytes(data, encodings, charset.DefaultTextDelimiters)
	}
}

func BenchmarkDecode_Greek(b *testing.B) {
	data := []byte("Ελληνικά")
	encodings := []string{"UTF-8"} // Using UTF-8 for Greek

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = charset.DecodeBytes(data, encodings, charset.DefaultTextDelimiters)
	}
}

func BenchmarkDecode_Cyrillic(b *testing.B) {
	data := []byte("Русский")
	encodings := []string{"UTF-8"} // Using UTF-8 for Cyrillic

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = charset.DecodeBytes(data, encodings, charset.DefaultTextDelimiters)
	}
}

func BenchmarkDecode_Arabic(b *testing.B) {
	data := []byte("مرحبا")
	encodings := []string{"UTF-8"} // Using UTF-8 for Arabic

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = charset.DecodeBytes(data, encodings, charset.DefaultTextDelimiters)
	}
}
