package network

// Comprehensive SOP Class UIDs for DICOM networking.
// Covers all major storage, query/retrieve, worklist, print, and procedure step classes.
// Reference: DICOM PS3.4, pynetdicom sop_classes.py

// --- Storage SOP Classes ---

// Computed Radiography and Digital X-Ray
const (
	ComputedRadiographyImageStorageUID         = "1.2.840.10008.5.1.4.1.1.1"
	DigitalXRayImageStorageForPresentationUID  = "1.2.840.10008.5.1.4.1.1.1.1"
	DigitalXRayImageStorageForProcessingUID    = "1.2.840.10008.5.1.4.1.1.1.1.1"
	DigitalMammographyImageStoragePresentUID   = "1.2.840.10008.5.1.4.1.1.1.2"
	DigitalMammographyImageStorageProcessUID   = "1.2.840.10008.5.1.4.1.1.1.2.1"
	DigitalIntraOralXRayImageStoragePresentUID = "1.2.840.10008.5.1.4.1.1.1.3"
	DigitalIntraOralXRayImageStorageProcessUID = "1.2.840.10008.5.1.4.1.1.1.3.1"
)

// CT and MR
const (
	CTImageStorageSOP                 = "1.2.840.10008.5.1.4.1.1.2"
	EnhancedCTImageStorageSOP         = "1.2.840.10008.5.1.4.1.1.2.1"
	LegacyConvertedEnhancedCTImageUID = "1.2.840.10008.5.1.4.1.1.2.2"
	MRImageStorageSOP                 = "1.2.840.10008.5.1.4.1.1.4"
	EnhancedMRImageStorageSOP         = "1.2.840.10008.5.1.4.1.1.4.1"
	MRSpectroscopyStorageUID          = "1.2.840.10008.5.1.4.1.1.4.2"
	EnhancedMRColorImageStorageUID    = "1.2.840.10008.5.1.4.1.1.4.3"
	LegacyConvertedEnhancedMRImageUID = "1.2.840.10008.5.1.4.1.1.4.4"
)

// Ultrasound
const (
	UltrasoundMultiFrameImageStorageRetiredUID = "1.2.840.10008.5.1.4.1.1.3"
	UltrasoundMultiFrameImageStorageUID        = "1.2.840.10008.5.1.4.1.1.3.1"
	UltrasoundImageStorageRetiredUID           = "1.2.840.10008.5.1.4.1.1.6"
	UltrasoundImageStorageSOP                  = "1.2.840.10008.5.1.4.1.1.6.1"
	EnhancedUSVolumeStorageUID                 = "1.2.840.10008.5.1.4.1.1.6.2"
	PhotoacousticImageStorageUID               = "1.2.840.10008.5.1.4.1.1.6.3"
)

// Secondary Capture
const (
	SecondaryCaptureImageStorageSOP            = "1.2.840.10008.5.1.4.1.1.7"
	MultiFrameSingleBitSecondaryCaptureUID     = "1.2.840.10008.5.1.4.1.1.7.1"
	MultiFrameGrayscaleByteSecondaryCaptureUID = "1.2.840.10008.5.1.4.1.1.7.2"
	MultiFrameGrayscaleWordSecondaryCaptureUID = "1.2.840.10008.5.1.4.1.1.7.3"
	MultiFrameTrueColorSecondaryCaptureUID     = "1.2.840.10008.5.1.4.1.1.7.4"
)

// Nuclear Medicine and PET
const (
	NuclearMedicineImageStorageRetiredUID = "1.2.840.10008.5.1.4.1.1.5"
	NuclearMedicineImageStorageUID        = "1.2.840.10008.5.1.4.1.1.20"
	PositronEmissionTomographyImageUID    = "1.2.840.10008.5.1.4.1.1.128"
	EnhancedPETImageStorageUID            = "1.2.840.10008.5.1.4.1.1.130"
	LegacyConvertedEnhancedPETImageUID    = "1.2.840.10008.5.1.4.1.1.128.1"
)

// Radiation Therapy
const (
	RTImageStorageUID             = "1.2.840.10008.5.1.4.1.1.481.1"
	RTDoseStorageUID              = "1.2.840.10008.5.1.4.1.1.481.2"
	RTStructureSetStorageUID      = "1.2.840.10008.5.1.4.1.1.481.3"
	RTBeamsTreatmentRecordUID     = "1.2.840.10008.5.1.4.1.1.481.4"
	RTPlanStorageUID              = "1.2.840.10008.5.1.4.1.1.481.5"
	RTBrachyTreatmentRecordUID    = "1.2.840.10008.5.1.4.1.1.481.6"
	RTTreatmentSummaryRecordUID   = "1.2.840.10008.5.1.4.1.1.481.7"
	RTIonPlanStorageUID           = "1.2.840.10008.5.1.4.1.1.481.8"
	RTIonBeamsTreatmentRecordUID  = "1.2.840.10008.5.1.4.1.1.481.9"
	RTBeamsDeliveryInstructionUID = "1.2.840.10008.5.1.4.34.7"
)

