package image_raw

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestFormatName(t *testing.T) {
	if got := (Format{}).Name(); got != "raw" {
		t.Fatalf("Name() = %q, want %q", got, "raw")
	}
}

func TestFormatCreate_Delegates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "disk.raw")
	if err := (Format{}).Create(path, 2048); err != nil {
		t.Fatalf("Create: %v", err)
	}
	ok, err := (Format{}).Detect(path)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if !ok {
		t.Fatal("created raw image not detected")
	}
}

func TestFormatCreate_InvalidSize(t *testing.T) {
	if err := (Format{}).Create(filepath.Join(t.TempDir(), "x.raw"), -1); err == nil {
		t.Fatal("expected error for negative size")
	}
}

func TestFormatDetect_NotRaw(t *testing.T) {
	path := writeContainer(t, "disk.qcow2", qcow2Magic, 1024, nil)
	ok, err := (Format{}).Detect(path)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if ok {
		t.Fatal("qcow2 file should not detect as raw")
	}
}

func TestFormatDetect_Error(t *testing.T) {
	ok, err := (Format{}).Detect(filepath.Join(t.TempDir(), "missing"))
	if ok || err == nil {
		t.Fatalf("expected (false, err), got (%v, %v)", ok, err)
	}
}

func TestFormatToRaw_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.raw")
	dst := filepath.Join(dir, "dst.raw")
	content := bytes.Repeat([]byte{0xAB}, 3000)
	if err := os.WriteFile(src, content, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := (Format{}).ToRaw(src, dst, io.Discard); err != nil {
		t.Fatalf("ToRaw: %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, content) {
		t.Fatal("ToRaw content mismatch")
	}
}

func TestFormatToRaw_BadSrc(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "dst.raw")
	if err := (Format{}).ToRaw(filepath.Join(t.TempDir(), "missing"), dst, io.Discard); err == nil {
		t.Fatal("expected error for missing src")
	}
}

func TestFormatResize_InvalidSize(t *testing.T) {
	path := filepath.Join(t.TempDir(), "disk.raw")
	if err := (Format{}).Resize(path, 0); err == nil {
		t.Fatal("expected error for size 0")
	}
}

func TestFormatResize_BadPath(t *testing.T) {
	if err := (Format{}).Resize(filepath.Join(t.TempDir(), "missing"), 512); err == nil {
		t.Fatal("expected error for missing path")
	}
}

func TestFormatResize_GrowAndShrink(t *testing.T) {
	path := filepath.Join(t.TempDir(), "disk.raw")
	if err := (Format{}).Create(path, 1*1024*1024); err != nil {
		t.Fatal(err)
	}
	if err := (Format{}).Resize(path, 2*1024*1024); err != nil {
		t.Fatalf("grow: %v", err)
	}
	if fi, _ := os.Stat(path); fi.Size() != 2*1024*1024 {
		t.Fatalf("after grow size = %d", fi.Size())
	}
	if err := (Format{}).Resize(path, 512*1024); err != nil {
		t.Fatalf("shrink: %v", err)
	}
	if fi, _ := os.Stat(path); fi.Size() != 512*1024 {
		t.Fatalf("after shrink size = %d", fi.Size())
	}
}

func TestFormatResize_TruncateError(t *testing.T) {
	defer restoreSeams()
	path := filepath.Join(t.TempDir(), "disk.raw")
	if err := Create(path, 1024); err != nil {
		t.Fatal(err)
	}
	fileTruncate = func(*os.File, int64) error { return errBoom }
	if err := (Format{}).Resize(path, 2048); err == nil {
		t.Fatal("expected truncate error")
	}
}
