package dataelem

import (
	"fmt"

	"github.com/amrshadid/go-dicom/tag"
)

// RawDataElement represents an immutable DICOM data element in its raw form,
// before any value conversion or processing. This is analogous to Python's
// RawDataElement NamedTuple.
//
// RawDataElement is used during DICOM file parsing to store the raw bytes
// and metadata before converting to a fully-typed DataElement. This allows
// for lazy evaluation and preserves the original byte representation.
type RawDataElement struct {
	tag               tag.Tag // The DICOM tag
	vr                VR      // Value Representation
	length            uint32  // Value length in bytes
	value             []byte  // Raw value bytes (nil for undefined length)
	valueOffset       int64   // Offset in file where value starts
	isImplicitVR      bool    // Whether this was read with implicit VR
	isLittleEndian    bool    // Byte order
	isUndefinedLength bool    // Whether length is undefined (0xFFFFFFFF)
}

// NewRawDataElement creates a new immutable RawDataElement.
// Once created, the fields cannot be modified (immutability enforced by unexported fields).
func NewRawDataElement(
	tag tag.Tag,
	vr VR,
	length uint32,
	value []byte,
	valueOffset int64,
	isImplicitVR bool,
	isLittleEndian bool,
	isUndefinedLength bool,
) *RawDataElement {
	// Make a copy of the value slice to ensure immutability
	var valueCopy []byte
	if value != nil {
		valueCopy = make([]byte, len(value))
		copy(valueCopy, value)
	}

	return &RawDataElement{
		tag:               tag,
		vr:                vr,
		length:            length,
		value:             valueCopy,
		valueOffset:       valueOffset,
		isImplicitVR:      isImplicitVR,
		isLittleEndian:    isLittleEndian,
		isUndefinedLength: isUndefinedLength,
	}
}

// Tag returns the DICOM tag.
func (raw *RawDataElement) Tag() tag.Tag {
	return raw.tag
}

// VR returns the Value Representation.
func (raw *RawDataElement) VR() VR {
	return raw.vr
}

// Length returns the value length in bytes.
func (raw *RawDataElement) Length() uint32 {
	return raw.length
}

// Value returns a copy of the raw value bytes to preserve immutability.
func (raw *RawDataElement) Value() []byte {
	if raw.value == nil {
		return nil
	}
	valueCopy := make([]byte, len(raw.value))
	copy(valueCopy, raw.value)
	return valueCopy
}

// ValueOffset returns the offset in the file where the value starts.
func (raw *RawDataElement) ValueOffset() int64 {
	return raw.valueOffset
}

// IsImplicitVR returns whether this element was read with implicit VR transfer syntax.
func (raw *RawDataElement) IsImplicitVR() bool {
	return raw.isImplicitVR
}

// IsLittleEndian returns the byte order used.
func (raw *RawDataElement) IsLittleEndian() bool {
	return raw.isLittleEndian
}

// IsUndefinedLength returns whether the length is undefined (0xFFFFFFFF).
// This is common for sequences and encapsulated pixel data.
func (raw *RawDataElement) IsUndefinedLength() bool {
	return raw.isUndefinedLength
}

// String returns a string representation of the raw data element.
func (raw *RawDataElement) String() string {
	return fmt.Sprintf("RawDataElement(tag=%s, VR=%s, length=%d, implicitVR=%v, littleEndian=%v, undefinedLength=%v)",
		raw.tag.String(),
		raw.vr,
		raw.length,
		raw.isImplicitVR,
		raw.isLittleEndian,
		raw.isUndefinedLength,
	)
}

// Equals compares two RawDataElements for equality.
func (raw *RawDataElement) Equals(other *RawDataElement) bool {
	if other == nil {
		return false
	}

	if raw.tag != other.tag {
		return false
	}

	if raw.vr != other.vr {
		return false
	}

	if raw.length != other.length {
		return false
	}

	if raw.valueOffset != other.valueOffset {
		return false
	}

	if raw.isImplicitVR != other.isImplicitVR {
		return false
	}

	if raw.isLittleEndian != other.isLittleEndian {
		return false
	}

	if raw.isUndefinedLength != other.isUndefinedLength {
		return false
	}

	// Compare value bytes
	if len(raw.value) != len(other.value) {
		return false
	}

	for i := range raw.value {
		if raw.value[i] != other.value[i] {
			return false
		}
	}

	return true
}