// X-Ray Angiographic and Fluoroscopy
const (
	XRayAngiographicImageStorageSOP       = "1.2.840.10008.5.1.4.1.1.12.1"
	EnhancedXAImageStorageUID             = "1.2.840.10008.5.1.4.1.1.12.1.1"
	XRayRadiofluoroscopicImageStorageUID  = "1.2.840.10008.5.1.4.1.1.12.2"
	EnhancedXRFImageStorageUID            = "1.2.840.10008.5.1.4.1.1.12.2.1"
	XRay3DAngiographicImageStorageUID     = "1.2.840.10008.5.1.4.1.1.13.1.1"
	XRay3DCraniofacialImageStorageUID     = "1.2.840.10008.5.1.4.1.1.13.1.2"
	BreastTomosynthesisImageStorageUID    = "1.2.840.10008.5.1.4.1.1.13.1.3"
	BreastProjectionXRayImageStoragePUID  = "1.2.840.10008.5.1.4.1.1.13.1.4"
	BreastProjectionXRayImageStoragePrUID = "1.2.840.10008.5.1.4.1.1.13.1.5"
)

// Visible Light / Ophthalmology / Pathology / Microscopy
const (
	VLEndoscopicImageStorageUID              = "1.2.840.10008.5.1.4.1.1.77.1.1"
	VideoEndoscopicImageStorageUID           = "1.2.840.10008.5.1.4.1.1.77.1.1.1"
	VLMicroscopicImageStorageUID             = "1.2.840.10008.5.1.4.1.1.77.1.2"
	VideoMicroscopicImageStorageUID          = "1.2.840.10008.5.1.4.1.1.77.1.2.1"
	VLSlideCoordinatesMicroscopicUID         = "1.2.840.10008.5.1.4.1.1.77.1.3"
	VLPhotographicImageStorageUID            = "1.2.840.10008.5.1.4.1.1.77.1.4"
	VideoPhotographicImageStorageUID         = "1.2.840.10008.5.1.4.1.1.77.1.4.1"
	OphthalmicPhotography8BitUID             = "1.2.840.10008.5.1.4.1.1.77.1.5.1"
	OphthalmicPhotography16BitUID            = "1.2.840.10008.5.1.4.1.1.77.1.5.2"
	StereometricRelationshipStorageUID       = "1.2.840.10008.5.1.4.1.1.77.1.5.3"
	OphthalmicTomographyImageUID             = "1.2.840.10008.5.1.4.1.1.77.1.5.4"
	WideFieldOphthalmicStereoProjectionUID   = "1.2.840.10008.5.1.4.1.1.77.1.5.5"
	WideFieldOphthalmic3DCoordinatesUID      = "1.2.840.10008.5.1.4.1.1.77.1.5.6"
	OphthalmicOCTEnFaceImageUID              = "1.2.840.10008.5.1.4.1.1.77.1.5.7"
	OphthalmicOCTBscanVolumeAnalysisUID      = "1.2.840.10008.5.1.4.1.1.77.1.5.8"
	VLWholeSlideMicroscopyImageUID           = "1.2.840.10008.5.1.4.1.1.77.1.6"
	DermoscopicPhotographyImageStorageUID    = "1.2.840.10008.5.1.4.1.1.77.1.7"
	ConfocalMicroscopyImageStorageUID        = "1.2.840.10008.5.1.4.1.1.77.1.8"
	ConfocalMicroscopyTiledPyramidalImageUID = "1.2.840.10008.5.1.4.1.1.77.1.9"
)

