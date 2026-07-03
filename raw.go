// Package image_raw implements the raw (unstructured) disk image format for the
// go-diskimages family.
//
// A raw image is a plain byte-for-byte image of a block device: the file's
// contents ARE the disk contents, with no header, no metadata, no allocation
// tables, and no compression. It is therefore the trivial identity format of
// the family:
//
//   - Create writes a sparse, zero-filled file of the requested size.
//   - ToRaw is a straight copy (a raw image already IS its own raw form).
//   - Detect is the negative/fallback case: a regular, non-empty file that
//     carries none of the known structured-container signatures is raw.
//   - Resize truncates the file up or down (both grow and shrink are safe and
//     supported, since a raw image has no internal structure to corrupt).
//
// A Format value satisfies the diskimage_format.Format interface defined in
// github.com/go-diskimages/interface without requiring an import of that module
// (Go structural typing).
package image_raw

import (
	"bytes"
	"fmt"
	"io"
	"os"
)

// convertChunkSize is the copy buffer size used by ConvertToRaw.
const convertChunkSize = 1 << 20 // 1 MiB

// magicPeekLen is the number of leading bytes read for container detection.
// It is large enough to hold the longest leading signature we test for.
const magicPeekLen = 32

// qcow2Magic is the leading signature of a QEMU QCOW2 image ("QFI\xfb").
var qcow2Magic = []byte{0x51, 0x46, 0x49, 0xfb}

// Test seams: indirections over os.File operations whose error branches are
// otherwise unreachable in unit tests. They default to the real methods and are
// overridden (then restored) via fault injection in the *_test.go files.
var (
	fileTruncate = (*os.File).Truncate
	fileStat     = (*os.File).Stat
	fileReadAt   = (*os.File).ReadAt
	fileWriteAt  = (*os.File).WriteAt
	fileRead     = (*os.File).Read
	fileWrite    = (*os.File).Write
)

// Create writes a new empty raw disk image at path with the given size in
// bytes. The file is created sparse (a hole of sizeBytes) where the underlying
// filesystem supports it: no data blocks are allocated and every byte reads
// back as zero.
func Create(path string, sizeBytes int64) error {
	if sizeBytes <= 0 {
		return fmt.Errorf("raw: size must be positive, got %d", sizeBytes)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("raw: create %s: %w", path, err)
	}
	defer f.Close()
	if err := fileTruncate(f, sizeBytes); err != nil {
		return fmt.Errorf("raw: truncate %s: %w", path, err)
	}
	return nil
}

// IsRawImage reports whether path holds a raw disk image.
//
// A raw image is the identity/fallback format of the family, so it is defined
// negatively: a regular, non-empty file that begins (or ends) with none of the
// known structured-container signatures — QCOW2, VMDK, VDI, VHD, or Apple UDIF
// (DMG). It returns (false, nil) when the file exists but is not a raw image
// (a directory, an empty file, or a recognised container), and (false, err)
// when the path cannot be examined.
func IsRawImage(path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, fmt.Errorf("raw: detect %s: %w", path, err)
	}
	defer f.Close()
	fi, err := fileStat(f)
	if err != nil {
		return false, fmt.Errorf("raw: detect %s: %w", path, err)
	}
	if !fi.Mode().IsRegular() {
		return false, nil
	}
	size := fi.Size()
	if size == 0 {
		return false, nil
	}
	head := make([]byte, magicPeekLen)
	n, err := fileReadAt(f, head, 0)
	if err != nil && err != io.EOF {
		return false, fmt.Errorf("raw: detect %s: %w", path, err)
	}
	if matchesKnownContainer(f, head[:n], size) {
		return false, nil
	}
	return true, nil
}

// matchesKnownContainer reports whether the given file looks like one of the
// structured container formats the family knows about. head holds the leading
// bytes of the file; size is the file length in bytes.
func matchesKnownContainer(f *os.File, head []byte, size int64) bool {
	switch {
	case bytes.HasPrefix(head, qcow2Magic): // QEMU QCOW2
		return true
	case bytes.HasPrefix(head, []byte("KDMV")): // VMDK sparse extent
		return true
	case bytes.HasPrefix(head, []byte("# Disk Desc")): // VMDK descriptor
		return true
	case bytes.HasPrefix(head, []byte("<<< ")): // Oracle VDI
		return true
	}
	// UDIF (DMG) and VHD keep their signature in a 512-byte trailer.
	if size >= 512 {
		foot := make([]byte, 512)
		if _, err := fileReadAt(f, foot, size-512); err == nil {
			if bytes.HasPrefix(foot, []byte("koly")) { // Apple UDIF / DMG
				return true
			}
			if bytes.HasPrefix(foot, []byte("conectix")) { // Microsoft VHD
				return true
			}
		}
	}
	return false
}

// ConvertToRaw copies the raw disk image at src to dst, emitting "N%\n" progress
// lines to w. Because a raw image already IS its own raw form, this is a plain
// byte-for-byte copy (the identity conversion).
func ConvertToRaw(src, dst string, w io.Writer) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("raw: open %s: %w", src, err)
	}
	defer in.Close()
	fi, err := fileStat(in)
	if err != nil {
		return fmt.Errorf("raw: stat %s: %w", src, err)
	}
	total := fi.Size()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("raw: create %s: %w", dst, err)
	}
	defer out.Close()

	buf := make([]byte, convertChunkSize)
	var copied int64
	lastPct := -1
	for {
		rn, rerr := fileRead(in, buf)
		if rn > 0 {
			if _, werr := fileWrite(out, buf[:rn]); werr != nil {
				return fmt.Errorf("raw: write %s: %w", dst, werr)
			}
			copied += int64(rn)
			if total > 0 {
				if pct := int(copied * 100 / total); pct > lastPct {
					lastPct = pct
					_, _ = fmt.Fprintf(w, "%d%%\n", pct)
				}
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return fmt.Errorf("raw: read %s: %w", src, rerr)
		}
	}
	return nil
}
