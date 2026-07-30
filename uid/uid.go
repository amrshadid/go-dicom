package uid

import (
	"sort"
	"strings"
)

// UID represents a DICOM Unique Identifier in dotted decimal format.
type UID struct {
	value string
}

// UIDInfo contains detailed information about a UID.
type UIDInfo struct {
	UID         string
	Name        string
	Type        string
	IsRetired   bool
	Description string
}

// New creates a new UID from a string.
func New(value string) UID {
	return UID{value: value}
}

// String returns the string representation of the UID.
func (u UID) String() string {
	return u.value
}

// IsEmpty returns true if the UID is empty.
func (u UID) IsEmpty() bool {
	return u.value == ""
}

// IsValid validates the UID format as dotted decimal.
func (u UID) IsValid() bool {
	if u.value == "" {
		return false
	}
	parts := strings.Split(u.value, ".")
	if len(parts) < 2 {
		return false
	}
	for _, part := range parts {
		if part == "" {
			return false
		}
		// Check if all characters are digits
		for _, ch := range part {
			if ch < '0' || ch > '9' {
				return false
			}
		}
	}
	return true
}

// Equals checks if two UIDs are equal.
func (u UID) Equals(other UID) bool {
	return u.value == other.value
}

// Info returns detailed information about the UID if it exists in the database.
func (u UID) Info() *UIDInfo {
	return GetUIDInfo(u.value)
}

