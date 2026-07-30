package dataset_test

// pydicomPixelExpectations is generated from pydicom's decode of every file
// in its own corpus. See TestPixelsAgainstWholePydicomCorpus.
type pydicomPixels struct {
	file   string
	syntax string
	n      int
	digest string  // sha256 of the samples, little endian; empty when lossy
	mean   float64 // compared within a tolerance when the syntax is lossy
}

var pydicomPixelExpectations = []pydicomPixels{
	{"693_J2KI.dcm", "1.2.840.10008.1.2.4.91", 262144, "", -8.322845},
	{"CT_small.dcm", "1.2.840.10008.1.2.1", 16384, "7a481f6ffff833ae", 904.926147},
	{"ExplVR_BigEnd.dcm", "1.2.840.10008.1.2.2", 14400, "1583c4339dd36e91", 171.577500},
	{"GDCMJ2K_TextGBR.dcm", "1.2.840.10008.1.2.4.90", 480000, "bea5673fdd49313f", 124.435881},
	{"J2K_pixelrep_mismatch.dcm", "1.2.840.10008.1.2.4.90", 262144, "1296350a0006ef69", -658.436806},
	{"JPEG2000.dcm", "1.2.840.10008.1.2.4.91", 262144, "", 13.458160},
	{"JPGExtended.dcm", "1.2.840.10008.1.2.4.51", 262144, "", 14.383038},
	{"MR_small.dcm", "1.2.840.10008.1.2.1", 4096, "88617aaa46138fb1", 518.881348},
	{"MR_small_RLE.dcm", "1.2.840.10008.1.2.5", 4096, "88617aaa46138fb1", 518.881348},
	{"MR_small_bigendian.dcm", "1.2.840.10008.1.2.2", 4096, "88617aaa46138fb1", 518.881348},
	{"MR_small_expb.dcm", "1.2.840.10008.1.2.2", 4096, "88617aaa46138fb1", 518.881348},
	{"MR_small_implicit.dcm", "1.2.840.10008.1.2", 4096, "88617aaa46138fb1", 518.881348},
	{"MR_small_jp2klossless.dcm", "1.2.840.10008.1.2.4.90", 4096, "88617aaa46138fb1", 518.881348},
	{"MR_small_jpeg_ls_lossless.dcm", "1.2.840.10008.1.2.4.80", 4096, "88617aaa46138fb1", 518.881348},
	{"MR_small_padded.dcm", "1.2.840.10008.1.2.1", 4096, "88617aaa46138fb1", 518.881348},
	{"SC_jpeg_no_color_transform.dcm", "1.2.840.10008.1.2.4.50", 196608, "", 210.352346},
	{"SC_jpeg_no_color_transform_2.dcm", "1.2.840.10008.1.2.4.50", 196608, "", 245.635997},
	{"SC_rgb_dcmtk_+eb+cr.dcm", "1.2.840.10008.1.2.4.50", 30000, "", 127.740000},
	{"SC_rgb_dcmtk_+eb+cy+n1.dcm", "1.2.840.10008.1.2.4.50", 30000, "", 127.803333},
	{"SC_rgb_dcmtk_+eb+cy+n2.dcm", "1.2.840.10008.1.2.4.50", 30000, "", 127.880000},
	{"SC_rgb_dcmtk_+eb+cy+np.dcm", "1.2.840.10008.1.2.4.50", 30000, "", 127.803333},
	{"SC_rgb_dcmtk_+eb+cy+s2.dcm", "1.2.840.10008.1.2.4.50", 30000, "", 127.880000},
	{"SC_rgb_dcmtk_+eb+cy+s4.dcm", "1.2.840.10008.1.2.4.50", 30000, "", 127.880000},
	{"SC_rgb_gdcm_KY.dcm", "1.2.840.10008.1.2.4.91", 30000, "", 127.700000},
	{"SC_rgb_jpeg.dcm", "1.2.840.10008.1.2.4.50", 196608, "", 210.352346},
	{"SC_rgb_jpeg_app14_dcmd.dcm", "1.2.840.10008.1.2.4.50", 196608, "", 245.635997},
	{"SC_rgb_jpeg_dcmd.dcm", "1.2.840.10008.1.2", 196608, "be7aa556b206ac44", 243.971593},
	{"SC_rgb_jpeg_dcmtk.dcm", "1.2.840.10008.1.2.4.50", 30000, "", 127.880000},
	{"SC_rgb_jpeg_gdcm.dcm", "1.2.840.10008.1.2.4.70", 30000, "169e619557b12114", 127.700000},
	{"SC_rgb_jpeg_lossy_gdcm.dcm", "1.2.840.10008.1.2.4.50", 30000, "", 127.525000},
	{"SC_rgb_rle.dcm", "1.2.840.10008.1.2.5", 30000, "169e619557b12114", 127.700000},
	{"SC_rgb_rle_16bit.dcm", "1.2.840.10008.1.2.5", 30000, "36de0258708d3af7", 32818.900000},
	{"SC_rgb_rle_16bit_2frame.dcm", "1.2.840.10008.1.2.5", 60000, "d7e2338dd240b58c", 32767.500000},
	{"SC_rgb_rle_2frame.dcm", "1.2.840.10008.1.2.5", 60000, "026dac3bc332e46b", 127.500000},
	{"SC_rgb_rle_32bit.dcm", "1.2.840.10008.1.2.5", 30000, "1a243c9351e3a9ae", 2150852249.300000},
	{"SC_rgb_rle_32bit_2frame.dcm", "1.2.840.10008.1.2.5", 60000, "3caa80cc3032f745", 2147483647.500000},
	{"SC_rgb_small_odd.dcm", "1.2.840.10008.1.2.1", 27, "ef2df252ba3cd066", 128.777778},
	{"SC_rgb_small_odd_big_endian.dcm", "1.2.840.10008.1.2.2", 27, "ef2df252ba3cd066", 128.777778},
	{"SC_rgb_small_odd_jpeg.dcm", "1.2.840.10008.1.2.4.50", 27, "", 128.000000},
	{"SC_ybr_full_422_uncompressed.dcm", "1.2.840.10008.1.2.1", 30000, "ddddadc3c3d361b5", 127.880000},
	{"image_dfl.dcm", "1.2.840.10008.1.2.1.99", 262144, "1f5f1b1c1a57606a", 127.115967},
	{"liver_1frame.dcm", "1.2.840.10008.1.2.1", 262144, "e036a07b502fdfd1", 0.138218},
	{"liver_expb_1frame.dcm", "1.2.840.10008.1.2.2", 262144, "e036a07b502fdfd1", 0.138218},
	{"rtdose.dcm", "1.2.840.10008.1.2", 1500, "e30a4288ac229022", 1013273.333333},
	{"rtdose_1frame.dcm", "1.2.840.10008.1.2", 100, "67f96b3373d7acf1", 1013780.000000},
	{"rtdose_expb.dcm", "1.2.840.10008.1.2.2", 1500, "e30a4288ac229022", 1013273.333333},
	{"rtdose_expb_1frame.dcm", "1.2.840.10008.1.2.2", 100, "67f96b3373d7acf1", 1013780.000000},
	{"rtdose_rle.dcm", "1.2.840.10008.1.2.5", 1500, "e30a4288ac229022", 1013273.333333},
	{"rtdose_rle_1frame.dcm", "1.2.840.10008.1.2.5", 100, "67f96b3373d7acf1", 1013780.000000},
}
