package image_raw

import (
	"fmt"
	"io"
	"os"
)

// Format is the raw disk image format.
// A Format value satisfies the diskimage_format.Format interface defined in
// github.com/go-diskimages/interface without requiring an import of that
// module (Go structural typing).
type Format struct{}

// Name returns "raw".
func (Format) Name() string { return "raw" }

// Create creates a new blank raw disk image. Delegates to the package-level
// Create.
func (Format) Create(path string, sizeBytes int64) error {
	return Create(path, sizeBytes)
}

// Detect returns (true, nil) if path holds a raw disk image, (false, nil) if
// the file exists but is a recognised container (or is otherwise not a raw
// image), and (false, err) if the path cannot be examined.
func (Format) Detect(path string) (bool, error) {
	return IsRawImage(path)
}

// ToRaw copies the raw image at src to dst. Progress messages are written to w.
// Since a raw image already IS its own raw form, this is the identity copy.
func (Format) ToRaw(src, dst string, w io.Writer) error {
	return ConvertToRaw(src, dst, w)
}

// Resize changes the virtual size of the raw image at path to newSizeBytes.
// Both growing and shrinking are supported: a raw image has no internal
// structure, so truncation up (grow, zero-filled tail) or down (shrink,
// discarding the tail) is always well defined.
func (Format) Resize(path string, newSizeBytes int64) error {
	if newSizeBytes <= 0 {
		return fmt.Errorf("raw: Resize: size must be positive, got %d", newSizeBytes)
	}
	f, err := os.OpenFile(path, os.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("raw: Resize: open %s: %w", path, err)
	}
	defer f.Close()
	if err := fileTruncate(f, newSizeBytes); err != nil {
		return fmt.Errorf("raw: Resize: %s: %w", path, err)
	}
	return nil
}
