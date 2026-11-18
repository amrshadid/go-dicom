#!/usr/bin/env python3
"""Write the small CT image the runnable examples in examples/commonjobs read.

It is made up, not a scan: an 8 by 8 ramp of signed 16-bit samples with the
attributes a CT carries (a rescale to Hounsfield units, a window, a patient,
a study, one coded item in a sequence), so that every example prints
something worth reading and nothing in it belongs to anyone. Written with
pydicom so that the examples read a file this library did not write.

    python3 scripts/commonjobs-fixture.py examples/commonjobs/testdata/ct.dcm
"""
import sys

import numpy as np
from pydicom.dataset import Dataset, FileMetaDataset
from pydicom.sequence import Sequence
from pydicom.uid import CTImageStorage, ExplicitVRLittleEndian

ds = Dataset()
ds.file_meta = FileMetaDataset()
ds.file_meta.TransferSyntaxUID = ExplicitVRLittleEndian
ds.file_meta.MediaStorageSOPClassUID = CTImageStorage
ds.file_meta.MediaStorageSOPInstanceUID = "1.2.826.0.1.3680043.10.511.1.1.3"
ds.SOPClassUID = CTImageStorage
ds.SOPInstanceUID = ds.file_meta.MediaStorageSOPInstanceUID
ds.StudyInstanceUID = "1.2.826.0.1.3680043.10.511.1.1.1"
ds.SeriesInstanceUID = "1.2.826.0.1.3680043.10.511.1.1.2"
ds.PatientName = "Doe^Jane"
ds.PatientID = "PID-0001"
ds.PatientBirthDate = "19800101"
ds.StudyDate = "20260101"
ds.Modality = "CT"
ds.InstitutionName = "Example Hospital"
code = Dataset()
code.CodeValue = "CTHEAD"
code.CodingSchemeDesignator = "99EXAMPLE"
code.CodeMeaning = "CT of the head"
ds.ProcedureCodeSequence = Sequence([code])
ds.Rows, ds.Columns = 8, 8
ds.SamplesPerPixel = 1
ds.PhotometricInterpretation = "MONOCHROME2"
ds.BitsAllocated, ds.BitsStored, ds.HighBit = 16, 16, 15
ds.PixelRepresentation = 1
ds.RescaleIntercept, ds.RescaleSlope = -1024, 1
ds.WindowCenter, ds.WindowWidth = 40, 400
# From 1024 - 400 to 1024 + 400 stored, so -400 to 400 HU: below, across and
# above the window.
ds.PixelData = (np.linspace(624, 1424, 64).round().astype("<i2")).tobytes()
ds.save_as(sys.argv[1], write_like_original=False)
