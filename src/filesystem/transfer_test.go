package filesystem

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func newTestChunk(containerName, fileName, filePath, id, data string, total uint64) FileChunk {
	return FileChunk{
		ID:            id,
		FileName:      fileName,
		FilePath:      filePath,
		Data:          data,
		ContainerName: containerName,
		Total:         total,
	}
}

func TestWriteFullTransfer(t *testing.T) {
	dir := t.TempDir()
	fSys := New()

	container := "test-app"
	fileName := "app.tgz"
	id := "transfer-1"
	payload := []byte("hello world")
	hexData := hex.EncodeToString(payload)
	total := uint64(len(payload))

	err := fSys.Write(newTestChunk(container, fileName, dir, id, "BEGIN", total))
	if err != nil {
		t.Fatalf("BEGIN failed: %v", err)
	}

	transfer := fSys.GetActiveTransfer(container)
	if transfer == nil {
		t.Fatal("expected active transfer after BEGIN")
	}

	err = fSys.Write(newTestChunk(container, fileName, dir, id, hexData, total))
	if err != nil {
		t.Fatalf("DATA chunk failed: %v", err)
	}

	if transfer.Current != total {
		t.Fatalf("expected Current=%d, got %d", total, transfer.Current)
	}

	err = fSys.Write(newTestChunk(container, fileName, dir, id, "END", total))
	if err != nil {
		t.Fatalf("END failed: %v", err)
	}

	if fSys.GetActiveTransfer(container) != nil {
		t.Fatal("expected no active transfer after END")
	}

	got, err := os.ReadFile(filepath.Join(dir, fileName))
	if err != nil {
		t.Fatalf("reading result file: %v", err)
	}
	if string(got) != string(payload) {
		t.Fatalf("file content mismatch: got %q, want %q", got, payload)
	}
}

func TestWriteMultipleChunks(t *testing.T) {
	dir := t.TempDir()
	fSys := New()

	container := "multi-chunk-app"
	fileName := "app.tgz"
	id := "transfer-2"
	part1 := []byte("first-part-")
	part2 := []byte("second-part")
	total := uint64(len(part1) + len(part2))

	fSys.Write(newTestChunk(container, fileName, dir, id, "BEGIN", total))
	fSys.Write(newTestChunk(container, fileName, dir, id, hex.EncodeToString(part1), total))
	fSys.Write(newTestChunk(container, fileName, dir, id, hex.EncodeToString(part2), total))
	fSys.Write(newTestChunk(container, fileName, dir, id, "END", total))

	got, _ := os.ReadFile(filepath.Join(dir, fileName))
	want := "first-part-second-part"
	if string(got) != want {
		t.Fatalf("file content mismatch: got %q, want %q", got, want)
	}
}

func TestWriteSecondTransferReplacesFirst(t *testing.T) {
	dir := t.TempDir()
	fSys := New()

	container := "replace-app"
	fileName := "app.tgz"
	oldPayload := []byte("old-data-that-is-longer-than-new")
	newPayload := []byte("new-data")

	fSys.Write(newTestChunk(container, fileName, dir, "id-1", "BEGIN", uint64(len(oldPayload))))
	fSys.Write(newTestChunk(container, fileName, dir, "id-1", hex.EncodeToString(oldPayload), uint64(len(oldPayload))))
	fSys.Write(newTestChunk(container, fileName, dir, "id-1", "END", uint64(len(oldPayload))))

	fSys.Write(newTestChunk(container, fileName, dir, "id-2", "BEGIN", uint64(len(newPayload))))
	fSys.Write(newTestChunk(container, fileName, dir, "id-2", hex.EncodeToString(newPayload), uint64(len(newPayload))))
	fSys.Write(newTestChunk(container, fileName, dir, "id-2", "END", uint64(len(newPayload))))

	got, _ := os.ReadFile(filepath.Join(dir, fileName))
	if string(got) != string(newPayload) {
		t.Fatalf("second transfer did not fully replace first: got %q (len %d), want %q (len %d)",
			got, len(got), newPayload, len(newPayload))
	}
}

