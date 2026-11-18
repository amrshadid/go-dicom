#!/usr/bin/env python3
"""Write the lookup table rendering fixtures and pydicom's picture of each.

No file in pydicom's corpus has a Modality LUT Sequence, a VOI LUT Sequence or
a Presentation LUT, so these are generated. Each NAME.dcm comes with
NAME_expected.raw: one byte per pixel, what pydicom's apply_modality_lut,
apply_voi_lut and apply_presentation_lut make of it, brought to 8 bits by the
output range each step declares (2^n - 1 for an n-bit table, the window's own
range for a window). Rendering the .dcm has to give the same bytes.

It needs pydicom 3 (pydicom.pixels.processing); 2.4 has no presentation LUT.
A pydicom source tree serves through PYTHONPATH. The output is the same bytes
on every run.

    PYTHONPATH=<pydicom>/src python3 scripts/render-lut-fixture.py dataset/testdata/lut
"""
import copy
import os
import sys

import numpy as np
from pydicom.dataset import Dataset, FileMetaDataset
from pydicom.sequence import Sequence
from pydicom.uid import ExplicitVRLittleEndian, generate_uid
from pydicom.pixels.processing import (
    apply_modality_lut,
    apply_presentation_lut,
    apply_voi_lut,
)

ROWS, COLS = 16, 16


def base(bits_stored, signed, photometric="MONOCHROME2"):
    ds = Dataset()
    ds.file_meta = FileMetaDataset()
    ds.file_meta.TransferSyntaxUID = ExplicitVRLittleEndian
    ds.file_meta.MediaStorageSOPClassUID = "1.2.840.10008.5.1.4.1.1.7"  # Secondary Capture
    ds.file_meta.MediaStorageSOPInstanceUID = generate_uid(entropy_srcs=[str(bits_stored), photometric])
    ds.SOPClassUID = ds.file_meta.MediaStorageSOPClassUID
    ds.SOPInstanceUID = ds.file_meta.MediaStorageSOPInstanceUID
    ds.Modality = "OT"
    ds.Rows, ds.Columns = ROWS, COLS
    ds.SamplesPerPixel = 1
    ds.PhotometricInterpretation = photometric
    ds.BitsAllocated = 16
    ds.BitsStored = bits_stored
    ds.HighBit = bits_stored - 1
    ds.PixelRepresentation = 1 if signed else 0
    return ds


def ramp(low, high):
    # Every row runs from low to high, so each table is used across its range
    # and past both ends.
    return np.tile(np.linspace(low, high, COLS).round().astype(np.int64), (ROWS, 1))


def table(entries, first, bits, data, vr_first="US"):
    item = Dataset()
    item.add_new(0x00283002, vr_first if first >= 0 else "SS", [entries, first, bits])
    item.add_new(0x00283006, "US" if bits <= 8 else "OW",
                 list(int(v) for v in data) if bits <= 8 else np.asarray(data, "<u2").tobytes())
    item.LUTExplanation = "generated"
    return item


def voi_lut(values, ds):
    """apply_voi_lut, worked around one fault of pydicom's.

    pydicom's apply_voi builds its table indices in the type of the table's
    entries, so for 8-bit entries an index past 255 wraps: a 300-entry table
    sends input 300 to entry 44. The same table read as 16-bit entries holds
    the same numbers and indexes correctly, so that is what is applied.
    """
    if ds.get("VOILUTSequence") and ds.VOILUTSequence[0].LUTDescriptor[2] == 8:
        ds = copy.deepcopy(ds)
        entries, first, _ = ds.VOILUTSequence[0].LUTDescriptor
        ds.VOILUTSequence[0].LUTDescriptor = [entries, first, 16]
    return apply_voi_lut(values, ds)


def to8(values, top):
    # Round half up, as Go's math.Round does for values that are not negative.
    return np.floor(np.clip(np.asarray(values, np.float64) / top * 255, 0, 255) + 0.5).astype(np.uint8)


def window8(values, ds):
    # pydicom's windowing returns values in the output range it derives from
    # Bits Stored or the Modality LUT; brought back to 0..255 by that range.
    if ds.get("ModalityLUTSequence"):
        low, high = 0, 2 ** ds.ModalityLUTSequence[0].LUTDescriptor[2] - 1
    elif ds.PixelRepresentation == 0:
        low, high = 0, 2 ** ds.BitsStored - 1
    else:
        low, high = -(2 ** (ds.BitsStored - 1)), 2 ** (ds.BitsStored - 1) - 1
    if "RescaleSlope" in ds and "RescaleIntercept" in ds:
        low, high = low * ds.RescaleSlope + ds.RescaleIntercept, high * ds.RescaleSlope + ds.RescaleIntercept
    return (np.asarray(values, np.float64) - low) / (high - low) * 255


