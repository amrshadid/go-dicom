#!/usr/bin/env python3
"""Print the expectations table for the JPEG 2000 decoder's gate test.

The samples themselves are not committed: five megabytes of arrays in a Go
module is five megabytes every caller downloads. What is committed is a digest
of each one, which is as exact and fits on a line, and for a lossy file a mean,
which is all an inexact decode can be held to. This mirrors what
dataset/pydicompixels_table_test.go already does for the corpus at large.

    python3 scripts/jpeg2000-reference.py <pydicom test_files dir>
"""
import hashlib
import os
import sys

import pydicom

NAMES = [
    "MR_small_jp2klossless.dcm", "693_J2KI.dcm", "J2K_pixelrep_mismatch.dcm",
    "SC_rgb_gdcm_KY.dcm", "GDCMJ2K_TextGBR.dcm", "JPEG2000.dcm",
]

print("var pydicomJPEG2000 = []pydicomFrame{")
for name in NAMES:
    path = os.path.join(sys.argv[1], name)
    ds = pydicom.dcmread(path)
    arr = ds.pixel_array
    if arr.ndim == 4:          # multi-frame colour: the first frame only
        arr = arr[0]
    elif arr.ndim == 3 and int(getattr(ds, "SamplesPerPixel", 1)) == 1:
        arr = arr[0]           # multi-frame greyscale
    components = int(getattr(ds, "SamplesPerPixel", 1))
    flat = arr.reshape(-1).astype("int32")

    representation = int(getattr(ds, "PixelRepresentation", 0))
    digest = hashlib.sha256(flat.tobytes()).hexdigest()[:16]
    print(f'\t{{"{name}", {components}, {int(ds.BitsStored)}, {representation}, '
          f'{len(flat)}, "{digest}", {flat.mean():.6f}}},')
print("}")
