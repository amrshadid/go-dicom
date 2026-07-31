package network_test

import (
	"testing"

	"github.com/amrshadid/go-dicom/network"
)

// The SOP class constants in this package are the sort of thing that looks right
// and is not. Two have already been wrong in ways that mattered: the JPEG
// Lossless syntaxes were named the wrong way round, and the compression table
// listed 1.2.840.10008.1.2.4.71, which the standard does not define at all.
//
// So these are checked against their values rather than assumed, and the context
// helpers are checked to actually carry them.

func TestServiceClassUIDs(t *testing.T) {
	for _, tc := range []struct {
		name string
		got  string
		want string
	}{
		{"General Relevant Patient Information Query", network.GeneralRelevantPatientInfoQueryUID, "1.2.840.10008.5.1.4.37.1"},
		{"Breast Imaging Relevant Patient Information Query", network.BreastImagingRelevantPatientInfoQueryUID, "1.2.840.10008.5.1.4.37.2"},
		{"Cardiac Relevant Patient Information Query", network.CardiacRelevantPatientInfoQueryUID, "1.2.840.10008.5.1.4.37.3"},
		{"Display System", network.DisplaySystemUID, "1.2.840.10008.5.1.1.40"},
		{"Display System well-known instance", network.DisplaySystemInstanceUID, "1.2.840.10008.5.1.1.40.1"},
		{"Media Creation Management", network.MediaCreationManagementUID, "1.2.840.10008.5.1.1.33"},
		{"Procedural Event Logging", network.ProceduralEventLoggingUID, "1.2.840.10008.1.40"},
		{"Substance Administration Logging", network.SubstanceAdministrationLoggingUID, "1.2.840.10008.1.42"},
		{"Unified Procedure Step Push", network.UnifiedProcedureStepPushUID, "1.2.840.10008.5.1.4.34.6.1"},
	} {
		if tc.got != tc.want {
			t.Errorf("%s = %q, want %q", tc.name, tc.got, tc.want)
		}
	}
}

// TestServiceClassContextsCarryTheirSyntaxes checks each helper proposes the
// classes its name promises, with distinct odd IDs.
//
// Presentation context IDs must be odd and unique within an association; a
// helper handing out even or repeated ones produces a request a strict peer
// rejects outright.
func TestServiceClassContextsCarryTheirSyntaxes(t *testing.T) {
	for _, tc := range []struct {
		name     string
		contexts []network.PresentationContextItem
		want     []string
	}{
		{
			"relevant patient information",
			network.RelevantPatientInformationPresentationContexts(),
			[]string{
				network.GeneralRelevantPatientInfoQueryUID,
				network.BreastImagingRelevantPatientInfoQueryUID,
				network.CardiacRelevantPatientInfoQueryUID,
			},
		},
		{"display system", network.DisplaySystemPresentationContexts(),
			[]string{network.DisplaySystemUID}},
		{"media creation", network.MediaCreationManagementPresentationContexts(),
			[]string{network.MediaCreationManagementUID}},
		{"event logging", network.EventLoggingPresentationContexts(),
			[]string{network.ProceduralEventLoggingUID, network.SubstanceAdministrationLoggingUID}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if len(tc.contexts) != len(tc.want) {
				t.Fatalf("got %d contexts, want %d", len(tc.contexts), len(tc.want))
			}
			seen := map[byte]bool{}
			for i, ctx := range tc.contexts {
				if ctx.AbstractSyntax != tc.want[i] {
					t.Errorf("context %d proposes %q, want %q", i, ctx.AbstractSyntax, tc.want[i])
				}
				if ctx.ID%2 == 0 {
					t.Errorf("context %d has even ID %d; presentation context IDs are odd", i, ctx.ID)
				}
				if seen[ctx.ID] {
					t.Errorf("context ID %d is used twice", ctx.ID)
				}
				seen[ctx.ID] = true
				if len(ctx.TransferSyntaxes) == 0 {
					t.Errorf("context %d proposes no transfer syntax", i)
				}
			}
		})
	}
}
