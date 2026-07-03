package image_raw

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var errBoom = errors.New("boom")

// restoreSeams resets every fault-injection seam to the real os.File method.
func restoreSeams() {
	fileTruncate = (*os.File).Truncate
	fileStat = (*os.File).Stat
	fileReadAt = (*os.File).ReadAt
	fileWriteAt = (*os.File).WriteAt
	fileRead = (*os.File).Read
	fileWrite = (*os.File).Write
}

func TestCreate_Success(t *testing.T) {
	path := filepath.Join(t.TempDir(), "disk.raw")
	const size = 4 * 1024 * 1024
	if err := Create(path, size); err != nil {
		t.Fatalf("Create: %v", err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Size() != size {
		t.Fatalf("size = %d, want %d", fi.Size(), size)
	}
	// Sparse / zero-filled: every byte reads back as zero.
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	buf := make([]byte, 4096)
	if _, err := f.ReadAt(buf, 0); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(buf, make([]byte, 4096)) {
		t.Fatal("fresh image is not zero-filled")
	}
}

func TestCreate_InvalidSize(t *testing.T) {
	if err := Create(filepath.Join(t.TempDir(), "x.raw"), 0); err == nil {
		t.Fatal("expected error for size 0")
	}
	if err := Create(filepath.Join(t.TempDir(), "x.raw"), -1); err == nil {
		t.Fatal("expected error for negative size")
	}
}

func TestCreate_OpenError(t *testing.T) {
	// Parent directory does not exist.
	bad := filepath.Join(t.TempDir(), "nope", "disk.raw")
	if err := Create(bad, 1024); err == nil {
		t.Fatal("expected error creating file in missing directory")
	}
}

func TestCreate_TruncateError(t *testing.T) {
	defer restoreSeams()
	fileTruncate = func(*os.File, int64) error { return errBoom }
	if err := Create(filepath.Join(t.TempDir(), "disk.raw"), 1024); err == nil {
		t.Fatal("expected truncate error")
	}
}

func TestIsRawImage_PlainFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "disk.raw")
	if err := Create(path, 1024*1024); err != nil {
		t.Fatal(err)
	}
	ok, err := IsRawImage(path)
	if err != nil {
		t.Fatalf("IsRawImage: %v", err)
	}
	if !ok {
		t.Fatal("plain zero-filled image should be detected as raw")
	}
}

func TestIsRawImage_SmallFile(t *testing.T) {
	// A regular non-empty file smaller than the peek window and 512-byte
	// footer window: still raw, exercises the io.EOF short-read path.
	path := filepath.Join(t.TempDir(), "tiny.raw")
	if err := os.WriteFile(path, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	ok, err := IsRawImage(path)
	if err != nil {
		t.Fatalf("IsRawImage: %v", err)
	}
	if !ok {
		t.Fatal("small non-container file should be raw")
	}
}

func TestIsRawImage_NotExist(t *testing.T) {
	ok, err := IsRawImage(filepath.Join(t.TempDir(), "missing"))
	if ok || err == nil {
		t.Fatalf("expected (false, err) for missing path, got (%v, %v)", ok, err)
	}
}

func TestIsRawImage_Directory(t *testing.T) {
	ok, err := IsRawImage(t.TempDir())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("a directory is not a raw image")
	}
}

func TestIsRawImage_Empty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.raw")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	ok, err := IsRawImage(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("an empty file is not a raw image")
	}
}

func TestIsRawImage_StatError(t *testing.T) {
	defer restoreSeams()
	path := filepath.Join(t.TempDir(), "disk.raw")
	if err := Create(path, 1024); err != nil {
		t.Fatal(err)
	}
	fileStat = func(*os.File) (os.FileInfo, error) { return nil, errBoom }
	if _, err := IsRawImage(path); err == nil {
		t.Fatal("expected stat error")
	}
}

func TestIsRawImage_HeadReadError(t *testing.T) {
	defer restoreSeams()
	path := filepath.Join(t.TempDir(), "disk.raw")
	if err := Create(path, 1024); err != nil {
		t.Fatal(err)
	}
	fileReadAt = func(*os.File, []byte, int64) (int, error) { return 0, errBoom }
	if _, err := IsRawImage(path); err == nil {
		t.Fatal("expected head read error")
	}
}

