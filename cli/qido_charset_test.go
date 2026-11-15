package cli

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A DICOMweb answer is JSON, which is UTF-8. qido printed its text through
// the same Latin-1 reading show used, so a Greek name in a search result
// came out as accented Latin letters (#270).
func TestQidoPrintsNamesThatAreNotLatin(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/dicom+json")
		_, _ = io.WriteString(w, `[{"00080005":{"vr":"CS","Value":["ISO_IR 192"]},`+
			`"00100010":{"vr":"PN","Value":[{"Alphabetic":"Διονυσιος"}]},`+
			`"00081030":{"vr":"LO","Value":["Μαγνητική"]}}]`)
	}))
	defer server.Close()

	var out bytes.Buffer
	if err := (&QidoCommand{out: &out}).Execute([]string{server.URL}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Διονυσιος", "Μαγνητική"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("the output does not show %q:\n%s", want, out.String())
		}
	}
}