// Ophthalmic Measurements
const (
	LensometryMeasurementsStorageUID       = "1.2.840.10008.5.1.4.1.1.78.1"
	AutorefractionMeasurementsStorageUID    = "1.2.840.10008.5.1.4.1.1.78.2"
	KeratometryMeasurementsStorageUID       = "1.2.840.10008.5.1.4.1.1.78.3"
	SubjectiveRefractionMeasurementsUID     = "1.2.840.10008.5.1.4.1.1.78.4"
	VisualAcuityMeasurementsStorageUID      = "1.2.840.10008.5.1.4.1.1.78.5"
	SpectaclePrescriptionReportStorageUID   = "1.2.840.10008.5.1.4.1.1.78.6"
	OphthalmicAxialMeasurementsStorageUID   = "1.2.840.10008.5.1.4.1.1.78.7"
	IntraocularLensCalculationsStorageUID   = "1.2.840.10008.5.1.4.1.1.78.8"
	MacularGridThicknessVolumeReportUID     = "1.2.840.10008.5.1.4.1.1.79.1"
	OphthalmicVisualFieldStaticPerimetryUID = "1.2.840.10008.5.1.4.1.1.80.1"
	OphthalmicThicknessMapStorageUID        = "1.2.840.10008.5.1.4.1.1.81.1"
	CornealTopographyMapStorageUID          = "1.2.840.10008.5.1.4.1.1.82.1"
)

// Intravascular OCT
const (
	IntravascularOCTImageStoragePresentUID = "1.2.840.10008.5.1.4.1.1.14.1"
	IntravascularOCTImageStorageProcessUID = "1.2.840.10008.5.1.4.1.1.14.2"
)

// Additional RT Storage
const (
	RTPhysicianIntentStorageUID                = "1.2.840.10008.5.1.4.1.1.481.10"
	RTSegmentAnnotationStorageUID              = "1.2.840.10008.5.1.4.1.1.481.11"
	RTRadiationSetStorageUID                   = "1.2.840.10008.5.1.4.1.1.481.12"
	CArmPhotonElectronRadiationStorageUID       = "1.2.840.10008.5.1.4.1.1.481.13"
	TomotherapeuticRadiationStorageUID         = "1.2.840.10008.5.1.4.1.1.481.14"
	RoboticArmRadiationStorageUID              = "1.2.840.10008.5.1.4.1.1.481.15"
	RTRadiationRecordSetStorageUID             = "1.2.840.10008.5.1.4.1.1.481.16"
	RTRadiationSalvageRecordStorageUID         = "1.2.840.10008.5.1.4.1.1.481.17"
	TomotherapeuticRadiationRecordStorageUID   = "1.2.840.10008.5.1.4.1.1.481.18"
	CArmPhotonElectronRadiationRecordUID       = "1.2.840.10008.5.1.4.1.1.481.19"
	RoboticArmRadiationRecordStorageUID        = "1.2.840.10008.5.1.4.1.1.481.20"
	RTRadiationSetDeliveryInstructionUID       = "1.2.840.10008.5.1.4.1.1.481.21"
	RTTreatmentPreparationStorageUID           = "1.2.840.10008.5.1.4.1.1.481.22"
	EnhancedRTImageStorageUID                  = "1.2.840.10008.5.1.4.1.1.481.23"
	EnhancedContinuousRTImageStorageUID        = "1.2.840.10008.5.1.4.1.1.481.24"
	RTBrachyApplicationSetupDeliveryInstUID    = "1.2.840.10008.5.1.4.34.10"
)

// Additional SR / Annotations / Content
const (
	ExtensibleSRStorageUID                        = "1.2.840.10008.5.1.4.1.1.88.35"
	PlannedImagingAgentAdministrationSRUID         = "1.2.840.10008.5.1.4.1.1.88.74"
	PerformedImagingAgentAdministrationSRUID       = "1.2.840.10008.5.1.4.1.1.88.75"
	EnhancedXRayRadiationDoseSRStorageUID          = "1.2.840.10008.5.1.4.1.1.88.76"
	WaveformAnnotationSRStorageUID                 = "1.2.840.10008.5.1.4.1.1.88.77"
	ContentAssessmentResultsStorageUID             = "1.2.840.10008.5.1.4.1.1.90.1"
	MicroscopyBulkSimpleAnnotationsStorageUID      = "1.2.840.10008.5.1.4.1.1.91.1"
)

// Additional Presentation State
const (
	GrayscalePlanarMPRVolumetricPresentUID     = "1.2.840.10008.5.1.4.1.1.11.6"
	CompositingPlanarMPRVolumetricPresentUID    = "1.2.840.10008.5.1.4.1.1.11.7"
	AdvancedBlendingPresentationStateUID        = "1.2.840.10008.5.1.4.1.1.11.8"
	VolumeRenderingVolumetricPresentUID         = "1.2.840.10008.5.1.4.1.1.11.9"
	SegmentedVolumeRenderingVolumetricPresentUID = "1.2.840.10008.5.1.4.1.1.11.10"
	MultipleVolumeRenderingVolumetricPresentUID = "1.2.840.10008.5.1.4.1.1.11.11"
	VariableModalityLUTSoftcopyPresentUID       = "1.2.840.10008.5.1.4.1.1.11.12"
	WaveformPresentationStateStorageUID         = "1.2.840.10008.5.1.4.1.1.9.100.1"
	WaveformAcquisitionPresentationStateUID     = "1.2.840.10008.5.1.4.1.1.9.100.2"
)