func TestWriteBeginMidTransferReplacesOld(t *testing.T) {
	dir := t.TempDir()
	fSys := New()

	container := "restart-app"
	fileName := "app.tgz"
	oldPayload := []byte("partial-old-data")
	newPayload := []byte("complete-new")

	fSys.Write(newTestChunk(container, fileName, dir, "id-1", "BEGIN", uint64(len(oldPayload))))
	fSys.Write(newTestChunk(container, fileName, dir, "id-1", hex.EncodeToString(oldPayload), uint64(len(oldPayload))))

	fSys.Write(newTestChunk(container, fileName, dir, "id-2", "BEGIN", uint64(len(newPayload))))
	fSys.Write(newTestChunk(container, fileName, dir, "id-2", hex.EncodeToString(newPayload), uint64(len(newPayload))))
	fSys.Write(newTestChunk(container, fileName, dir, "id-2", "END", uint64(len(newPayload))))

	got, _ := os.ReadFile(filepath.Join(dir, fileName))
	if string(got) != string(newPayload) {
		t.Fatalf("restarted transfer has stale data: got %q, want %q", got, newPayload)
	}
}

func TestWriteChunkWithoutBeginFails(t *testing.T) {
	dir := t.TempDir()
	fSys := New()

	err := fSys.Write(newTestChunk("no-begin", "app.tgz", dir, "id-1", hex.EncodeToString([]byte("data")), 4))
	if err == nil {
		t.Fatal("expected error when writing chunk without BEGIN")
	}
}

func TestWriteCanceledTransfer(t *testing.T) {
	dir := t.TempDir()
	fSys := New()

	container := "cancel-app"
	fileName := "app.tgz"
	id := "id-1"

	fSys.Write(newTestChunk(container, fileName, dir, id, "BEGIN", 100))
	fSys.CancelFileTransfer(container)

	err := fSys.Write(newTestChunk(container, fileName, dir, id, hex.EncodeToString([]byte("data")), 100))
	if err == nil || err.Error() != "canceled" {
		t.Fatalf("expected canceled error, got: %v", err)
	}
}

func TestWriteWrongIDIgnored(t *testing.T) {
	dir := t.TempDir()
	fSys := New()

	container := "wrong-id-app"
	fileName := "app.tgz"
	correctPayload := []byte("correct")

	fSys.Write(newTestChunk(container, fileName, dir, "id-current", "BEGIN", uint64(len(correctPayload))))

	err := fSys.Write(newTestChunk(container, fileName, dir, "id-stale", hex.EncodeToString([]byte("stale")), 100))
	if err != nil {
		t.Fatalf("expected stale chunk to be silently ignored, got: %v", err)
	}

	fSys.Write(newTestChunk(container, fileName, dir, "id-current", hex.EncodeToString(correctPayload), uint64(len(correctPayload))))
	fSys.Write(newTestChunk(container, fileName, dir, "id-current", "END", uint64(len(correctPayload))))

	got, _ := os.ReadFile(filepath.Join(dir, fileName))
	if string(got) != string(correctPayload) {
		t.Fatalf("got %q, want %q", got, correctPayload)
	}
}

func TestCleanupFailedTransferClosesFile(t *testing.T) {
	dir := t.TempDir()
	fSys := New()

	container := "cleanup-app"
	fileName := "app.tgz"

	fSys.Write(newTestChunk(container, fileName, dir, "id-1", "BEGIN", 100))

	transfer := fSys.GetActiveTransfer(container)
	if transfer == nil || transfer.File == nil {
		t.Fatal("expected active transfer with open file")
	}

	fSys.CleanupFailedTransfer(container)

	if fSys.GetActiveTransfer(container) != nil {
		t.Fatal("expected no active transfer after cleanup")
	}

	_, err := transfer.File.Write([]byte("test"))
	if err == nil {
		t.Fatal("expected error writing to closed file")
	}
}

func TestCancelFileTransferClosesFile(t *testing.T) {
	dir := t.TempDir()
	fSys := New()

	container := "cancel-close-app"
	fileName := "app.tgz"

	fSys.Write(newTestChunk(container, fileName, dir, "id-1", "BEGIN", 100))

	transfer := fSys.GetActiveTransfer(container)
	if transfer == nil || transfer.File == nil {
		t.Fatal("expected active transfer with open file")
	}

	fSys.CancelFileTransfer(container)

	if !transfer.Canceled {
		t.Fatal("expected transfer to be marked canceled")
	}

	_, err := transfer.File.Write([]byte("test"))
	if err == nil {
		t.Fatal("expected error writing to closed file")
	}
}

func TestCancelNonExistentTransfer(t *testing.T) {
	fSys := New()
	fSys.CancelFileTransfer("nonexistent")
}

func TestCleanupNonExistentTransfer(t *testing.T) {
	fSys := New()
	fSys.CleanupFailedTransfer("nonexistent")
}
