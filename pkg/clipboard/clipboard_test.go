package clipboard

import (
	"bytes"
	"encoding/hex"
	"strings"
	"testing"
)

func TestParseAppleScriptData(t *testing.T) {
	png := append(append([]byte{}, pngMagic...), "rest of the image"...)
	out := "«data PNGf" + strings.ToUpper(hex.EncodeToString(png)) + "»\n"

	got, err := parseAppleScriptData([]byte(out))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, png) {
		t.Errorf("got %q, want %q", got, png)
	}

	for _, bad := range []string{
		"",
		"some text",
		"«data TIFF4D4D»",
		"«data PNGfZZ»",
		"«data PNGf" + hex.EncodeToString([]byte("not a png")) + "»",
	} {
		if _, err := parseAppleScriptData([]byte(bad)); err == nil {
			t.Errorf("parsed %q, want an error", bad)
		}
	}
}