// Additional Waveform
const (
	General32bitECGWaveformStorageUID = "1.2.840.10008.5.1.4.1.1.9.1.4"
	BasicVoiceAudioWaveformStorageUID = "1.2.840.10008.5.1.4.1.1.9.4.1"
)

// Additional Miscellaneous Storage
const (
	BasicStructuredDisplayStorageUID          = "1.2.840.10008.5.1.4.1.1.131"
	CTPerformedProcedureProtocolStorageUID    = "1.2.840.10008.5.1.4.1.1.200.2"
	XAPerformedProcedureProtocolStorageUID    = "1.2.840.10008.5.1.4.1.1.200.8"
	CTDefinedProcedureProtocolStorageUID      = "1.2.840.10008.5.1.4.1.1.200.1"
	ProtocolApprovalStorageUID                = "1.2.840.10008.5.1.4.1.1.200.3"
	XADefinedProcedureProtocolStorageUID      = "1.2.840.10008.5.1.4.1.1.200.7"
	InventoryStorageUID                       = "1.2.840.10008.5.1.4.1.1.201.1"
	TractographyResultsStorageUID            = "1.2.840.10008.5.1.4.1.1.66.6"
	LabelMapSegmentationStorageUID            = "1.2.840.10008.5.1.4.1.1.66.7"
	MediaStorageDirectoryStorageUID           = "1.2.840.10008.1.3.10"
)

// Waveform Storage
const (
	TwelveLeadECGWaveformStorageUID     = "1.2.840.10008.5.1.4.1.1.9.1.1"
	GeneralECGWaveformStorageUID        = "1.2.840.10008.5.1.4.1.1.9.1.2"
	AmbulatoryECGWaveformStorageUID     = "1.2.840.10008.5.1.4.1.1.9.1.3"
	HemodynamicWaveformStorageUID       = "1.2.840.10008.5.1.4.1.1.9.2.1"
	BasicCardiacElectrophysiologyUID    = "1.2.840.10008.5.1.4.1.1.9.3.1"
	ArterialPulseWaveformStorageUID     = "1.2.840.10008.5.1.4.1.1.9.5.1"
	RespiratoryWaveformStorageUID       = "1.2.840.10008.5.1.4.1.1.9.6.1"
	GeneralAudioWaveformStorageUID      = "1.2.840.10008.5.1.4.1.1.9.4.2"
	MultichannelRespiratoryWaveformUID  = "1.2.840.10008.5.1.4.1.1.9.6.2"
	RoutineScalpElectroencephalogramUID = "1.2.840.10008.5.1.4.1.1.9.7.1"
	ElectromyogramWaveformStorageUID    = "1.2.840.10008.5.1.4.1.1.9.7.2"
	ElectrooculogramWaveformStorageUID  = "1.2.840.10008.5.1.4.1.1.9.7.3"
	SleepElectroencephalogramUID        = "1.2.840.10008.5.1.4.1.1.9.7.4"
	BodyPositionWaveformStorageUID      = "1.2.840.10008.5.1.4.1.1.9.8.1"
)

// Structured Reporting
const (
	BasicTextSRStorageUID                 = "1.2.840.10008.5.1.4.1.1.88.11"
	EnhancedSRStorageUID                  = "1.2.840.10008.5.1.4.1.1.88.22"
	ComprehensiveSRStorageUID             = "1.2.840.10008.5.1.4.1.1.88.33"
	Comprehensive3DSRStorageUID           = "1.2.840.10008.5.1.4.1.1.88.34"
	ProcedureLogStorageUID                = "1.2.840.10008.5.1.4.1.1.88.40"
	MammographyCADSRStorageUID            = "1.2.840.10008.5.1.4.1.1.88.50"
	KeyObjectSelectionDocumentUID         = "1.2.840.10008.5.1.4.1.1.88.59"
	ChestCADSRStorageUID                  = "1.2.840.10008.5.1.4.1.1.88.65"
	XRayRadiationDoseSRStorageUID         = "1.2.840.10008.5.1.4.1.1.88.67"
	RadiopharmaceuticalRadiationDoseSRUID = "1.2.840.10008.5.1.4.1.1.88.68"
	ColonCADSRStorageUID                  = "1.2.840.10008.5.1.4.1.1.88.69"
	ImplantationPlanSRStorageUID          = "1.2.840.10008.5.1.4.1.1.88.70"
	AcquisitionContextSRStorageUID        = "1.2.840.10008.5.1.4.1.1.88.71"
	SimplifiedAdultEchoSRStorageUID       = "1.2.840.10008.5.1.4.1.1.88.72"
	PatientRadiationDoseSRStorageUID      = "1.2.840.10008.5.1.4.1.1.88.73"
)

