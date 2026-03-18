package network

// Pre-built presentation context collections for all DICOM service types.
// Equivalent to pynetdicom's presentation module collections.

// AllStoragePresentationContexts returns presentation contexts for all storage SOP classes.
func AllStoragePresentationContexts() []PresentationContextItem {
	uids := AllStorageSOPClassUIDs()
	ts := DefaultTransferSyntaxes()
	contexts := make([]PresentationContextItem, len(uids))
	for i, uid := range uids {
		contexts[i] = PresentationContextItem{
			ID:               byte((2*i + 1) % 256),
			AbstractSyntax:   uid,
			TransferSyntaxes: ts,
		}
	}
	return contexts
}

// VerificationPresentationContexts returns presentation contexts for C-ECHO.
func VerificationPresentationContexts() []PresentationContextItem {
	return DefaultVerificationContexts()
}

// QueryRetrievePresentationContexts returns presentation contexts for Q/R services.
func QueryRetrievePresentationContexts() []PresentationContextItem {
	return DefaultQueryRetrieveContexts()
}

// BasicWorklistPresentationContexts returns presentation contexts for Modality Worklist.
func BasicWorklistPresentationContexts() []PresentationContextItem {
	ts := DefaultTransferSyntaxes()
	return []PresentationContextItem{
		{ID: 1, AbstractSyntax: ModalityWorklistInformationModelFindUID, TransferSyntaxes: ts},
	}
}

// StorageCommitmentPresentationContexts returns contexts for Storage Commitment.
func StorageCommitmentPresentationContexts() []PresentationContextItem {
	ts := DefaultTransferSyntaxes()
	return []PresentationContextItem{
		{ID: 1, AbstractSyntax: StorageCommitmentPushModelUID, TransferSyntaxes: ts},
	}
}

// PrintManagementPresentationContexts returns contexts for Print Management.
func PrintManagementPresentationContexts() []PresentationContextItem {
	ts := DefaultTransferSyntaxes()
	uids := []string{
		BasicGrayscalePrintManagementUID,
		BasicColorPrintManagementUID,
		BasicFilmSessionSOPClassUID,
		BasicFilmBoxSOPClassUID,
		BasicGrayscaleImageBoxSOPClassUID,
		BasicColorImageBoxSOPClassUID,
		PrinterSOPClassUID,
		PrintJobSOPClassUID,
		PrinterConfigurationRetrievalUID,
	}
	contexts := make([]PresentationContextItem, len(uids))
	for i, uid := range uids {
		contexts[i] = PresentationContextItem{
			ID:               byte(2*i + 1),
			AbstractSyntax:   uid,
			TransferSyntaxes: ts,
		}
	}
	return contexts
}

// ModalityPerformedProcedurePresentationContexts returns contexts for MPPS.
func ModalityPerformedProcedurePresentationContexts() []PresentationContextItem {
	ts := DefaultTransferSyntaxes()
	uids := []string{
		ModalityPerformedProcedureStepUID,
		ModalityPerformedProcedureStepRetrUID,
		ModalityPerformedProcedureStepNotifUID,
	}
	contexts := make([]PresentationContextItem, len(uids))
	for i, uid := range uids {
		contexts[i] = PresentationContextItem{
			ID:               byte(2*i + 1),
			AbstractSyntax:   uid,
			TransferSyntaxes: ts,
		}
	}
	return contexts
}

// UnifiedProcedureStepPresentationContexts returns contexts for UPS.
func UnifiedProcedureStepPresentationContexts() []PresentationContextItem {
	ts := DefaultTransferSyntaxes()
	uids := []string{
		UnifiedProcedureStepPushUID,
		UnifiedProcedureStepWatchUID,
		UnifiedProcedureStepPullUID,
		UnifiedProcedureStepEventUID,
		UnifiedProcedureStepQueryUID,
	}
	contexts := make([]PresentationContextItem, len(uids))
	for i, uid := range uids {
		contexts[i] = PresentationContextItem{
			ID:               byte(2*i + 1),
			AbstractSyntax:   uid,
			TransferSyntaxes: ts,
		}
	}
	return contexts
}

// InstanceAvailabilityPresentationContexts returns contexts for Instance Availability.
func InstanceAvailabilityPresentationContexts() []PresentationContextItem {
	ts := DefaultTransferSyntaxes()
	return []PresentationContextItem{
		{ID: 1, AbstractSyntax: InstanceAvailabilityNotificationUID, TransferSyntaxes: ts},
	}
}

// SubstanceAdministrationPresentationContexts returns contexts for Substance Administration.
func SubstanceAdministrationPresentationContexts() []PresentationContextItem {
	ts := DefaultTransferSyntaxes()
	uids := []string{
		SubstanceAdministrationLoggingUID,
		ProductCharacteristicsQueryUID,
		SubstanceApprovalQueryUID,
	}
	contexts := make([]PresentationContextItem, len(uids))
	for i, uid := range uids {
		contexts[i] = PresentationContextItem{
			ID:               byte(2*i + 1),
			AbstractSyntax:   uid,
			TransferSyntaxes: ts,
		}
	}
	return contexts
}

// NonPatientObjectPresentationContexts returns contexts for Non-Patient Object Storage.
func NonPatientObjectPresentationContexts() []PresentationContextItem {
	ts := DefaultTransferSyntaxes()
	uids := []string{
		HangingProtocolStorageUID,
		ColorPaletteStorageUID,
		GenericImplantTemplateUID,
		ImplantAssemblyTemplateUID,
		ImplantTemplateGroupUID,
	}
	contexts := make([]PresentationContextItem, len(uids))
	for i, uid := range uids {
		contexts[i] = PresentationContextItem{
			ID:               byte(2*i + 1),
			AbstractSyntax:   uid,
			TransferSyntaxes: ts,
		}
	}
	return contexts
}
