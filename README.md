<p align="center"><img src="https://raw.githubusercontent.com/go-diskimages/brand/main/social/go-diskimages-raw.png" alt="go-diskimages/raw" width="720"></p>

# raw

[![Go Reference](https://pkg.go.dev/badge/github.com/go-diskimages/raw.svg)](https://pkg.go.dev/github.com/go-diskimages/raw)
[![License: BSD-3-Clause](https://img.shields.io/badge/License-BSD--3--Clause-blue.svg)](LICENSE)
[![CI](https://github.com/go-diskimages/raw/actions/workflows/ci.yml/badge.svg)](https://github.com/go-diskimages/raw/actions/workflows/ci.yml)

Pure-Go creator, detector, converter, and read-write block device for the
**raw** (unstructured) disk image format. No CGO, no external tools. Works on
all platforms and all supported Go architectures.

## Module

```
github.com/go-diskimages/raw
```

The package identifier is `image_raw`.

## Raw format

A raw image is a plain byte-for-byte image of a block device: the file's
contents **are** the disk contents. There is no header, no metadata, no
allocation table, and no compression. Raw is therefore the trivial *identity*
format of the go-diskimages family:

- **Create** writes a sparse, zero-filled file of the requested size. On
  filesystems that support holes, no data blocks are allocated.
- **ToRaw** is a straight byte copy — a raw image already *is* its own raw form.
- **Detect** is the negative / fallback case: a regular, non-empty file that
  carries none of the known structured-container signatures (QCOW2, VMDK, VDI,
  VHD, or Apple UDIF/DMG) is a raw image.
- **Resize** truncates the file up or down. Because there is no internal
  structure to corrupt, both grow (zero-filled tail) and shrink (discarded
  tail) are safe and supported.

## Public API

### Package-level functions

```go
// Create writes a new empty raw disk image with the given size in bytes.
// The file is sparse and reads back as zeros.
func Create(path string, sizeBytes int64) error

// IsRawImage reports whether path holds a raw disk image. It returns
// (false, nil) for a directory, an empty file, or a recognised container,
// and (false, err) when the path cannot be examined.
func IsRawImage(path string) (bool, error)

// ConvertToRaw copies the raw image at src to dst, emitting "N%\n" progress
// lines to w. This is the identity copy.
func ConvertToRaw(src, dst string, w io.Writer) error
```

### Format value

`Format` satisfies the `github.com/go-diskimages/interface` format interface by
structural typing (no import of that module required):

```go
type Format struct{}

func (Format) Name() string                                 // "raw"
func (Format) Create(path string, sizeBytes int64) error    // new sparse raw image
func (Format) Detect(path string) (bool, error)             // negative/fallback detection
func (Format) ToRaw(src, dst string, w io.Writer) error     // identity copy
func (Format) Resize(path string, newSizeBytes int64) error // grow or shrink
```

### Read-write block device

`Device` exposes a raw image as a random-access block device. Virtual offset N
maps directly onto file offset N. The virtual size is fixed at open time; use
`Truncate` to change it (both grow and shrink are supported).

```go
func OpenDevice(path string) (*Device, error)

func (d *Device) ReadAt(p []byte, off int64) (int, error)
func (d *Device) WriteAt(p []byte, off int64) (int, error)
func (d *Device) Size() (int64, error)
func (d *Device) Truncate(size int64) error
func (d *Device) Sync() error
func (d *Device) Fd() uintptr
func (d *Device) Close() error
```

## Examples

### Create, inspect, convert

```go
import image_raw "github.com/go-diskimages/raw"

// Create a 4 GiB sparse raw image.
err := image_raw.Create("disk.raw", 4<<30)

// Detect.
ok, err := image_raw.IsRawImage("disk.raw") // true, nil

// Copy to another raw image (progress printed to stdout).
err = image_raw.ConvertToRaw("disk.raw", "copy.raw", os.Stdout)
```

### Random-access reads and writes

```go
import image_raw "github.com/go-diskimages/raw"

dev, err := image_raw.OpenDevice("disk.raw")
if err != nil {
    return err
}
defer dev.Close()

if _, err := dev.WriteAt([]byte("hello"), 1<<20); err != nil {
    return err
}
if err := dev.Sync(); err != nil {
    return err
}

buf := make([]byte, 5)
if _, err := dev.ReadAt(buf, 1<<20); err != nil {
    return err
}
```

## License

BSD-3-Clause. Copyright the go-diskimages/raw authors.