// Presentation State
const (
	GrayscaleSoftcopyPresentationStateUID = "1.2.840.10008.5.1.4.1.1.11.1"
	ColorSoftcopyPresentationStateUID     = "1.2.840.10008.5.1.4.1.1.11.2"
	PseudoColorSoftcopyPresentationUID    = "1.2.840.10008.5.1.4.1.1.11.3"
	BlendingSoftcopyPresentationStateUID  = "1.2.840.10008.5.1.4.1.1.11.4"
	XAXRFGrayscaleSoftcopyPresentUID      = "1.2.840.10008.5.1.4.1.1.11.5"
)

// Segmentation and Surface
const (
	SegmentationStorageUID        = "1.2.840.10008.5.1.4.1.1.66.4"
	SurfaceSegmentationStorageUID = "1.2.840.10008.5.1.4.1.1.66.5"
	SurfaceScanMeshStorageUID     = "1.2.840.10008.5.1.4.1.1.68.1"
	SurfaceScanPointCloudUID      = "1.2.840.10008.5.1.4.1.1.68.2"
)

// Parametric Map and Real World Value
const (
	ParametricMapStorageUID       = "1.2.840.10008.5.1.4.1.1.30"
	RealWorldValueMappingUID      = "1.2.840.10008.5.1.4.1.1.67"
	RawDataStorageUID             = "1.2.840.10008.5.1.4.1.1.66"
	SpatialRegistrationStorageUID = "1.2.840.10008.5.1.4.1.1.66.1"
	SpatialFiducialsStorageUID    = "1.2.840.10008.5.1.4.1.1.66.2"
	DeformableSpatialRegUID       = "1.2.840.10008.5.1.4.1.1.66.3"
)

// Encapsulated Document Storage
const (
	EncapsulatedPDFStorageUID = "1.2.840.10008.5.1.4.1.1.104.1"
	EncapsulatedCDAStorageUID = "1.2.840.10008.5.1.4.1.1.104.2"
	EncapsulatedSTLStorageUID = "1.2.840.10008.5.1.4.1.1.104.3"
	EncapsulatedOBJStorageUID = "1.2.840.10008.5.1.4.1.1.104.4"
	EncapsulatedMTLStorageUID = "1.2.840.10008.5.1.4.1.1.104.5"
)

// --- Worklist and Procedure Step ---
const (
	ModalityWorklistInformationModelFindUID = "1.2.840.10008.5.1.4.31"
	ModalityPerformedProcedureStepUID       = "1.2.840.10008.3.1.2.3.3"
	ModalityPerformedProcedureStepRetrUID   = "1.2.840.10008.3.1.2.3.4"
	ModalityPerformedProcedureStepNotifUID  = "1.2.840.10008.3.1.2.3.5"
)

// --- Print Management ---
const (
	BasicFilmSessionSOPClassUID       = "1.2.840.10008.5.1.1.1"
	BasicFilmBoxSOPClassUID           = "1.2.840.10008.5.1.1.2"
	BasicGrayscaleImageBoxSOPClassUID = "1.2.840.10008.5.1.1.4"
	BasicColorImageBoxSOPClassUID     = "1.2.840.10008.5.1.1.4.1"
	PrintJobSOPClassUID               = "1.2.840.10008.5.1.1.14"
	BasicGrayscalePrintManagementUID  = "1.2.840.10008.5.1.1.9"
	BasicColorPrintManagementUID      = "1.2.840.10008.5.1.1.18"
	PrinterSOPClassUID                = "1.2.840.10008.5.1.1.16"
	PrinterConfigurationRetrievalUID  = "1.2.840.10008.5.1.1.16.376"
)

// --- Storage Commitment ---
const (
	StorageCommitmentPushModelUID = "1.2.840.10008.1.20.1"
)

// --- Instance Availability ---
const (
	InstanceAvailabilityNotificationUID = "1.2.840.10008.5.1.4.33"
)

// --- Unified Procedure Step ---
const (
	UnifiedProcedureStepPushUID  = "1.2.840.10008.5.1.4.34.6.1"
	UnifiedProcedureStepWatchUID = "1.2.840.10008.5.1.4.34.6.2"
	UnifiedProcedureStepPullUID  = "1.2.840.10008.5.1.4.34.6.3"
	UnifiedProcedureStepEventUID = "1.2.840.10008.5.1.4.34.6.4"
	UnifiedProcedureStepQueryUID = "1.2.840.10008.5.1.4.34.6.5"
)

