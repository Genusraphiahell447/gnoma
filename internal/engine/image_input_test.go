package engine

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"somegit.dev/Owlibou/gnoma/internal/message"
)

// pngOnePixel is the minimum valid 1x1 PNG. Used so http.DetectContentType
// returns "image/png" and the parser accepts the file.
var pngOnePixel = []byte{
	0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A,
	0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52,
	0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
	0x08, 0x02, 0x00, 0x00, 0x00, 0x90, 0x77, 0x53,
	0xDE, 0x00, 0x00, 0x00, 0x0C, 0x49, 0x44, 0x41,
	0x54, 0x08, 0x99, 0x63, 0xF8, 0xCF, 0xC0, 0x00,
	0x00, 0x00, 0x03, 0x00, 0x01, 0x5B, 0x3E, 0xBA,
	0xD6, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4E,
	0x44, 0xAE, 0x42, 0x60, 0x82,
}

func writeTempPNG(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "test.png")
	if err := os.WriteFile(p, pngOnePixel, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestParseImageMarkers_NoMarkers(t *testing.T) {
	got, warns := parseImageMarkers("just plain text")
	if len(got) != 1 || got[0].Type != message.ContentText || got[0].Text != "just plain text" {
		t.Errorf("got %+v, want single text block", got)
	}
	if len(warns) != 0 {
		t.Errorf("unexpected warnings: %v", warns)
	}
}

func TestParseImageMarkers_SingleImage(t *testing.T) {
	path := writeTempPNG(t)
	got, warns := parseImageMarkers("[Image: " + path + "] what is this?")
	if len(warns) != 0 {
		t.Fatalf("unexpected warnings: %v", warns)
	}
	if len(got) != 2 {
		t.Fatalf("got %d blocks, want 2", len(got))
	}
	if got[0].Type != message.ContentImage {
		t.Errorf("block 0 type = %v, want ContentImage", got[0].Type)
	}
	if got[0].Image == nil || !bytes.Equal(got[0].Image.Data, pngOnePixel) {
		t.Error("image bytes not captured into Content.Image.Data")
	}
	if got[0].Image.MediaType != "image/png" {
		t.Errorf("MediaType = %q, want image/png", got[0].Image.MediaType)
	}
	if got[1].Type != message.ContentText || got[1].Text != " what is this?" {
		t.Errorf("block 1 = %+v, want trailing text", got[1])
	}
}

func TestParseImageMarkers_MissingFileWarnsAndFallsBackToText(t *testing.T) {
	got, warns := parseImageMarkers("see [Image: /nonexistent/path.png] please")
	if len(warns) != 1 {
		t.Fatalf("got %d warnings, want 1", len(warns))
	}
	if !strings.Contains(warns[0], "/nonexistent/path.png") {
		t.Errorf("warning %q should mention path", warns[0])
	}
	// Marker stays as literal text so subprocess CLIs that auto-ingest paths still work.
	var joined string
	for _, c := range got {
		if c.Type == message.ContentText {
			joined += c.Text
		}
		if c.Type == message.ContentImage {
			t.Error("missing file should not produce image content")
		}
	}
	if !strings.Contains(joined, "[Image: /nonexistent/path.png]") {
		t.Errorf("joined text %q should keep literal marker", joined)
	}
}

func TestParseImageMarkers_RelativePathRejected(t *testing.T) {
	_, warns := parseImageMarkers("[Image: relative/path.png]")
	if len(warns) != 1 {
		t.Fatalf("got %d warnings, want 1", len(warns))
	}
	if !strings.Contains(warns[0], "absolute") {
		t.Errorf("warning %q should explain absolute-path requirement", warns[0])
	}
}

func TestParseImageMarkers_OversizedRejected(t *testing.T) {
	p := filepath.Join(t.TempDir(), "big.png")
	// Write a >10MiB file (header still says PNG so media type detect passes).
	big := make([]byte, imageMaxBytes+1)
	copy(big, pngOnePixel)
	if err := os.WriteFile(p, big, 0o600); err != nil {
		t.Fatal(err)
	}
	_, warns := parseImageMarkers("[Image: " + p + "]")
	if len(warns) != 1 {
		t.Fatalf("got %d warnings, want 1", len(warns))
	}
	if !strings.Contains(warns[0], "exceeds") {
		t.Errorf("warning %q should explain size limit", warns[0])
	}
}

func TestParseImageMarkers_NonImageFileRejected(t *testing.T) {
	p := filepath.Join(t.TempDir(), "not_an_image.txt")
	if err := os.WriteFile(p, []byte("plain text, not an image"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, warns := parseImageMarkers("[Image: " + p + "]")
	if len(warns) != 1 {
		t.Fatalf("got %d warnings, want 1", len(warns))
	}
	if !strings.Contains(warns[0], "unsupported media type") {
		t.Errorf("warning %q should mention media type", warns[0])
	}
}

func TestParseImageMarkers_MultipleImagesAndText(t *testing.T) {
	p1 := writeTempPNG(t)
	p2 := writeTempPNG(t)
	input := "before [Image: " + p1 + "] between [Image: " + p2 + "] after"
	got, warns := parseImageMarkers(input)
	if len(warns) != 0 {
		t.Fatalf("unexpected warnings: %v", warns)
	}
	// Expected order: text, image, text, image, text
	wantTypes := []message.ContentType{
		message.ContentText, message.ContentImage,
		message.ContentText, message.ContentImage,
		message.ContentText,
	}
	if len(got) != len(wantTypes) {
		t.Fatalf("got %d blocks, want %d", len(got), len(wantTypes))
	}
	for i, want := range wantTypes {
		if got[i].Type != want {
			t.Errorf("block %d type = %v, want %v", i, got[i].Type, want)
		}
	}
}