// writeContainer writes a file whose leading bytes are lead, padded to at least
// size bytes, and (when foot is non-nil) whose trailing 512 bytes start with
// foot.
func writeContainer(t *testing.T, name string, lead []byte, size int, foot []byte) string {
	t.Helper()
	buf := make([]byte, size)
	copy(buf, lead)
	if foot != nil {
		copy(buf[size-512:], foot)
	}
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, buf, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestIsRawImage_KnownContainers(t *testing.T) {
	cases := []struct {
		name string
		lead []byte
		foot []byte
	}{
		{"qcow2", qcow2Magic, nil},
		{"vmdk-sparse", []byte("KDMV"), nil},
		{"vmdk-descriptor", []byte("# Disk DescriptorFile"), nil},
		{"vdi", []byte("<<< Oracle VM VirtualBox Disk Image >>>"), nil},
		{"udif", nil, []byte("koly")},
		{"vhd", nil, []byte("conectix")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := writeContainer(t, tc.name+".img", tc.lead, 1024, tc.foot)
			ok, err := IsRawImage(path)
			if err != nil {
				t.Fatalf("IsRawImage: %v", err)
			}
			if ok {
				t.Fatalf("%s container was misdetected as raw", tc.name)
			}
		})
	}
}

func TestConvertToRaw_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.raw")
	dst := filepath.Join(dir, "dst.raw")

	// Non-trivial content spanning more than one copy chunk.
	content := bytes.Repeat([]byte("go-diskimages/raw "), (convertChunkSize/18)+7)
	if err := os.WriteFile(src, content, 0o600); err != nil {
		t.Fatal(err)
	}

	var progress bytes.Buffer
	if err := ConvertToRaw(src, dst, &progress); err != nil {
		t.Fatalf("ConvertToRaw: %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, content) {
		t.Fatal("round-tripped content differs from source")
	}
	if !strings.Contains(progress.String(), "100%") {
		t.Fatalf("expected 100%% progress, got %q", progress.String())
	}
}

func TestConvertToRaw_EmptySource(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "empty.raw")
	dst := filepath.Join(dir, "dst.raw")
	if err := os.WriteFile(src, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	var progress bytes.Buffer
	if err := ConvertToRaw(src, dst, &progress); err != nil {
		t.Fatalf("ConvertToRaw: %v", err)
	}
	fi, err := os.Stat(dst)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Size() != 0 {
		t.Fatalf("dst size = %d, want 0", fi.Size())
	}
	if progress.Len() != 0 {
		t.Fatalf("expected no progress for empty source, got %q", progress.String())
	}
}

func TestConvertToRaw_OpenSrcError(t *testing.T) {
	if err := ConvertToRaw(filepath.Join(t.TempDir(), "missing"), filepath.Join(t.TempDir(), "dst"), io.Discard); err == nil {
		t.Fatal("expected open src error")
	}
}

func TestConvertToRaw_StatError(t *testing.T) {
	defer restoreSeams()
	dir := t.TempDir()
	src := filepath.Join(dir, "src.raw")
	if err := Create(src, 1024); err != nil {
		t.Fatal(err)
	}
	fileStat = func(*os.File) (os.FileInfo, error) { return nil, errBoom }
	if err := ConvertToRaw(src, filepath.Join(dir, "dst"), io.Discard); err == nil {
		t.Fatal("expected stat error")
	}
}

func TestConvertToRaw_CreateDstError(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.raw")
	if err := Create(src, 1024); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "nope", "dst.raw") // missing parent dir
	if err := ConvertToRaw(src, dst, io.Discard); err == nil {
		t.Fatal("expected create dst error")
	}
}

func TestConvertToRaw_ReadError(t *testing.T) {
	defer restoreSeams()
	dir := t.TempDir()
	src := filepath.Join(dir, "src.raw")
	if err := Create(src, 1024); err != nil {
		t.Fatal(err)
	}
	fileRead = func(*os.File, []byte) (int, error) { return 0, errBoom }
	if err := ConvertToRaw(src, filepath.Join(dir, "dst"), io.Discard); err == nil {
		t.Fatal("expected read error")
	}
}

func TestConvertToRaw_WriteError(t *testing.T) {
	defer restoreSeams()
	dir := t.TempDir()
	src := filepath.Join(dir, "src.raw")
	if err := os.WriteFile(src, []byte("payload"), 0o600); err != nil {
		t.Fatal(err)
	}
	fileWrite = func(*os.File, []byte) (int, error) { return 0, errBoom }
	if err := ConvertToRaw(src, filepath.Join(dir, "dst"), io.Discard); err == nil {
		t.Fatal("expected write error")
	}
}