// --- Substance Administration ---
const (
	SubstanceAdministrationLoggingUID = "1.2.840.10008.1.42"
	ProductCharacteristicsQueryUID    = "1.2.840.10008.5.1.4.41"
	SubstanceApprovalQueryUID         = "1.2.840.10008.5.1.4.42"
)

// --- Non-Patient Object Storage ---
const (
	HangingProtocolStorageUID  = "1.2.840.10008.5.1.4.38.1"
	ColorPaletteStorageUID     = "1.2.840.10008.5.1.4.39.1"
	GenericImplantTemplateUID  = "1.2.840.10008.5.1.4.43.1"
	ImplantAssemblyTemplateUID = "1.2.840.10008.5.1.4.44.1"
	ImplantTemplateGroupUID    = "1.2.840.10008.5.1.4.45.1"
)

// AllStorageSOPClassUIDs returns all supported Storage SOP Class UIDs.
// This is the equivalent of pynetdicom's StoragePresentationContexts.
func AllStorageSOPClassUIDs() []string {
	return []string{
		// CR and Digital X-Ray
		ComputedRadiographyImageStorageUID,
		DigitalXRayImageStorageForPresentationUID,
		DigitalXRayImageStorageForProcessingUID,
		DigitalMammographyImageStoragePresentUID,
		DigitalMammographyImageStorageProcessUID,
		DigitalIntraOralXRayImageStoragePresentUID,
		DigitalIntraOralXRayImageStorageProcessUID,

		// CT and MR
		CTImageStorageSOP,
		EnhancedCTImageStorageSOP,
		LegacyConvertedEnhancedCTImageUID,
		MRImageStorageSOP,
		EnhancedMRImageStorageSOP,
		MRSpectroscopyStorageUID,
		EnhancedMRColorImageStorageUID,
		LegacyConvertedEnhancedMRImageUID,

		// Ultrasound
		UltrasoundMultiFrameImageStorageUID,
		UltrasoundImageStorageSOP,
		EnhancedUSVolumeStorageUID,

		// Secondary Capture
		SecondaryCaptureImageStorageSOP,
		MultiFrameSingleBitSecondaryCaptureUID,
		MultiFrameGrayscaleByteSecondaryCaptureUID,
		MultiFrameGrayscaleWordSecondaryCaptureUID,
		MultiFrameTrueColorSecondaryCaptureUID,

		// Nuclear Medicine and PET
		NuclearMedicineImageStorageUID,
		PositronEmissionTomographyImageUID,
		EnhancedPETImageStorageUID,
		LegacyConvertedEnhancedPETImageUID,

		// Radiation Therapy
		RTImageStorageUID,
		RTDoseStorageUID,
		RTStructureSetStorageUID,
		RTBeamsTreatmentRecordUID,
		RTPlanStorageUID,
		RTBrachyTreatmentRecordUID,
		RTTreatmentSummaryRecordUID,
		RTIonPlanStorageUID,
		RTIonBeamsTreatmentRecordUID,

		// X-Ray Angiographic and Fluoroscopy
		XRayAngiographicImageStorageSOP,
		EnhancedXAImageStorageUID,
		XRayRadiofluoroscopicImageStorageUID,
		EnhancedXRFImageStorageUID,
		XRay3DAngiographicImageStorageUID,
		XRay3DCraniofacialImageStorageUID,
		BreastTomosynthesisImageStorageUID,

		// Visible Light / Ophthalmology / Microscopy
		VLEndoscopicImageStorageUID,
		VideoEndoscopicImageStorageUID,
		VLMicroscopicImageStorageUID,
		VideoMicroscopicImageStorageUID,
		VLPhotographicImageStorageUID,
		VideoPhotographicImageStorageUID,
		OphthalmicPhotography8BitUID,
		OphthalmicPhotography16BitUID,
		StereometricRelationshipStorageUID,
		OphthalmicTomographyImageUID,
		WideFieldOphthalmicStereoProjectionUID,
		WideFieldOphthalmic3DCoordinatesUID,
		OphthalmicOCTEnFaceImageUID,
		OphthalmicOCTBscanVolumeAnalysisUID,
		VLWholeSlideMicroscopyImageUID,
		DermoscopicPhotographyImageStorageUID,
		ConfocalMicroscopyImageStorageUID,
		ConfocalMicroscopyTiledPyramidalImageUID,

		// Intravascular OCT
		IntravascularOCTImageStoragePresentUID,
		IntravascularOCTImageStorageProcessUID,

		// Ophthalmic Measurements
		LensometryMeasurementsStorageUID,
		AutorefractionMeasurementsStorageUID,
		KeratometryMeasurementsStorageUID,
		SubjectiveRefractionMeasurementsUID,
		VisualAcuityMeasurementsStorageUID,
		SpectaclePrescriptionReportStorageUID,
		OphthalmicAxialMeasurementsStorageUID,
		IntraocularLensCalculationsStorageUID,
		MacularGridThicknessVolumeReportUID,
		OphthalmicVisualFieldStaticPerimetryUID,
		OphthalmicThicknessMapStorageUID,
		CornealTopographyMapStorageUID,

		// Waveform
		TwelveLeadECGWaveformStorageUID,
		GeneralECGWaveformStorageUID,
		AmbulatoryECGWaveformStorageUID,
		General32bitECGWaveformStorageUID,
		HemodynamicWaveformStorageUID,
		BasicCardiacElectrophysiologyUID,
		BasicVoiceAudioWaveformStorageUID,
		GeneralAudioWaveformStorageUID,
		ArterialPulseWaveformStorageUID,
		RespiratoryWaveformStorageUID,
		MultichannelRespiratoryWaveformUID,
		RoutineScalpElectroencephalogramUID,
		ElectromyogramWaveformStorageUID,
		ElectrooculogramWaveformStorageUID,
		SleepElectroencephalogramUID,
		BodyPositionWaveformStorageUID,
		WaveformPresentationStateStorageUID,
		WaveformAcquisitionPresentationStateUID,

		// Structured Reporting
		BasicTextSRStorageUID,
		EnhancedSRStorageUID,
		ComprehensiveSRStorageUID,
		Comprehensive3DSRStorageUID,
		ExtensibleSRStorageUID,
		ProcedureLogStorageUID,
		MammographyCADSRStorageUID,
		KeyObjectSelectionDocumentUID,
		ChestCADSRStorageUID,
		XRayRadiationDoseSRStorageUID,
		RadiopharmaceuticalRadiationDoseSRUID,
		ColonCADSRStorageUID,
		ImplantationPlanSRStorageUID,
		AcquisitionContextSRStorageUID,
		SimplifiedAdultEchoSRStorageUID,
		PatientRadiationDoseSRStorageUID,
		PlannedImagingAgentAdministrationSRUID,
		PerformedImagingAgentAdministrationSRUID,
		EnhancedXRayRadiationDoseSRStorageUID,
		WaveformAnnotationSRStorageUID,
		ContentAssessmentResultsStorageUID,
		MicroscopyBulkSimpleAnnotationsStorageUID,

		// Presentation State
		GrayscaleSoftcopyPresentationStateUID,
		ColorSoftcopyPresentationStateUID,
		PseudoColorSoftcopyPresentationUID,
		BlendingSoftcopyPresentationStateUID,
		XAXRFGrayscaleSoftcopyPresentUID,
		GrayscalePlanarMPRVolumetricPresentUID,
		CompositingPlanarMPRVolumetricPresentUID,
		AdvancedBlendingPresentationStateUID,
		VolumeRenderingVolumetricPresentUID,
		SegmentedVolumeRenderingVolumetricPresentUID,
		MultipleVolumeRenderingVolumetricPresentUID,
		VariableModalityLUTSoftcopyPresentUID,

		// Segmentation
		SegmentationStorageUID,
		SurfaceSegmentationStorageUID,
		SurfaceScanMeshStorageUID,
		TractographyResultsStorageUID,
		LabelMapSegmentationStorageUID,

		// Parametric and Spatial
		ParametricMapStorageUID,
		RealWorldValueMappingUID,
		RawDataStorageUID,
		SpatialRegistrationStorageUID,
		SpatialFiducialsStorageUID,
		DeformableSpatialRegUID,

		// Encapsulated Documents
		EncapsulatedPDFStorageUID,
		EncapsulatedCDAStorageUID,
		EncapsulatedSTLStorageUID,
		EncapsulatedOBJStorageUID,
		EncapsulatedMTLStorageUID,

		// Additional RT
		RTPhysicianIntentStorageUID,
		RTSegmentAnnotationStorageUID,
		RTRadiationSetStorageUID,
		CArmPhotonElectronRadiationStorageUID,
		TomotherapeuticRadiationStorageUID,
		RoboticArmRadiationStorageUID,
		RTRadiationRecordSetStorageUID,
		RTRadiationSalvageRecordStorageUID,
		TomotherapeuticRadiationRecordStorageUID,
		CArmPhotonElectronRadiationRecordUID,
		RoboticArmRadiationRecordStorageUID,
		RTRadiationSetDeliveryInstructionUID,
		RTTreatmentPreparationStorageUID,
		EnhancedRTImageStorageUID,
		EnhancedContinuousRTImageStorageUID,
		RTBrachyApplicationSetupDeliveryInstUID,

		// Miscellaneous
		BasicStructuredDisplayStorageUID,
		CTPerformedProcedureProtocolStorageUID,
		XAPerformedProcedureProtocolStorageUID,
	}
}