// uids is the global UID database.
var uids = map[string]*UIDInfo{
	// Implicit VR Little Endian
	"1.2.840.10008.1.2": {
		UID:         "1.2.840.10008.1.2",
		Name:        "Implicit VR Little Endian",
		Type:        "TransferSyntax",
		IsRetired:   false,
		Description: "Default transfer syntax for DICOM files",
	},
	// Explicit VR Little Endian
	"1.2.840.10008.1.2.1": {
		UID:         "1.2.840.10008.1.2.1",
		Name:        "Explicit VR Little Endian",
		Type:        "TransferSyntax",
		IsRetired:   false,
		Description: "Standard explicit VR transfer syntax",
	},
	// Explicit VR Big Endian
	"1.2.840.10008.1.2.2": {
		UID:         "1.2.840.10008.1.2.2",
		Name:        "Explicit VR Big Endian",
		Type:        "TransferSyntax",
		IsRetired:   false,
		Description: "Big-endian explicit VR transfer syntax",
	},
	// JPEG Baseline (Process 1)
	"1.2.840.10008.1.2.4.50": {
		UID:         "1.2.840.10008.1.2.4.50",
		Name:        "JPEG Baseline (Process 1)",
		Type:        "TransferSyntax",
		IsRetired:   false,
		Description: "JPEG lossy compression",
	},
	// JPEG Extended (Process 2 & 4)
	"1.2.840.10008.1.2.4.51": {
		UID:         "1.2.840.10008.1.2.4.51",
		Name:        "JPEG Extended (Process 2 & 4)",
		Type:        "TransferSyntax",
		IsRetired:   false,
		Description: "JPEG extended lossy compression",
	},
	// JPEG Lossless, Non-Hierarchical (Process 14)
	"1.2.840.10008.1.2.4.57": {
		UID:         "1.2.840.10008.1.2.4.57",
		Name:        "JPEG Lossless, Non-Hierarchical (Process 14)",
		Type:        "TransferSyntax",
		IsRetired:   false,
		Description: "JPEG lossless compression, any prediction selection value",
	},
	// JPEG Lossless, Non-Hierarchical, First-Order Prediction
	// (Process 14 [Selection Value 1]) — the one archives use in practice.
	"1.2.840.10008.1.2.4.70": {
		UID:         "1.2.840.10008.1.2.4.70",
		Name:        "JPEG Lossless, Non-Hierarchical, First-Order Prediction (Process 14 [Selection Value 1])",
		Type:        "TransferSyntax",
		IsRetired:   false,
		Description: "JPEG lossless compression fixed to selection value 1",
	},
	// JPEG-LS Lossless
	"1.2.840.10008.1.2.4.80": {
		UID:         "1.2.840.10008.1.2.4.80",
		Name:        "JPEG-LS Lossless",
		Type:        "TransferSyntax",
		IsRetired:   false,
		Description: "JPEG-LS lossless compression",
	},
	// JPEG-LS Lossy (Near-Lossless)
	"1.2.840.10008.1.2.4.81": {
		UID:         "1.2.840.10008.1.2.4.81",
		Name:        "JPEG-LS Lossy (Near-Lossless)",
		Type:        "TransferSyntax",
		IsRetired:   false,
		Description: "JPEG-LS near-lossless compression",
	},
	// JPEG 2000 Lossless
	"1.2.840.10008.1.2.4.90": {
		UID:         "1.2.840.10008.1.2.4.90",
		Name:        "JPEG 2000 Lossless",
		Type:        "TransferSyntax",
		IsRetired:   false,
		Description: "JPEG 2000 lossless compression",
	},
	// JPEG 2000 Lossy
	"1.2.840.10008.1.2.4.91": {
		UID:         "1.2.840.10008.1.2.4.91",
		Name:        "JPEG 2000 Lossy",
		Type:        "TransferSyntax",
		IsRetired:   false,
		Description: "JPEG 2000 lossy compression",
	},
	// JPEG 2000 Part 2 Multicomponent, Lossless only
	"1.2.840.10008.1.2.4.92": {
		UID:         "1.2.840.10008.1.2.4.92",
		Name:        "JPEG 2000 Part 2 Multicomponent, Lossless only",
		Type:        "TransferSyntax",
		IsRetired:   false,
		Description: "JPEG 2000 Part 2 multicomponent lossless",
	},
	// JPEG 2000 Part 2 Multicomponent, Lossy only
	"1.2.840.10008.1.2.4.93": {
		UID:         "1.2.840.10008.1.2.4.93",
		Name:        "JPEG 2000 Part 2 Multicomponent, Lossy only",
		Type:        "TransferSyntax",
		IsRetired:   false,
		Description: "JPEG 2000 Part 2 multicomponent lossy",
	},
	// RLE Lossless
	"1.2.840.10008.1.2.5": {
		UID:         "1.2.840.10008.1.2.5",
		Name:        "RLE Lossless",
		Type:        "TransferSyntax",
		IsRetired:   false,
		Description: "Run-Length Encoding lossless compression",
	},
	// Verification SOP Class
	"1.2.840.10008.1.1": {
		UID:         "1.2.840.10008.1.1",
		Name:        "Verification SOP Class",
		Type:        "SOPClass",
		IsRetired:   false,
		Description: "Used for verification of DICOM communication",
	},
	// CR Image Storage
	"1.2.840.10008.5.1.4.1.1.2": {
		UID:         "1.2.840.10008.5.1.4.1.1.2",
		Name:        "CR Image Storage",
		Type:        "SOPClass",
		IsRetired:   false,
		Description: "Computed Radiography image storage",
	},
	// CT Image Storage
	"1.2.840.10008.5.1.4.1.1.2.1": {
		UID:         "1.2.840.10008.5.1.4.1.1.2.1",
		Name:        "CT Image Storage",
		Type:        "SOPClass",
		IsRetired:   false,
		Description: "Computed Tomography image storage",
	},
	// MR Image Storage
	"1.2.840.10008.5.1.4.1.1.4": {
		UID:         "1.2.840.10008.5.1.4.1.1.4",
		Name:        "MR Image Storage",
		Type:        "SOPClass",
		IsRetired:   false,
		Description: "Magnetic Resonance image storage",
	},
	// Ultrasound Image Storage
	"1.2.840.10008.5.1.4.1.1.6.4": {
		UID:         "1.2.840.10008.5.1.4.1.1.6.4",
		Name:        "Ultrasound Image Storage",
		Type:        "SOPClass",
		IsRetired:   false,
		Description: "Ultrasound image storage",
	},
}

// GetUIDInfo retrieves information about a UID from the database.
func GetUIDInfo(uid string) *UIDInfo {
	if info, ok := uids[uid]; ok {
		return info
	}
	return nil
}

// IsTransferSyntax checks if a UID is a transfer syntax.
func IsTransferSyntax(uid string) bool {
	info := GetUIDInfo(uid)
	return info != nil && info.Type == "TransferSyntax"
}

// IsSOPClass checks if a UID is a SOP class.
func IsSOPClass(uid string) bool {
	info := GetUIDInfo(uid)
	return info != nil && info.Type == "SOPClass"
}

// AllUIDs returns all registered UIDs sorted.
func AllUIDs() []UID {
	result := make([]UID, 0, len(uids))
	for uidStr := range uids {
		result = append(result, New(uidStr))
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].String() < result[j].String()
	})
	return result
}

// AllUIDInfos returns all registered UID information structures sorted.
func AllUIDInfos() []*UIDInfo {
	infos := make([]*UIDInfo, 0, len(uids))
	for _, info := range uids {
		infos = append(infos, info)
	}
	sort.Slice(infos, func(i, j int) bool {
		return infos[i].UID < infos[j].UID
	})
	return infos
}

