package network

// Comprehensive DICOM status codes organized by service class.
// Reference: DICOM PS3.4, pynetdicom status.py

// --- General Status Codes (all services) ---
const (
	StatusRefusedOutOfResources       uint16 = 0x0112
	StatusRefusedSOPClassNotSupported uint16 = 0x0122
	StatusRefusedNotAuthorized        uint16 = 0x0124
	StatusInvalidArgumentValue        uint16 = 0x0115
	StatusInvalidObjectInstance       uint16 = 0x0117
	StatusMissingAttribute            uint16 = 0x0120
	StatusMistypedArgument            uint16 = 0x0212
	StatusNoSuchArgument              uint16 = 0x0114
	StatusNoSuchSOPClass              uint16 = 0x0118
	StatusProcessingFailure           uint16 = 0x0110
	StatusResourceLimitation          uint16 = 0x0213
	StatusUnrecognizedOperation       uint16 = 0x0211
	StatusDuplicateInvocation         uint16 = 0x0210
)

// --- Storage Service Status Codes ---
const (
	StatusStorageCoercionOfDataElements  uint16 = 0xB000
	StatusStorageDataSetNotMatchSOPClass uint16 = 0xB007
	StatusStorageElementsDiscarded       uint16 = 0xB006
)

// --- Query/Retrieve Service Status Codes ---
const (
	StatusQROptionalKeysNotSupported  uint16 = 0x0001
	StatusQRSubOpsOneOrMoreFailures   uint16 = 0xB000
	StatusQRRefusedOutOfResourcesFind uint16 = 0xA700
	StatusQRRefusedOutOfResourcesMove uint16 = 0xA701
	StatusQRIdentifierNotMatch        uint16 = 0xA900
	StatusQRMoveDestinationUnknown    uint16 = 0xA801
	StatusQRCancelMatchingTerminated  uint16 = 0xFE00
	StatusQRPendingMatches            uint16 = 0xFF00
	StatusQRPendingMatchesWarning     uint16 = 0xFF01
)

// --- Print Management Service Status Codes ---
const (
	StatusPrintFilmSessionEmpty        uint16 = 0xB600
	StatusPrintFilmSessionPrintingDone uint16 = 0xB601
	StatusPrintFilmSessionSomePrinted  uint16 = 0xB602
	StatusPrintFilmBoxEmpty            uint16 = 0xB603
	StatusPrintImageDemagnified        uint16 = 0xB604
	StatusPrintMinMaxDensityOutOfRange uint16 = 0xB605
	StatusPrintImageCropped            uint16 = 0xB609
	StatusPrintImageDecimated          uint16 = 0xB60A
)

// --- Modality Worklist Status Codes ---
const (
	StatusWorklistRefusedOutOfResources uint16 = 0xA700
	StatusWorklistIdentifierNotMatch    uint16 = 0xA900
	StatusWorklistCancelMatchTerminated uint16 = 0xFE00
	StatusWorklistPendingMatches        uint16 = 0xFF00
	StatusWorklistPendingMatchesWarning uint16 = 0xFF01
)

// --- Unified Procedure Step Status Codes ---
const (
	StatusUPSUnknownActionType   uint16 = 0xC300
	StatusUPSRefusedNotUpdatable uint16 = 0xC301
	StatusUPSCannotDelete        uint16 = 0xC302
	StatusUPSAlreadyCompleted    uint16 = 0xC303
	StatusUPSNoSuchProcedureStep uint16 = 0xC307
	StatusUPSAlreadyInProgress   uint16 = 0xC310
)

// --- Storage Commitment Status Codes ---
const (
	StatusStorageCommitmentRefused            uint16 = 0x0110
	StatusStorageCommitmentNoSuchObject       uint16 = 0x0112
	StatusStorageCommitmentResourceLimitation uint16 = 0xA700
)

// StatusCategory represents the category of a DICOM status code.
type StatusCategory int

const (
	StatusCategorySuccess StatusCategory = iota
	StatusCategoryPending
	StatusCategoryCancel
	StatusCategoryWarning
	StatusCategoryFailure
	StatusCategoryUnknown
)

// String returns the name of the status category.
func (sc StatusCategory) String() string {
	switch sc {
	case StatusCategorySuccess:
		return "Success"
	case StatusCategoryPending:
		return "Pending"
	case StatusCategoryCancel:
		return "Cancel"
	case StatusCategoryWarning:
		return "Warning"
	case StatusCategoryFailure:
		return "Failure"
	default:
		return "Unknown"
	}
}

// CategorizeStatus returns the category of a DICOM status code.
func CategorizeStatus(status uint16) StatusCategory {
	switch {
	case status == 0x0000:
		return StatusCategorySuccess
	case status == 0xFE00:
		return StatusCategoryCancel
	case status == 0xFF00 || status == 0xFF01:
		return StatusCategoryPending
	case status&0xF000 == 0xB000:
		return StatusCategoryWarning
	case status == 0x0001:
		return StatusCategoryWarning
	default:
		return StatusCategoryFailure
	}
}

// IsSuccess returns true if the status indicates success.
func IsSuccess(status uint16) bool {
	return CategorizeStatus(status) == StatusCategorySuccess
}

// IsFailure returns true if the status indicates failure.
func IsFailure(status uint16) bool {
	return CategorizeStatus(status) == StatusCategoryFailure
}

// IsWarning returns true if the status indicates a warning.
func IsWarning(status uint16) bool {
	return CategorizeStatus(status) == StatusCategoryWarning
}

// IsCancel returns true if the status indicates cancellation.
func IsCancel(status uint16) bool {
	return CategorizeStatus(status) == StatusCategoryCancel
}
