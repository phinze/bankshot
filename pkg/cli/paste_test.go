package cli

import "testing"

func TestParseWlPasteArgs(t *testing.T) {
	for _, tc := range []struct {
		args []string
		list bool
		mime string
	}{
		// What Claude Code runs on Linux.
		{[]string{"-l"}, true, ""},
		{[]string{"--type", "image/png"}, false, "image/png"},
		{[]string{"--type", "image/bmp"}, false, "image/bmp"},
		{[]string{"--no-newline"}, false, ""},
		// Other spellings.
		{[]string{"--list-types"}, true, ""},
		{[]string{"-t", "image/png"}, false, "image/png"},
		{[]string{"--type=image/png"}, false, "image/png"},
		{[]string{"-t"}, false, ""},
	} {
		list, mime := parseWlPasteArgs(tc.args)
		if list != tc.list || mime != tc.mime {
			t.Errorf("parseWlPasteArgs(%q) = %v, %q; want %v, %q", tc.args, list, mime, tc.list, tc.mime)
		}
	}
}