// GetByName finds a UID by its name in a case-insensitive manner.
func GetByName(name string) *UID {
	name = strings.ToLower(strings.TrimSpace(name))
	for _, info := range uids {
		if strings.ToLower(info.Name) == name {
			u := New(info.UID)
			return &u
		}
	}
	return nil
}

// GetByType returns all UIDs of a specific type sorted.
func GetByType(typeStr string) []UID {
	var result []UID
	for _, info := range uids {
		if info.Type == typeStr {
			result = append(result, New(info.UID))
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].String() < result[j].String()
	})
	return result
}

// LittleEndianTransferSyntaxes returns common little-endian transfer syntaxes.
func LittleEndianTransferSyntaxes() []UID {
	return []UID{
		New("1.2.840.10008.1.2"),   // Implicit VR Little Endian
		New("1.2.840.10008.1.2.1"), // Explicit VR Little Endian
	}
}

// BigEndianTransferSyntax returns the big-endian explicit VR transfer syntax UID.
func BigEndianTransferSyntax() UID {
	return New("1.2.840.10008.1.2.2")
}

// ImplicitVRLittleEndian returns the implicit VR little endian transfer syntax UID.
func ImplicitVRLittleEndian() UID {
	return New("1.2.840.10008.1.2")
}

// ExplicitVRLittleEndian returns the explicit VR little endian transfer syntax UID.
func ExplicitVRLittleEndian() UID {
	return New("1.2.840.10008.1.2.1")
}

// CompressedTransferSyntaxes returns all compressed transfer syntax UIDs.
func CompressedTransferSyntaxes() []UID {
	return []UID{
		New("1.2.840.10008.1.2.4.50"), // JPEG Baseline
		New("1.2.840.10008.1.2.4.51"), // JPEG Extended
		New("1.2.840.10008.1.2.4.57"), // JPEG Lossless (Process 14)
		New("1.2.840.10008.1.2.4.70"), // JPEG Lossless (Process 14 SV1)
		New("1.2.840.10008.1.2.4.80"), // JPEG-LS Lossless
		New("1.2.840.10008.1.2.4.81"), // JPEG-LS Lossy
		New("1.2.840.10008.1.2.4.90"), // JPEG 2000 Lossless
		New("1.2.840.10008.1.2.4.91"), // JPEG 2000 Lossy
		New("1.2.840.10008.1.2.5"),    // RLE Lossless
	}
}

// IsCompressed checks if a UID represents a compressed transfer syntax.
func IsCompressed(uid UID) bool {
	compressed := CompressedTransferSyntaxes()
	for _, c := range compressed {
		if c.Equals(uid) {
			return true
		}
	}
	return false
}

// IsLossless checks if a UID represents a lossless transfer syntax.
func IsLossless(uid UID) bool {
	lossless := []string{
		"1.2.840.10008.1.2",      // Implicit VR Little Endian
		"1.2.840.10008.1.2.1",    // Explicit VR Little Endian
		"1.2.840.10008.1.2.2",    // Explicit VR Big Endian
		"1.2.840.10008.1.2.4.57", // JPEG Lossless (Process 14)
		"1.2.840.10008.1.2.4.70", // JPEG Lossless (Process 14 SV1)
		"1.2.840.10008.1.2.4.80", // JPEG-LS Lossless
		"1.2.840.10008.1.2.4.90", // JPEG 2000 Lossless
		"1.2.840.10008.1.2.4.92", // JPEG 2000 Part 2 Multicomponent Lossless
		"1.2.840.10008.1.2.5",    // RLE Lossless
	}
	for _, l := range lossless {
		if uid.String() == l {
			return true
		}
	}
	return false
}

// SupportsMultipleFrames checks if a UID supports multiple frames.
func SupportsMultipleFrames(uid UID) bool {
	info := uid.Info()
	if info == nil {
		return false
	}
	return info.Type == "TransferSyntax"
}

// VerificationSOPClass returns the Verification SOP class UID.
func VerificationSOPClass() UID {
	return New("1.2.840.10008.1.1")
}

// CTImageStorage returns the CT Image Storage SOP class UID.
func CTImageStorage() UID {
	return New("1.2.840.10008.5.1.4.1.1.2.1")
}

// MRImageStorage returns the MR Image Storage SOP class UID.
func MRImageStorage() UID {
	return New("1.2.840.10008.5.1.4.1.1.4")
}