def write(out, name, ds, stored, expected):
    dtype = "<i2" if ds.PixelRepresentation else "<u2"
    ds.PixelData = stored.astype(dtype).tobytes()
    ds.save_as(os.path.join(out, name + ".dcm"), enforce_file_format=True)
    with open(os.path.join(out, name + "_expected.raw"), "wb") as f:
        f.write(np.asarray(expected, np.uint8).tobytes())
    print(f"{name}: expected {expected.min()}..{expected.max()}")


def main(out):
    os.makedirs(out, exist_ok=True)

    # 1. A VOI LUT Sequence: a 12-bit image through a 4096-entry curve of 16-bit
    # entries. A window is there too; pydicom prefers the table, as PS3.3
    # C.11.2.1.2 allows either.
    ds = base(12, False)
    curve = (np.sqrt(np.arange(4096) / 4095) * 65535).round()
    ds.VOILUTSequence = Sequence([table(4096, 0, 16, curve)])
    ds.WindowCenter, ds.WindowWidth = 2048, 4096
    stored = ramp(0, 4095)
    write(out, "voi_lut_sequence", ds, stored, to8(apply_voi_lut(stored, ds), 2 ** 16 - 1))

    # 2. A VOI LUT of 8-bit entries starting at 1000: values below take the
    # first entry and values past the end take the last.
    ds = base(12, False)
    data = [int(255 * (i / 199) ** 2) for i in range(200)]
    ds.VOILUTSequence = Sequence([table(200, 1000, 8, data)])
    stored = ramp(900, 1300)
    write(out, "voi_lut_8bit_offset", ds, stored, to8(voi_lut(stored, ds), 2 ** 8 - 1))

    # 3. Signed samples and a VOI LUT whose first mapped value is negative,
    # so its LUT Descriptor is SS.
    ds = base(12, True)
    curve = (np.linspace(0, 1, 2048) ** 0.5 * 4095).round()
    ds.VOILUTSequence = Sequence([table(2048, -1024, 12, curve)])
    stored = ramp(-2048, 2047)
    write(out, "voi_lut_signed", ds, stored, to8(apply_voi_lut(stored, ds), 2 ** 12 - 1))

    # 4. A Modality LUT Sequence of 16-bit entries (PS3.3 C.11.1.1.1 allows 8
    # or 16), then a window in its units.
    ds = base(10, False)
    mlut = (np.linspace(0, 1, 1024) ** 2 * 65535).round()
    ds.ModalityLUTSequence = Sequence([table(1024, 0, 16, mlut)])
    ds.ModalityLUTSequence[0].ModalityLUTType = "US"
    ds.WindowCenter, ds.WindowWidth = 24000, 32000
    stored = ramp(0, 1023)
    write(out, "modality_lut_sequence", ds, stored,
          to8(window8(apply_voi_lut(apply_modality_lut(stored, ds), ds), ds), 255))

    # 5. A Modality LUT Sequence and a VOI LUT Sequence both.
    ds = base(10, False)
    # The Modality LUT's entries are 16 bits wide and reach 4095, the VOI
    # LUT's input range.
    ds.ModalityLUTSequence = Sequence([table(1024, 0, 16, (mlut / 16).round())])
    ds.ModalityLUTSequence[0].ModalityLUTType = "US"
    vlut = (np.linspace(0, 1, 4096) ** 0.25 * 255).round()
    ds.VOILUTSequence = Sequence([table(4096, 0, 8, vlut)])
    stored = ramp(0, 1023)
    write(out, "modality_and_voi_lut", ds, stored,
          to8(voi_lut(apply_modality_lut(stored, ds), ds), 2 ** 8 - 1))

    # 6. Presentation LUT Shape INVERSE on MONOCHROME2. The window clips both
    # ends, so the image's own lowest and highest values, which pydicom
    # inverts about, are the ends of the range.
    ds = base(12, False)
    ds.WindowCenter, ds.WindowWidth = 2048, 2000
    ds.PresentationLUTShape = "INVERSE"
    stored = ramp(0, 4095)
    voi = window8(apply_voi_lut(stored, ds), ds)
    write(out, "presentation_lut_inverse", ds, stored,
          to8(apply_presentation_lut(voi, ds), 255))

    # 7. A Presentation LUT Sequence: 256 entries of 10 bits, after a window
    # that clips both ends, for the same reason as above.
    ds = base(12, False)
    ds.WindowCenter, ds.WindowWidth = 2048, 2000
    plut = (np.linspace(0, 1, 256) ** 2.2 * 1023).round()
    item = Dataset()
    item.LUTDescriptor = [256, 0, 10]
    item.LUTData = np.asarray(plut, "<u2").tobytes()
    item["LUTData"].VR = "OW"
    ds.PresentationLUTSequence = Sequence([item])
    stored = ramp(0, 4095)
    voi = window8(apply_voi_lut(stored, ds), ds)
    write(out, "presentation_lut_sequence", ds, stored,
          to8(apply_presentation_lut(voi, ds), 2 ** 10 - 1))


if __name__ == "__main__":
    main(sys.argv[1])