// AllQueryRetrieveSOPClassUIDs returns all Query/Retrieve SOP Class UIDs.
func AllQueryRetrieveSOPClassUIDs() []string {
	return []string{
		PatientRootQueryRetrieveFind,
		PatientRootQueryRetrieveMove,
		PatientRootQueryRetrieveGet,
		StudyRootQueryRetrieveFind,
		StudyRootQueryRetrieveMove,
		StudyRootQueryRetrieveGet,
	}
}

// AllWorklistSOPClassUIDs returns Worklist-related SOP Class UIDs.
func AllWorklistSOPClassUIDs() []string {
	return []string{
		ModalityWorklistInformationModelFindUID,
	}
}

// AllTransferSyntaxUIDs returns all supported Transfer Syntax UIDs (45 total).
// Matches pynetdicom's ALL_TRANSFER_SYNTAXES.
func AllTransferSyntaxUIDs() []string {
	return []string{
		// Uncompressed
		ImplicitVRLittleEndianUID,
		ExplicitVRLittleEndianUID,
		DeflatedExplicitVRLittleEndianUID,
		ExplicitVRBigEndianUID,

		// JPEG
		JPEGBaselineUID,
		JPEGExtendedUID,
		JPEGLosslessSV1UID,
		JPEGLosslessUID,

		// JPEG-LS
		JPEGLSLosslessUID,
		JPEGLSNearLosslessUID,

		// JPEG 2000
		JPEG2000LosslessUID,
		JPEG2000UID,
		JPEG2000Part2MultiComponentLosslessUID,
		JPEG2000Part2MultiComponentUID,

		// JPIP
		JPIPReferencedUID,
		JPIPReferencedDeflateUID,

		// MPEG2
		MPEG2MainProfileUID,
		MPEG2MainProfileFragmentUID,
		MPEG2MainProfileHighUID,
		MPEG2MainProfileHighFragmentUID,

		// MPEG-4 AVC/H.264
		MPEG4AVCH264HighProfileUID,
		MPEG4AVCH264HighProfileFragmentUID,
		MPEG4AVCH264BDCompatibleUID,
		MPEG4AVCH264BDCompatibleFragmentUID,
		MPEG4AVCH264HighProfile2DUID,
		MPEG4AVCH264HighProfile2DFragmentUID,
		MPEG4AVCH264HighProfile3DUID,
		MPEG4AVCH264HighProfile3DFragmentUID,
		MPEG4AVCH264StereoHighProfileUID,
		MPEG4AVCH264StereoHighFragmentUID,

		// HEVC/H.265
		HEVCH265MainProfileUID,
		HEVCH265Main10ProfileUID,

		// JPEG XL
		JPEGXLLosslessUID,
		JPEGXLJPEGRecompressionUID,
		JPEGXLUID,

		// High-Throughput JPEG 2000
		HTJ2KLosslessUID,
		HTJ2KLosslessRPCLUID,
		HTJ2KUID,
		JPIPHTJ2KReferencedUID,
		JPIPHTJ2KReferencedDeflateUID,

		// RLE
		RLELosslessUID,

		// SMPTE ST 2110
		SMPTEST2110UncompressedProgressiveUID,
		SMPTEST2110UncompressedInterlacedUID,
		SMPTEST2110PCMDigitalAudioUID,
	}
}

// UncompressedTransferSyntaxUIDs returns only uncompressed transfer syntaxes.
func UncompressedTransferSyntaxUIDs() []string {
	return []string{
		ImplicitVRLittleEndianUID,
		ExplicitVRLittleEndianUID,
		ExplicitVRBigEndianUID,
	}
}

// IsStorageSOPClass returns true if the given UID is a Storage SOP Class.
func IsStorageSOPClass(uid string) bool {
	for _, u := range AllStorageSOPClassUIDs() {
		if u == uid {
			return true
		}
	}
	return false
}

// IsQueryRetrieveSOPClass returns true if the given UID is a Query/Retrieve SOP Class.
func IsQueryRetrieveSOPClass(uid string) bool {
	for _, u := range AllQueryRetrieveSOPClassUIDs() {
		if u == uid {
			return true
		}
	}
	return false
}
