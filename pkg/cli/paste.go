package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/google/uuid"
	"github.com/phinze/bankshot/pkg/protocol"
	"github.com/spf13/cobra"
)

// errNoImage is a clipboard that holds no image, which callers treat as a
// plain "no" rather than a failure.
var errNoImage = errors.New("no image on the clipboard")

func newPasteCmd() *cobra.Command {
	var check bool
	cmd := &cobra.Command{
		Use:   "paste",
		Short: "Save the local clipboard's image here and print its path",
		Long: `Fetches the image on the local machine's clipboard (a screenshot, say),
saves it as a PNG in bankshot's inbox on this host, and prints the path.
Only images are fetched, never text. Exits 1 when there's no image.`,
		Args:          cobra.NoArgs,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if check {
				ok, err := peekClipboard()
				if err != nil {
					fmt.Fprintf(os.Stderr, "bankshot paste: %s\n", err)
				}
				if !ok {
					os.Exit(1)
				}
				return nil
			}

			path, err := pasteFile()
			if err != nil {
				fmt.Fprintf(os.Stderr, "bankshot paste: %s\n", err)
				os.Exit(1)
			}
			fmt.Println(path)
			return nil
		},
	}
	cmd.Flags().BoolVar(&check, "check", false, "Only report, by exit status, whether there's an image (not a file)")
	return cmd
}

// newWlPasteCmd is bankshot running as wl-paste, for tools that read the
// clipboard that way. Claude Code's image paste on Linux lists the
// clipboard's types with -l and then saves --type image/png, so those are
// the two calls answered; asking for text, or anything else, gets a refusal.
func newWlPasteCmd() *cobra.Command {
	return &cobra.Command{
		Use:                "wl-paste",
		Short:              "Serve the local clipboard's image as wl-paste would",
		Hidden:             true,
		DisableFlagParsing: true,
		SilenceErrors:      true,
		RunE: func(cmd *cobra.Command, args []string) error {
			list, mime := parseWlPasteArgs(args)
			switch {
			case list:
				ok, err := peekClipboard()
				if err != nil || !ok {
					os.Exit(1)
				}
				fmt.Println("image/png")
			case mime == "image/png" || mime == "image":
				data, err := fetchClipboard()
				if err != nil {
					os.Exit(1)
				}
				if _, err := os.Stdout.Write(data); err != nil {
					os.Exit(1)
				}
			default:
				fmt.Fprintln(os.Stderr, "wl-paste (bankshot): only image/png from the local clipboard is available")
				os.Exit(1)
			}
			return nil
		},
	}
}

// parseWlPasteArgs picks out what a wl-paste call asks for: the list of
// types, or the contents as one type. Other flags are ignored.
func parseWlPasteArgs(args []string) (list bool, mime string) {
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case a == "-l" || a == "--list-types":
			list = true
		case (a == "-t" || a == "--type") && i+1 < len(args):
			i++
			mime = args[i]
		case strings.HasPrefix(a, "--type="):
			mime = strings.TrimPrefix(a, "--type=")
		}
	}
	return list, mime
}

func clipboardRequest(peek bool) (*protocol.ClipboardResponse, error) {
	payload, err := json.Marshal(protocol.ClipboardRequest{Peek: peek})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}
	resp, err := sendRequest(&protocol.Request{
		ID:      uuid.New().String(),
		Type:    protocol.CommandClipboard,
		Payload: payload,
	})
	if err != nil {
		return nil, err
	}
	if !resp.Success {
		return nil, errors.New(resp.Error)
	}
	var clipResp protocol.ClipboardResponse
	if err := json.Unmarshal(resp.Data, &clipResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}
	return &clipResp, nil
}

func peekClipboard() (bool, error) {
	clipResp, err := clipboardRequest(true)
	if err != nil {
		return false, err
	}
	return clipResp.Image, nil
}

func fetchClipboard() ([]byte, error) {
	clipResp, err := clipboardRequest(false)
	if err != nil {
		return nil, err
	}
	if !clipResp.Image {
		return nil, errNoImage
	}
	return clipResp.Data, nil
}

// pasteFile has the monitor fetch the clipboard's image or file into this
// host's inbox, and returns its path.
func pasteFile() (string, error) {
	resp, err := sendRequest(&protocol.Request{
		ID:   uuid.New().String(),
		Type: protocol.CommandClipboardFile,
	})
	if err != nil {
		return "", err
	}
	if !resp.Success {
		return "", errors.New(resp.Error)
	}
	var file protocol.ClipboardFileResponse
	if err := json.Unmarshal(resp.Data, &file); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}
	if file.Path == "" {
		return "", errors.New("no image or video on the clipboard")
	}
	return file.Path, nil
}
