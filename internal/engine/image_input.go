package engine

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"somegit.dev/Owlibou/gnoma/internal/message"
)

// imageMarkerRe matches the `[Image: /absolute/path/to/file.ext]` form that
// the TUI emits when expanding pasted image placeholders.
var imageMarkerRe = regexp.MustCompile(`\[Image:\s*([^\]]+?)\]`)

// imageMaxBytes caps how big an inline image is allowed to be. Larger files
// are skipped (the marker stays as plain text). 10 MiB roughly matches what
// vision providers accept inline; bigger payloads almost always indicate a
// misclick (e.g. a screen recording) rather than an actual screenshot.
const imageMaxBytes = 10 << 20

// parseImageMarkers splits a user input string into a sequence of content
// blocks. Each `[Image: /path]` marker is replaced by an ImageContent block
// carrying the file bytes; the surrounding text is preserved as ContentText
// blocks. If a marker references a file that can't be read or whose bytes
// exceed imageMaxBytes, the marker is left as literal text and a warning
// is appended to warnings — the turn still proceeds.
//
// When no markers are present, the result is a single text block matching
// the legacy NewUserText behavior.
func parseImageMarkers(input string) (content []message.Content, warnings []string) {
	indices := imageMarkerRe.FindAllStringSubmatchIndex(input, -1)
	if len(indices) == 0 {
		return []message.Content{message.NewTextContent(input)}, nil
	}

	var blocks []message.Content
	cursor := 0
	for _, idx := range indices {
		matchStart, matchEnd := idx[0], idx[1]
		pathStart, pathEnd := idx[2], idx[3]
		path := strings.TrimSpace(input[pathStart:pathEnd])

		// Emit any preceding text as a text block.
		if matchStart > cursor {
			if pre := input[cursor:matchStart]; pre != "" {
				blocks = append(blocks, message.NewTextContent(pre))
			}
		}

		img, warn := loadImage(path)
		if warn != "" {
			warnings = append(warnings, warn)
			// Fall back to literal text so the model still sees the reference.
			blocks = append(blocks, message.NewTextContent(input[matchStart:matchEnd]))
		} else {
			blocks = append(blocks, message.NewImageContent(img))
		}
		cursor = matchEnd
	}
	if cursor < len(input) {
		if tail := input[cursor:]; tail != "" {
			blocks = append(blocks, message.NewTextContent(tail))
		}
	}
	if len(blocks) == 0 {
		blocks = []message.Content{message.NewTextContent("")}
	}
	return blocks, warnings
}

func loadImage(path string) (message.Image, string) {
	if path == "" {
		return message.Image{}, "image marker had empty path"
	}
	if !filepath.IsAbs(path) {
		return message.Image{}, fmt.Sprintf("image path %q must be absolute; skipping", path)
	}
	info, err := os.Stat(path)
	if err != nil {
		return message.Image{}, fmt.Sprintf("image %q: %v", path, err)
	}
	if info.IsDir() {
		return message.Image{}, fmt.Sprintf("image %q is a directory", path)
	}
	if info.Size() > imageMaxBytes {
		return message.Image{}, fmt.Sprintf("image %q is %d bytes, exceeds %d limit", path, info.Size(), imageMaxBytes)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return message.Image{}, fmt.Sprintf("image %q read failed: %v", path, err)
	}
	mediaType := http.DetectContentType(data)
	if !strings.HasPrefix(mediaType, "image/") {
		return message.Image{}, fmt.Sprintf("image %q has unsupported media type %q", path, mediaType)
	}
	return message.Image{Data: data, MediaType: mediaType, Path: path}, ""
}
