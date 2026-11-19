package jpeg2000

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestTilePOCThatLeavesPacketsOut pins how this decoder reads a codestream
// whose second tile carries a progression order change in its tile-part
// header with volumes that leave packets out, where OpenJPEG 2.5.4 parts from
// it (#550).
//
// The two fixtures are opj_compress's codestreams of the same 64x48 crop of
// CT_small's pixels (12 bits), tiles of 24x20, three levels, two layers, the
// second lossless; one with "-POC T2=0,0,2,2,1,RPCL", whose volume holds
// resolutions 0 and 1 of the four and whose finer packets were never written,
// the other without it. A POC in a tile-part header applies to that tile
// alone (A.6.6, as this package cites it), so every other tile, tile 0
// among them, is coded alike in both and must decode alike: the no-POC
// codestream's samples, which are the source. Apple's ImageIO (Kakadu)
// decodes the POC codestream to this decoder's samples within its 8-bit
// output; opj_decompress decodes tile 0 up to 540 away from the source.
func TestTilePOCThatLeavesPacketsOut(t *testing.T) {
	decode := func(name string) *Image {
		t.Helper()
		data, err := os.ReadFile(filepath.Join("testdata", name))
		if err != nil {
			t.Fatal(err)
		}
		c, err := ParseCodestream(data)
		if err != nil {
			t.Fatal(err)
		}
		img, err := Decode(c)
		if err != nil {
			t.Fatal(err)
		}
		return img
	}
	withPOC := decode("opj_tile_poc_leaves_packets_out.j2k")
	without := decode("opj_tile_poc_leaves_packets_out_without.j2k")
	differ := 0
	for y := 0; y < withPOC.Height; y++ {
		for x := 0; x < withPOC.Width; x++ {
			i := y*withPOC.Width + x
			inTile1 := x >= 24 && x < 48 && y < 20
			if inTile1 {
				if withPOC.Components[0][i] != without.Components[0][i] {
					differ++
				}
				continue
			}
			if withPOC.Components[0][i] != without.Components[0][i] {
				t.Fatalf("sample (%d,%d), outside the tile with the POC, is %d; without the POC it is %d",
					x, y, withPOC.Components[0][i], without.Components[0][i])
			}
		}
	}
	// Tile 1 is the image of its two coarsest resolutions, its finer packets
	// never having been written: not the source, and pinned by its digest.
	if differ == 0 {
		t.Error("the tile with the POC decodes to the whole image, which its packets do not hold")
	}
	if got := samplesDigest(withPOC.Components); got != "3746d6e500452a52" {
		t.Errorf("the samples hash to %s", got)
	}

	if _, err := exec.LookPath("opj_decompress"); err == nil {
		data, _ := os.ReadFile(filepath.Join("testdata", "opj_tile_poc_leaves_packets_out.j2k"))
		planes := decodeWithOpenJPEG(t, t.TempDir(), data, 1)
		off := 0
		for i := 0; i < 20*withPOC.Width; i++ {
			if i%withPOC.Width < 24 && planes[0][i] != without.Components[0][i] {
				off++
			}
		}
		t.Logf("opj_decompress decodes %d of tile 0's 480 samples otherwise than the source (#550)", off)
	}
}
