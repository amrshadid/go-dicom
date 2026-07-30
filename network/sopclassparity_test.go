package network_test

import (
	"testing"

	"github.com/amrshadid/go-dicom/network"
)

// TestSOPClassUIDValues checks the UIDs added for parity with pynetdicom hold
// the values pynetdicom holds.
//
// Worth asserting rather than assuming: this package has already shipped a
// compression table naming 1.2.840.10008.1.2.4.71, which the standard does not
// define, and two JPEG Lossless constants named the wrong way round. A UID that
// looks plausible and is wrong fails at association time against a real peer and
// nowhere else.
func TestSOPClassUIDValues(t *testing.T) {
	for _, tc := range []struct {
		name string
		got  string
		want string
	}{
		{"HangingProtocolInformationModelFindUID", network.HangingProtocolInformationModelFindUID, "1.2.840.10008.5.1.4.38.2"},
		{"HangingProtocolInformationModelMoveUID", network.HangingProtocolInformationModelMoveUID, "1.2.840.10008.5.1.4.38.3"},
		{"HangingProtocolInformationModelGetUID", network.HangingProtocolInformationModelGetUID, "1.2.840.10008.5.1.4.38.4"},
		{"ColorPaletteInformationModelFindUID", network.ColorPaletteInformationModelFindUID, "1.2.840.10008.5.1.4.39.2"},
		{"ColorPaletteInformationModelMoveUID", network.ColorPaletteInformationModelMoveUID, "1.2.840.10008.5.1.4.39.3"},
		{"ColorPaletteInformationModelGetUID", network.ColorPaletteInformationModelGetUID, "1.2.840.10008.5.1.4.39.4"},
		{"DefinedProcedureProtocolInformationModelFindUID", network.DefinedProcedureProtocolInformationModelFindUID, "1.2.840.10008.5.1.4.20.1"},
		{"DefinedProcedureProtocolInformationModelMoveUID", network.DefinedProcedureProtocolInformationModelMoveUID, "1.2.840.10008.5.1.4.20.2"},
		{"DefinedProcedureProtocolInformationModelGetUID", network.DefinedProcedureProtocolInformationModelGetUID, "1.2.840.10008.5.1.4.20.3"},
		{"GenericImplantTemplateInformationModelFindUID", network.GenericImplantTemplateInformationModelFindUID, "1.2.840.10008.5.1.4.43.2"},
		{"GenericImplantTemplateInformationModelMoveUID", network.GenericImplantTemplateInformationModelMoveUID, "1.2.840.10008.5.1.4.43.3"},
		{"GenericImplantTemplateInformationModelGetUID", network.GenericImplantTemplateInformationModelGetUID, "1.2.840.10008.5.1.4.43.4"},
		{"ImplantAssemblyTemplateInformationModelFindUID", network.ImplantAssemblyTemplateInformationModelFindUID, "1.2.840.10008.5.1.4.44.2"},
		{"ImplantAssemblyTemplateInformationModelMoveUID", network.ImplantAssemblyTemplateInformationModelMoveUID, "1.2.840.10008.5.1.4.44.3"},
		{"ImplantAssemblyTemplateInformationModelGetUID", network.ImplantAssemblyTemplateInformationModelGetUID, "1.2.840.10008.5.1.4.44.4"},
		{"ImplantTemplateGroupInformationModelFindUID", network.ImplantTemplateGroupInformationModelFindUID, "1.2.840.10008.5.1.4.45.2"},
		{"ImplantTemplateGroupInformationModelMoveUID", network.ImplantTemplateGroupInformationModelMoveUID, "1.2.840.10008.5.1.4.45.3"},
		{"ImplantTemplateGroupInformationModelGetUID", network.ImplantTemplateGroupInformationModelGetUID, "1.2.840.10008.5.1.4.45.4"},
		{"ProtocolApprovalInformationModelFindUID", network.ProtocolApprovalInformationModelFindUID, "1.2.840.10008.5.1.4.1.1.200.4"},
		{"ProtocolApprovalInformationModelMoveUID", network.ProtocolApprovalInformationModelMoveUID, "1.2.840.10008.5.1.4.1.1.200.5"},
		{"ProtocolApprovalInformationModelGetUID", network.ProtocolApprovalInformationModelGetUID, "1.2.840.10008.5.1.4.1.1.200.6"},
		{"CompositeInstanceRootRetrieveMoveUID", network.CompositeInstanceRootRetrieveMoveUID, "1.2.840.10008.5.1.4.1.2.4.2"},
		{"CompositeInstanceRootRetrieveGetUID", network.CompositeInstanceRootRetrieveGetUID, "1.2.840.10008.5.1.4.1.2.4.3"},
		{"CompositeInstanceRetrieveWithoutBulkDataUID", network.CompositeInstanceRetrieveWithoutBulkDataUID, "1.2.840.10008.5.1.4.1.2.5.3"},
		{"InventoryFindUID", network.InventoryFindUID, "1.2.840.10008.5.1.4.1.1.201.2"},
		{"InventoryMoveUID", network.InventoryMoveUID, "1.2.840.10008.5.1.4.1.1.201.3"},
		{"InventoryGetUID", network.InventoryGetUID, "1.2.840.10008.5.1.4.1.1.201.4"},
		{"InventoryCreationUID", network.InventoryCreationUID, "1.2.840.10008.5.1.4.1.1.201.5"},
		{"RepositoryQueryUID", network.RepositoryQueryUID, "1.2.840.10008.5.1.4.1.1.201.6"},
		{"StorageManagementInstanceUID", network.StorageManagementInstanceUID, "1.2.840.10008.5.1.4.1.1.201.1.1"},
		{"RTConventionalMachineVerificationUID", network.RTConventionalMachineVerificationUID, "1.2.840.10008.5.1.4.34.8"},
		{"RTIonMachineVerificationUID", network.RTIonMachineVerificationUID, "1.2.840.10008.5.1.4.34.9"},
		{"ProceduralEventLoggingInstanceUID", network.ProceduralEventLoggingInstanceUID, "1.2.840.10008.1.40.1"},
		{"SubstanceAdministrationLoggingInstanceUID", network.SubstanceAdministrationLoggingInstanceUID, "1.2.840.10008.1.42.1"},
		{"PrinterInstanceUID", network.PrinterInstanceUID, "1.2.840.10008.5.1.1.17"},
		{"PrinterConfigurationRetrievalInstanceUID", network.PrinterConfigurationRetrievalInstanceUID, "1.2.840.10008.5.1.1.17.376"},
		{"UPSGlobalSubscriptionInstanceUID", network.UPSGlobalSubscriptionInstanceUID, "1.2.840.10008.5.1.4.34.5"},
		{"UPSFilteredGlobalSubscriptionInstanceUID", network.UPSFilteredGlobalSubscriptionInstanceUID, "1.2.840.10008.5.1.4.34.5.1"},
		{"BasicAnnotationBoxSOPClassUID", network.BasicAnnotationBoxSOPClassUID, "1.2.840.10008.5.1.1.15"},
		{"PresentationLUTSOPClassUID", network.PresentationLUTSOPClassUID, "1.2.840.10008.5.1.1.23"},
	} {
		if tc.got != tc.want {
			t.Errorf("%s = %q, want %q", tc.name, tc.got, tc.want)
		}
	}
}
