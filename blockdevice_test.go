package image_raw

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// newDev creates a raw image of the given size and opens it as a Device.
func newDev(t *testing.T, size int64) (*Device, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "disk.raw")
	if err := Create(path, size); err != nil {
		t.Fatal(err)
	}
	d, err := OpenDevice(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d, path
}

func TestOpenDevice_NotExist(t *testing.T) {
	if _, err := OpenDevice(filepath.Join(t.TempDir(), "missing.raw")); err == nil {
		t.Fatal("expected open error")
	}
}

func TestOpenDevice_StatError(t *testing.T) {
	defer restoreSeams()
	path := filepath.Join(t.TempDir(), "disk.raw")
	if err := Create(path, 1024); err != nil {
		t.Fatal(err)
	}
	fileStat = func(*os.File) (os.FileInfo, error) { return nil, errBoom }
	if _, err := OpenDevice(path); err == nil {
		t.Fatal("expected stat error")
	}
}

func TestDevice_ReadWriteRoundTrip(t *testing.T) {
	d, _ := newDev(t, 1<<20)
	want := []byte("the go-diskimages/raw authors")
	if n, err := d.WriteAt(want, 4096); err != nil || n != len(want) {
		t.Fatalf("WriteAt = (%d, %v)", n, err)
	}
	if err := d.Sync(); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	got := make([]byte, len(want))
	if n, err := d.ReadAt(got, 4096); err != nil || n != len(want) {
		t.Fatalf("ReadAt = (%d, %v)", n, err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("read %q, want %q", got, want)
	}
	if sz, err := d.Size(); err != nil || sz != 1<<20 {
		t.Fatalf("Size = (%d, %v)", sz, err)
	}
	if d.Fd() == 0 {
		t.Fatal("Fd returned 0")
	}
}

func TestDevice_ReadAt_NegativeOffset(t *testing.T) {
	d, _ := newDev(t, 512)
	if _, err := d.ReadAt(make([]byte, 4), -1); err == nil {
		t.Fatal("expected error for negative offset")
	}
}

func TestDevice_ReadAt_PastEnd(t *testing.T) {
	d, _ := newDev(t, 512)
	if _, err := d.ReadAt(make([]byte, 4), 512); err != io.EOF {
		t.Fatalf("expected io.EOF, got %v", err)
	}
}

func TestDevice_ReadAt_Clipped(t *testing.T) {
	d, _ := newDev(t, 512)
	n, err := d.ReadAt(make([]byte, 1024), 500)
	if n != 12 || err != io.EOF {
		t.Fatalf("clipped ReadAt = (%d, %v), want (12, EOF)", n, err)
	}
}

func TestDevice_ReadAt_NormalError(t *testing.T) {
	d, _ := newDev(t, 512)
	_ = d.f.Close() // force subsequent reads to fail
	if _, err := d.ReadAt(make([]byte, 4), 0); err == nil {
		t.Fatal("expected read error on closed fd")
	}
}

func TestDevice_ReadAt_ClippedError(t *testing.T) {
	d, _ := newDev(t, 512)
	_ = d.f.Close()
	if _, err := d.ReadAt(make([]byte, 1024), 500); err == nil {
		t.Fatal("expected clipped read error on closed fd")
	}
}

func TestDevice_WriteAt_OutOfRange(t *testing.T) {
	d, _ := newDev(t, 512)
	if _, err := d.WriteAt([]byte{1}, -1); err == nil {
		t.Fatal("expected error for negative offset")
	}
	if _, err := d.WriteAt([]byte{1}, 512); err == nil {
		t.Fatal("expected error for offset past end")
	}
}

func TestDevice_WriteAt_Clipped(t *testing.T) {
	d, _ := newDev(t, 16)
	n, err := d.WriteAt(bytes.Repeat([]byte{0xFF}, 20), 8)
	if n != 8 || err != io.ErrShortWrite {
		t.Fatalf("clipped WriteAt = (%d, %v), want (8, ErrShortWrite)", n, err)
	}
}

func TestDevice_WriteAt_NormalError(t *testing.T) {
	d, _ := newDev(t, 512)
	_ = d.f.Close()
	if _, err := d.WriteAt([]byte{1, 2, 3, 4}, 0); err == nil {
		t.Fatal("expected write error on closed fd")
	}
}

func TestDevice_WriteAt_ClippedError(t *testing.T) {
	d, _ := newDev(t, 16)
	_ = d.f.Close()
	if _, err := d.WriteAt(bytes.Repeat([]byte{0xFF}, 20), 8); err == nil {
		t.Fatal("expected clipped write error on closed fd")
	}
}

func TestDevice_Truncate(t *testing.T) {
	d, _ := newDev(t, 1024)
	if err := d.Truncate(4096); err != nil {
		t.Fatalf("grow: %v", err)
	}
	if sz, _ := d.Size(); sz != 4096 {
		t.Fatalf("size after grow = %d", sz)
	}
	// The grown tail is readable (zero-filled) now that virtualSize expanded.
	if _, err := d.ReadAt(make([]byte, 4), 2048); err != nil {
		t.Fatalf("read grown region: %v", err)
	}
	if err := d.Truncate(256); err != nil {
		t.Fatalf("shrink: %v", err)
	}
	if sz, _ := d.Size(); sz != 256 {
		t.Fatalf("size after shrink = %d", sz)
	}
}

func TestDevice_Truncate_Negative(t *testing.T) {
	d, _ := newDev(t, 512)
	if err := d.Truncate(-1); err == nil {
		t.Fatal("expected error for negative size")
	}
}

func TestDevice_Truncate_Error(t *testing.T) {
	d, _ := newDev(t, 512)
	_ = d.f.Close()
	if err := d.Truncate(1024); err == nil {
		t.Fatal("expected truncate error on closed fd")
	}
}

func TestDevice_Close(t *testing.T) {
	path := filepath.Join(t.TempDir(), "disk.raw")
	if err := Create(path, 512); err != nil {
		t.Fatal(err)
	}
	d, err := OpenDevice(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := d.Close(); err == nil {
		t.Fatal("expected error on double close")
	}
}
