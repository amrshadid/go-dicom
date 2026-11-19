package jpeg2000

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// TestHTCodestreamsMatchOpenJPEG decodes every HT codestream in the directory
// GODICOM_HTJ2K_DATA names, each a bare codestream or a JP2/JPH file, with
// this package and with opj_decompress, and requires the same samples:
// exactly for a reversible codestream, within CONFORMANCE.md §8.1's tolerance
// for an irreversible one.
//
// The codestreams are third-party files that are not committed: the two HTJ2K
// instances of pydicom-data (their first frames) and the HT codestreams of
// openjpeg-data and pylibjpeg-data. Without the variable the test is skipped;
// TestDecodeHTFixtures guards the same code with committed fixtures.
func TestHTCodestreamsMatchOpenJPEG(t *testing.T) {
	dir := os.Getenv("GODICOM_HTJ2K_DATA")
	if dir == "" {
		t.Skip("GODICOM_HTJ2K_DATA is not set")
	}
	requireOpenJPEG(t)
	var files []string
	err := filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if err == nil && !entry.IsDir() {
			switch filepath.Ext(path) {
			case ".j2k", ".j2c", ".jph", ".jhc":
				files = append(files, path)
			}
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatalf("no HT codestreams in %s", dir)
	}
	sort.Strings(files)
	for _, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		label, _ := filepath.Rel(dir, path)
		off := compareWithOpenJPEG(t, label, data)
		t.Logf("%s: agrees with OpenJPEG, %d samples within the tolerance", label, off)
	}
}

// htEncodings are the opj_compress options the HT matrix transcodes from:
// the tiles, precincts, progression orders, origins, levels, code-block
// sizes and filters of openJPEGEncodings, without the rate, the code-block
// styles and the region of interest, which are Part 1's or refused for HT.
var htEncodings = []openJPEGEncoding{
	{"LRCP", nil},
	{"RLCP", []string{"-p", "RLCP"}},
	{"RPCL precincts", []string{"-p", "RPCL", "-c", "[32,32],[16,16]"}},
	{"PCRL tiles and origin", []string{"-p", "PCRL", "-t", "48,48", "-d", "5,3", "-c", "[32,32],[16,16]"}},
	{"CPRL origin", []string{"-p", "CPRL", "-n", "4", "-d", "17,9", "-c", "[16,16]"}},
	{"precincts below code-blocks", []string{"-p", "RPCL", "-n", "5", "-b", "32,32", "-c", "[16,16],[8,8],[4,4],[2,2]"}},
	{"RPCL tiles", []string{"-p", "RPCL", "-n", "4", "-t", "40,24", "-c", "[32,32]"}},
	{"one level, small blocks", []string{"-n", "2", "-b", "8,8"}},
	{"tall blocks", []string{"-b", "16,64"}},
	{"wide odd blocks", []string{"-b", "64,4", "-n", "3"}},
	{"POC two volumes", []string{"-POC", "T1=0,0,1,3,3,CPRL/T1=3,0,1,6,3,RPCL"}},
	{"irreversible", []string{"-I"}},
	{"irreversible RPCL tiles precincts", []string{"-I", "-p", "RPCL", "-t", "48,40", "-c", "[32,32]", "-n", "4"}},
}

// htVariants are the ways each codestream is coded as HT: the cleanup pass
// alone, and a SigProp pass or both refinement passes, with and without the
// vertically causal style, all in one layer. Refinement passes in later
// layers than their cleanup pass are in TestHTRefinementInLaterLayers, since
// OpenJPEG cannot decode them.
var htVariants = []struct {
	name    string
	options htTranscodeOptions
}{
	{"cleanup", htTranscodeOptions{}},
	{"SigProp", htTranscodeOptions{refinement: htSigProp}},
	{"SigProp vertically causal", htTranscodeOptions{refinement: htSigProp, vertical: true}},
	{"SigProp+MagRef", htTranscodeOptions{refinement: htSigPropMagRef}},
	{"SigProp+MagRef vertically causal", htTranscodeOptions{refinement: htSigPropMagRef, vertical: true}},
	// HT and Part 1 tile-components side by side (T.814 8.2, HTDECLARED):
	// the first component HT, the others left as opj_compress coded them.
	{"Part 1 beside HT", htTranscodeOptions{refinement: htSigPropMagRef, partOne: map[int]bool{1: true, 2: true}}},
}

// TestHTDecodeMatchesOpenJPEG is the HT matrix: every source image of
// TestDecodeMatchesOpenJPEG, written by opj_compress under every option of
// htEncodings, transcoded to HT code-blocks in every way of htVariants, and
// decoded by this package and by OpenJPEG to the same samples.
func TestHTDecodeMatchesOpenJPEG(t *testing.T) {
	requireOpenJPEG(t)
	checked := 0
	for _, encoding := range htEncodings {
		for _, source := range openJPEGSources() {
			part1 := encodeWithOpenJPEG(t, t.TempDir(), source.img, encoding.args)
			for _, variant := range htVariants {
				label := encoding.name + ", " + source.name + ", " + variant.name
				data := htTranscode(t, part1, variant.options)
				if off := compareWithOpenJPEG(t, label, data); off > 0 {
					t.Logf("%s: %d of %d samples differ, all within the tolerance", label, off,
						source.img.Width*source.img.Height*len(source.img.Components))
				}
				checked++
			}
		}
	}
	t.Logf("OpenJPEG and this decoder agree on %d HT codestreams", checked)
}
