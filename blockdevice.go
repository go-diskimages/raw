package image_raw

import (
	"fmt"
	"io"
	"os"
	"sync"
)

// Device provides read-write block device access to a raw disk image.
//
// A raw image maps one-to-one onto the underlying file: virtual offset N is
// file offset N. The virtual size is fixed when the device is opened; use
// Truncate to change it (both grow and shrink are supported, unlike the sparse
// container formats whose virtual size is immutable).
type Device struct {
	f           *os.File
	virtualSize int64
	mu          sync.RWMutex
}

// OpenDevice opens the raw image at path as a read-write block device. The
// virtual size is taken from the current file length.
func OpenDevice(path string) (*Device, error) {
	f, err := os.OpenFile(path, os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("raw: open %s: %w", path, err)
	}
	fi, err := fileStat(f)
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("raw: stat %s: %w", path, err)
	}
	return &Device{f: f, virtualSize: fi.Size()}, nil
}

// ReadAt implements io.ReaderAt for the virtual disk address space. Reads past
// the end of the virtual size return io.EOF; a read that starts in-bounds but
// extends past the end is clipped and returns io.EOF.
func (d *Device) ReadAt(p []byte, off int64) (int, error) {
	if off < 0 {
		return 0, fmt.Errorf("raw: read at negative offset %d", off)
	}
	d.mu.RLock()
	defer d.mu.RUnlock()
	if off >= d.virtualSize {
		return 0, io.EOF
	}
	if int64(len(p)) > d.virtualSize-off {
		n, err := fileReadAt(d.f, p[:d.virtualSize-off], off)
		if err != nil {
			return n, err
		}
		return n, io.EOF
	}
	return fileReadAt(d.f, p, off)
}

// WriteAt implements io.WriterAt for the virtual disk address space. Writes
// must start within the virtual size; a write that would extend past the end is
// clipped to the last byte and returns io.ErrShortWrite.
func (d *Device) WriteAt(p []byte, off int64) (int, error) {
	if off < 0 || off >= d.virtualSize {
		return 0, fmt.Errorf("raw: write at %d outside virtual size %d", off, d.virtualSize)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if int64(len(p)) > d.virtualSize-off {
		n, err := fileWriteAt(d.f, p[:d.virtualSize-off], off)
		if err != nil {
			return n, err
		}
		return n, io.ErrShortWrite
	}
	return fileWriteAt(d.f, p, off)
}

// Size returns the virtual size of the raw image.
func (d *Device) Size() (int64, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.virtualSize, nil
}

// Truncate changes the virtual size of the device to size bytes. Growing
// zero-fills the new tail; shrinking discards the tail.
func (d *Device) Truncate(size int64) error {
	if size < 0 {
		return fmt.Errorf("raw: truncate: negative size %d", size)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := fileTruncate(d.f, size); err != nil {
		return fmt.Errorf("raw: truncate: %w", err)
	}
	d.virtualSize = size
	return nil
}

// Sync flushes all writes to the underlying file.
func (d *Device) Sync() error { return d.f.Sync() }

// Fd returns the underlying file descriptor. For a raw image this fd addresses
// the virtual disk directly (offset N of the fd is guest offset N).
func (d *Device) Fd() uintptr { return d.f.Fd() }

// Close closes the underlying file.
func (d *Device) Close() error { return d.f.Close() }
