package tag_test

import (
	"testing"

	"github.com/amrshadid/go-dicom/tag"
)

// Test private dictionary functionality
func TestPrivateDictionary(t *testing.T) {
	// Test that we have private tags
	count := tag.CountPrivateTags()
	if count < 5000 {
		t.Errorf("Expected at least 5,000 private tags, got %d", count)
	}

	// Test that we have vendors
	vendors := tag.GetAllPrivateVendors()
	if len(vendors) < 300 {
		t.Errorf("Expected at least 300 vendors, got %d", len(vendors))
	}

	// Verify some known vendors exist
	hasGEMS := false
	hasPhilips := false
	for _, vendor := range vendors {
		if len(vendor) > 5 && vendor[:5] == "GEMS_" {
			hasGEMS = true
		}
		if len(vendor) > 7 && vendor[:7] == "Philips" {
			hasPhilips = true
		}
	}

	if !hasGEMS {
		t.Error("Expected to find GE GEMS vendors")
	}
	if !hasPhilips {
		t.Error("Expected to find Philips vendors")
	}
}

// Test getting private tags by vendor
func TestGetPrivateTagsByVendor(t *testing.T) {
	// Get tags for a known vendor
	vendorTags := tag.GetPrivateTagsByVendor("GEMS_PARM_01")

	if len(vendorTags) == 0 {
		t.Error("Expected tags for GEMS_PARM_01 vendor")
	}

	// Verify tags are properly structured
	for tagInt, info := range vendorTags {
		if info.VR == "" {
			t.Errorf("Tag 0x%08X has empty VR", tagInt)
		}
		if info.Name == "" {
			t.Errorf("Tag 0x%08X has empty Name", tagInt)
		}
	}
}

// Test getting private tag info
func TestGetPrivateTagInfo(t *testing.T) {
	// Create a private tag (odd group number)
	privateTag := tag.New(0x000B, 0x0010) // Private group

	// This should return nil for non-existent vendor
	info := privateTag.GetPrivateTagInfo("NonExistentVendor")
	if info != nil {
		t.Error("Expected nil for non-existent vendor")
	}

	// Standard tags should return nil
	stdTag := tag.New(0x0010, 0x0010) // Patient Name
	info = stdTag.GetPrivateTagInfo("GEMS_PARM_01")
	if info != nil {
		t.Error("Expected nil for standard (non-private) tag")
	}
}

// Benchmark private tag lookups
func BenchmarkCountPrivateTags(b *testing.B) {
	for i := 0; i < b.N; i++ {
		tag.CountPrivateTags()
	}
}

func BenchmarkGetAllPrivateVendors(b *testing.B) {
	for i := 0; i < b.N; i++ {
		tag.GetAllPrivateVendors()
	}
}

func BenchmarkGetPrivateTagsByVendor(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tag.GetPrivateTagsByVendor("GEMS_PARM_01")
	}
}
