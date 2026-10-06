package recorder

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"cyannvr/server/config"
	"cyannvr/server/models"
	"cyannvr/server/store"
)

// A segment still being written has no moov box yet. Indexing it makes playback
// open an unplayable file ("moov atom not found"), so the indexer must skip it.
func TestIndexSkipsSegmentsWithoutMoov(t *testing.T) {
	root := t.TempDir()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()
	device := "dev-1"
	if err := st.CreateDevice(models.Device{ID: device, Name: "cam", Source: models.SourceRTSP, Created: time.Now()}); err != nil {
		t.Fatalf("seed device: %v", err)
	}
	recordDir := filepath.Join(root, "recordings")
	dayDir := filepath.Join(recordDir, device, "20261006")
	if err := os.MkdirAll(dayDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// finished segment: moov present anywhere in the file
	finished := filepath.Join(dayDir, "120000.mp4")
	if err := os.WriteFile(finished, append([]byte("ftypmoov"), make([]byte, 2048)...), 0o644); err != nil {
		t.Fatal(err)
	}
	// in-progress segment: no moov, still growing
	growing := filepath.Join(dayDir, "120500.mp4")
	if err := os.WriteFile(growing, append([]byte("ftypmdat"), make([]byte, 2048)...), 0o644); err != nil {
		t.Fatal(err)
	}

	m := &Manager{cfg: &config.Config{RecordDir: recordDir}, st: st}
	m.indexDeviceDay(device, filepath.Join(recordDir, device))

	segs, err := st.SegmentsForDay(device, time.Date(2026, 10, 6, 0, 0, 0, 0, time.Local), time.Date(2026, 10, 7, 0, 0, 0, 0, time.Local))
	if err != nil {
		t.Fatal(err)
	}
	if len(segs) != 1 {
		t.Fatalf("expected only the finished segment to be indexed, got %d: %+v", len(segs), segs)
	}
	if segs[0].Path != finished {
		t.Fatalf("indexed wrong file: %s", segs[0].Path)
	}
}

// A row whose file no longer exists (e.g. a segment interrupted by an upgrade)
// must be pruned, otherwise playback resolves to a missing file.
func TestPruneMissingSegmentRows(t *testing.T) {
	root := t.TempDir()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()
	device := "dev-3"
	recordDir := filepath.Join(root, "recordings")
	dayDir := filepath.Join(recordDir, device, "20261006")
	if err := os.MkdirAll(dayDir, 0o755); err != nil {
		t.Fatal(err)
	}
	present := filepath.Join(dayDir, "140000.mp4")
	if err := os.WriteFile(present, append([]byte("ftypmoov"), make([]byte, 1024)...), 0o644); err != nil {
		t.Fatal(err)
	}
	gone := filepath.Join(dayDir, "140500.mp4")
	start := time.Date(2026, 10, 6, 14, 0, 0, 0, time.Local)
	for id, p := range map[string]string{"seg-present": present, "seg-gone": gone} {
		if err := st.UpsertSegment(models.RecordingSegment{ID: id, DeviceID: device, Start: start, End: start.Add(5 * time.Minute), Path: p}); err != nil {
			t.Fatal(err)
		}
		start = start.Add(5 * time.Minute)
	}
	m := &Manager{cfg: &config.Config{RecordDir: recordDir}, st: st}
	m.pruneMissingSegmentRows()
	segs, err := st.SegmentsForDay(device, start.Add(-2*time.Hour), start.Add(2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(segs) != 1 || segs[0].Path != present {
		t.Fatalf("only the row with an existing file may remain, got %+v", segs)
	}
}

func TestSweepRemovesStaleIndexRow(t *testing.T) {
	root := t.TempDir()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()
	device := "dev-2"
	recordDir := filepath.Join(root, "recordings")
	dayDir := filepath.Join(recordDir, device, "20261006")
	if err := os.MkdirAll(dayDir, 0o755); err != nil {
		t.Fatal(err)
	}
	broken := filepath.Join(dayDir, "130000.mp4")
	if err := os.WriteFile(broken, append([]byte("ftypmdat"), make([]byte, 512)...), 0o644); err != nil {
		t.Fatal(err)
	}
	// older than the minimum sweep age so it is treated as "no longer being written"
	old := time.Now().Add(-10 * time.Minute)
	if err := os.Chtimes(broken, old, old); err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 10, 6, 13, 0, 0, 0, time.Local)
	if err := st.UpsertSegment(models.RecordingSegment{ID: "seg-broken", DeviceID: device, Start: start, End: start.Add(5 * time.Minute), Path: broken}); err != nil {
		t.Fatal(err)
	}
	m := &Manager{cfg: &config.Config{RecordDir: recordDir}, st: st}
	m.sweepBrokenSegments()
	if _, err := os.Stat(broken); !os.IsNotExist(err) {
		t.Fatalf("broken segment file should be removed, stat err=%v", err)
	}
	segs, err := st.SegmentsForDay(device, start.Add(-time.Hour), start.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(segs) != 0 {
		t.Fatalf("stale index row must be removed, got %+v", segs)
	}
}
