package main

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	pb "github.com/brotherlogic/cdprocessor/proto"
	pbgd "github.com/brotherlogic/godiscogs/proto"
	pbrc "github.com/brotherlogic/recordcollection/proto"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type encodeCall struct {
	kind     string
	wavPath  string
	outPath  string
}

type mockAudioEncoder struct {
	mu          sync.Mutex
	calls       []encodeCall
	flacErr     error
	mp3Err      error
	emptyOutput bool
	onFlacStart func(wavPath string)
}

func (m *mockAudioEncoder) EncodeFlac(ctx context.Context, wavPath, flacPath string) error {
	if m.onFlacStart != nil {
		m.onFlacStart(wavPath)
	}
	m.mu.Lock()
	m.calls = append(m.calls, encodeCall{kind: "flac", wavPath: wavPath, outPath: flacPath})
	m.mu.Unlock()
	if m.flacErr != nil {
		return m.flacErr
	}
	data := []byte("fLaC-encoded-data")
	if m.emptyOutput {
		data = nil
	}
	return os.WriteFile(flacPath, data, 0644)
}

func (m *mockAudioEncoder) EncodeMP3(ctx context.Context, wavPath, mp3Path string) error {
	m.mu.Lock()
	m.calls = append(m.calls, encodeCall{kind: "mp3", wavPath: wavPath, outPath: mp3Path})
	m.mu.Unlock()
	if m.mp3Err != nil {
		return m.mp3Err
	}
	data := []byte("ID3-mp3-encoded-data")
	if m.emptyOutput {
		data = nil
	}
	return os.WriteFile(mp3Path, data, 0644)
}

func makeFakeWav(payload string) []byte {
	var buf bytes.Buffer
	buf.WriteString("RIFF")
	buf.Write([]byte{0x24, 0x00, 0x00, 0x00})
	buf.WriteString("WAVE")
	buf.WriteString(payload)
	return buf.Bytes()
}

func buildTestZip(t *testing.T, entries map[string][]byte, dirEntries ...string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, d := range dirEntries {
		hdr := &zip.FileHeader{Name: d, Method: zip.Deflate}
		hdr.SetMode(os.ModeDir | 0755)
		if _, err := zw.CreateHeader(hdr); err != nil {
			t.Fatalf("create zip dir entry %s: %v", d, err)
		}
	}
	for name, content := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("create zip entry %s: %v", name, err)
		}
		if _, err := w.Write(content); err != nil {
			t.Fatalf("write zip entry %s: %v", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip writer: %v", err)
	}
	return buf.Bytes()
}

func makeReleaseRecord(releaseID int32, instanceID int64, trackCount int, formatName string) *pbrc.Record {
	tracks := make([]*pbgd.Track, trackCount)
	for i := 0; i < trackCount; i++ {
		tracks[i] = &pbgd.Track{
			Position:  fmt.Sprintf("%d", i+1),
			Title:     fmt.Sprintf("Track %d", i+1),
			TrackType: pbgd.Track_TRACK,
		}
	}
	return &pbrc.Record{
		Release: &pbgd.Release{
			Id:         releaseID,
			InstanceId: instanceID,
			Title:      "Test Digital Release",
			Formats:    []*pbgd.Format{{Name: formatName, Qty: "1"}},
			Tracklist:  tracks,
		},
		Metadata: &pbrc.ReleaseMetadata{
			GoalFolder: 1782105,
			FiledUnder: pbrc.ReleaseMetadata_FILE_DIGITAL,
		},
	}
}

func newDownloadTestServer(t *testing.T, enc audioEncoder) (*Server, string) {
	t.Helper()
	dir := t.TempDir() + "/"
	s := InitTestServer(dir)
	if enc != nil {
		s.downloader.encoder = enc
	}
	t.Cleanup(func() {
		_ = s.Shutdown(context.Background())
	})
	return s, dir
}

func waitForTerminalStatus(t *testing.T, s *Server, releaseID int32) *pb.GetDownloadStatusResponse {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := s.GetDownloadStatus(context.Background(), &pb.GetDownloadStatusRequest{ReleaseId: releaseID})
		if err != nil {
			t.Fatalf("GetDownloadStatus(%d) error: %v", releaseID, err)
		}
		if resp.GetState() != pb.DownloadState_DOWNLOAD_QUEUED && resp.GetState() != pb.DownloadState_DOWNLOAD_IN_PROGRESS {
			return resp
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for download %d to reach terminal state", releaseID)
	return nil
}

func TestDownloadReleaseZipEndToEnd(t *testing.T) {
	enc := &mockAudioEncoder{}
	s, dir := newDownloadTestServer(t, enc)
	tg := &testGetter{
		records: map[int64]*pbrc.Record{
			5001: makeReleaseRecord(1001, 5001, 3, "File"),
		},
	}
	s.getter = tg

	// Pre-seed old quarantine directory to ensure atomic commit cleans it up.
	oldQuarantine := filepath.Join(dir, ".quarantine", "1001")
	if err := os.MkdirAll(oldQuarantine, 0755); err != nil {
		t.Fatalf("mkdir quarantine: %v", err)
	}
	if err := os.WriteFile(filepath.Join(oldQuarantine, "stale.txt"), []byte("stale"), 0644); err != nil {
		t.Fatalf("write stale quarantine file: %v", err)
	}

	zipBytes := buildTestZip(t, map[string][]byte{
		"Artist - Album - 03 Third.wav":  makeFakeWav("track3"),
		"Artist - Album - 01 First.wav":  makeFakeWav("track1"),
		"Artist - Album - 02 Second.wav": makeFakeWav("track2"),
	})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(zipBytes)
	}))
	defer srv.Close()

	resp, err := s.DownloadRelease(context.Background(), &pb.DownloadReleaseRequest{
		ReleaseId:   1001,
		InstanceId:  5001,
		DownloadUrl: srv.URL + "/album.zip",
	})
	if err != nil {
		t.Fatalf("DownloadRelease failed: %v", err)
	}
	if resp.GetState() != pb.DownloadState_DOWNLOAD_QUEUED {
		t.Fatalf("expected DOWNLOAD_QUEUED, got %v", resp.GetState())
	}

	st := waitForTerminalStatus(t, s, 1001)
	if st.GetState() != pb.DownloadState_DOWNLOAD_COMPLETED {
		t.Fatalf("expected DOWNLOAD_COMPLETED, got %v (%s)", st.GetState(), st.GetErrorMessage())
	}
	if st.GetTrackCount() != 3 {
		t.Fatalf("expected track_count 3, got %d", st.GetTrackCount())
	}

	for i := 1; i <= 3; i++ {
		for _, ext := range []string{"wav", "flac", "mp3"} {
			p := filepath.Join(dir, "1001", fmt.Sprintf("track%02d.cdda.%s", i, ext))
			info, err := os.Stat(p)
			if err != nil || info.Size() == 0 {
				t.Fatalf("expected non-empty %s, err=%v", p, err)
			}
		}
		wavData, _ := os.ReadFile(filepath.Join(dir, "1001", fmt.Sprintf("track%02d.cdda.wav", i)))
		if !bytes.HasSuffix(wavData, []byte(fmt.Sprintf("track%d", i))) {
			t.Errorf("track %d payload mismatch: %q", i, string(wavData))
		}
	}

	if _, err := os.Stat(filepath.Join(dir, ".tmp-1001")); !os.IsNotExist(err) {
		t.Errorf("expected staging dir to be gone, err=%v", err)
	}
	if _, err := os.Stat(oldQuarantine); !os.IsNotExist(err) {
		t.Errorf("expected old quarantine dir to be removed, err=%v", err)
	}

	enc.mu.Lock()
	callCount := len(enc.calls)
	enc.mu.Unlock()
	if callCount != 6 {
		t.Errorf("expected 6 encoder calls (3 flac + 3 mp3), got %d", callCount)
	}

	ripped, err := s.GetRipped(context.Background(), &pb.GetRippedRequest{})
	if err != nil {
		t.Fatalf("GetRipped failed: %v", err)
	}
	found := false
	for _, r := range ripped.GetRipped() {
		if r.GetId() == 1001 {
			found = true
			if len(r.GetTracks()) != 3 {
				t.Errorf("expected 3 tracks in s.rips for 1001, got %d", len(r.GetTracks()))
			}
		}
	}
	if !found {
		t.Errorf("expected release 1001 to appear in GetRipped after commit")
	}
	if calls := atomic.LoadInt32(&tg.releaseCalls); calls != 0 {
		t.Errorf("expected 0 getRelease calls when cached Tracklist is populated, got %d", calls)
	}
}

func TestDownloadReleaseSingleWavEndToEnd(t *testing.T) {
	enc := &mockAudioEncoder{}
	s, dir := newDownloadTestServer(t, enc)
	s.getter = &testGetter{
		records: map[int64]*pbrc.Record{
			5002: makeReleaseRecord(1002, 5002, 1, "File"),
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(makeFakeWav("single-track"))
	}))
	defer srv.Close()

	resp, err := s.DownloadRelease(context.Background(), &pb.DownloadReleaseRequest{
		ReleaseId:   1002,
		InstanceId:  5002,
		DownloadUrl: srv.URL + "/single.wav",
	})
	if err != nil {
		t.Fatalf("DownloadRelease failed: %v", err)
	}
	if resp.GetState() != pb.DownloadState_DOWNLOAD_QUEUED {
		t.Fatalf("expected DOWNLOAD_QUEUED, got %v", resp.GetState())
	}

	st := waitForTerminalStatus(t, s, 1002)
	if st.GetState() != pb.DownloadState_DOWNLOAD_COMPLETED || st.GetTrackCount() != 1 {
		t.Fatalf("expected DOWNLOAD_COMPLETED with 1 track, got %v (%d, %s)", st.GetState(), st.GetTrackCount(), st.GetErrorMessage())
	}

	for _, ext := range []string{"wav", "flac", "mp3"} {
		p := filepath.Join(dir, "1002", "track01.cdda."+ext)
		if info, err := os.Stat(p); err != nil || info.Size() == 0 {
			t.Fatalf("expected non-empty %s, err=%v", p, err)
		}
	}
}

func TestDownloadReleaseZipTrackSortingEdgeCases(t *testing.T) {
	t.Run("digits_in_titles", func(t *testing.T) {
		enc := &mockAudioEncoder{}
		s, dir := newDownloadTestServer(t, enc)
		s.getter = &testGetter{
			records: map[int64]*pbrc.Record{
				6001: makeReleaseRecord(2001, 6001, 4, "File"),
			},
		}
		zipBytes := buildTestZip(t, map[string][]byte{
			"Artist - Album - 02 1999.wav":            makeFakeWav("payload-2"),
			"Artist - Album - 01 99 Red Balloons.wav": makeFakeWav("payload-1"),
			"04 - 7 Days.wav":                         makeFakeWav("payload-4"),
			"03 - 24 Hours.wav":                       makeFakeWav("payload-3"),
		})
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write(zipBytes)
		}))
		defer srv.Close()

		if _, err := s.DownloadRelease(context.Background(), &pb.DownloadReleaseRequest{
			ReleaseId: 2001, InstanceId: 6001, DownloadUrl: srv.URL + "/a.zip",
		}); err != nil {
			t.Fatalf("DownloadRelease failed: %v", err)
		}
		st := waitForTerminalStatus(t, s, 2001)
		if st.GetState() != pb.DownloadState_DOWNLOAD_COMPLETED {
			t.Fatalf("expected DOWNLOAD_COMPLETED, got %v (%s)", st.GetState(), st.GetErrorMessage())
		}
		for i := 1; i <= 4; i++ {
			got, _ := os.ReadFile(filepath.Join(dir, "2001", fmt.Sprintf("track%02d.cdda.wav", i)))
			wantSuffix := fmt.Sprintf("payload-%d", i)
			if !bytes.HasSuffix(got, []byte(wantSuffix)) {
				t.Errorf("track %d got %q, want suffix %q", i, string(got), wantSuffix)
			}
		}
	})

	t.Run("over_50_tracks", func(t *testing.T) {
		const nTracks = 55
		enc := &mockAudioEncoder{}
		s, dir := newDownloadTestServer(t, enc)
		s.getter = &testGetter{
			records: map[int64]*pbrc.Record{
				6002: makeReleaseRecord(2002, 6002, nTracks, "File"),
			},
		}
		entries := make(map[string][]byte, nTracks)
		for i := nTracks; i >= 1; i-- {
			name := fmt.Sprintf("Artist - Big Album - %02d Track %d.wav", i, i)
			entries[name] = makeFakeWav(fmt.Sprintf("big-%02d", i))
		}
		zipBytes := buildTestZip(t, entries)
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write(zipBytes)
		}))
		defer srv.Close()

		if _, err := s.DownloadRelease(context.Background(), &pb.DownloadReleaseRequest{
			ReleaseId: 2002, InstanceId: 6002, DownloadUrl: srv.URL + "/big.zip",
		}); err != nil {
			t.Fatalf("DownloadRelease failed: %v", err)
		}
		st := waitForTerminalStatus(t, s, 2002)
		if st.GetState() != pb.DownloadState_DOWNLOAD_COMPLETED || st.GetTrackCount() != nTracks {
			t.Fatalf("expected completed with %d tracks, got %v (%d, %s)", nTracks, st.GetState(), st.GetTrackCount(), st.GetErrorMessage())
		}
		for i := 1; i <= nTracks; i++ {
			got, err := os.ReadFile(filepath.Join(dir, "2002", fmt.Sprintf("track%02d.cdda.wav", i)))
			if err != nil {
				t.Fatalf("read track %d: %v", i, err)
			}
			want := fmt.Sprintf("big-%02d", i)
			if !bytes.HasSuffix(got, []byte(want)) {
				t.Fatalf("track %d got %q, want suffix %q", i, string(got), want)
			}
		}
	})

	t.Run("multi_disc_prefixes", func(t *testing.T) {
		cases := []struct {
			name  string
			files map[string]string // zip path -> payload
			order []string          // expected payload suffixes in order
		}{
			{
				name: "dash_prefix",
				files: map[string]string{
					"2-01 Encore.wav": "p3",
					"1-02 Second.wav": "p2",
					"1-01 First.wav":  "p1",
				},
				order: []string{"p1", "p2", "p3"},
			},
			{
				name: "cd_prefix",
				files: map[string]string{
					"CD2-01 - Later.wav": "p3",
					"CD1-02 - Mid.wav":   "p2",
					"CD1-01 - Open.wav":  "p1",
				},
				order: []string{"p1", "p2", "p3"},
			},
			{
				name: "disc_dirs",
				files: map[string]string{
					"Disc 2/01 Song C.wav": "p3",
					"Disc 1/02 Song B.wav": "p2",
					"Disc 1/01 Song A.wav": "p1",
				},
				order: []string{"p1", "p2", "p3"},
			},
			{
				name: "disc_segment",
				files: map[string]string{
					"Artist - Album - Disc 2 - 01 Gamma.wav": "p3",
					"Artist - Album - Disc 1 - 02 Beta.wav":  "p2",
					"Artist - Album - Disc 1 - 01 Alpha.wav": "p1",
				},
				order: []string{"p1", "p2", "p3"},
			},
		}

		for idx, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				releaseID := int32(2100 + idx)
				instanceID := int64(6100 + idx)
				enc := &mockAudioEncoder{}
				s, dir := newDownloadTestServer(t, enc)
				s.getter = &testGetter{
					records: map[int64]*pbrc.Record{
						instanceID: makeReleaseRecord(releaseID, instanceID, len(tc.order), "File"),
					},
				}
				entries := make(map[string][]byte, len(tc.files))
				for p, payload := range tc.files {
					entries[p] = makeFakeWav(payload)
				}
				zipBytes := buildTestZip(t, entries)
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					_, _ = w.Write(zipBytes)
				}))
				defer srv.Close()

				if _, err := s.DownloadRelease(context.Background(), &pb.DownloadReleaseRequest{
					ReleaseId: releaseID, InstanceId: instanceID, DownloadUrl: srv.URL + "/md.zip",
				}); err != nil {
					t.Fatalf("DownloadRelease failed: %v", err)
				}
				st := waitForTerminalStatus(t, s, releaseID)
				if st.GetState() != pb.DownloadState_DOWNLOAD_COMPLETED {
					t.Fatalf("expected completed, got %v (%s)", st.GetState(), st.GetErrorMessage())
				}
				for i, want := range tc.order {
					got, _ := os.ReadFile(filepath.Join(dir, fmt.Sprintf("%d", releaseID), fmt.Sprintf("track%02d.cdda.wav", i+1)))
					if !bytes.HasSuffix(got, []byte(want)) {
						t.Errorf("track %d got %q, want suffix %q", i+1, string(got), want)
					}
				}
			})
		}
	})

	t.Run("junk_files_ignored", func(t *testing.T) {
		enc := &mockAudioEncoder{}
		s, dir := newDownloadTestServer(t, enc)
		s.getter = &testGetter{
			records: map[int64]*pbrc.Record{
				6003: makeReleaseRecord(2003, 6003, 2, "File"),
			},
		}
		zipBytes := buildTestZip(t, map[string][]byte{
			"__MACOSX/._01 First.wav": []byte("appledouble"),
			"cover.jpg":               []byte("jpegdata"),
			"extras/liner.pdf":        []byte("pdfdata"),
			"readme.txt":              []byte("hello"),
			".DS_Store":               []byte("dsstore"),
			"audio/01 First.wav":      makeFakeWav("first"),
			"audio/02 Second.wav":     makeFakeWav("second"),
		}, "audio/", "extras/")
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write(zipBytes)
		}))
		defer srv.Close()

		if _, err := s.DownloadRelease(context.Background(), &pb.DownloadReleaseRequest{
			ReleaseId: 2003, InstanceId: 6003, DownloadUrl: srv.URL + "/junk.zip",
		}); err != nil {
			t.Fatalf("DownloadRelease failed: %v", err)
		}
		st := waitForTerminalStatus(t, s, 2003)
		if st.GetState() != pb.DownloadState_DOWNLOAD_COMPLETED || st.GetTrackCount() != 2 {
			t.Fatalf("expected 2 tracks completed, got %v (%d, %s)", st.GetState(), st.GetTrackCount(), st.GetErrorMessage())
		}
		entries, _ := os.ReadDir(filepath.Join(dir, "2003"))
		if len(entries) != 6 {
			t.Errorf("expected 6 files (2x wav+flac+mp3), got %d", len(entries))
		}
	})

	t.Run("natural_sort_fallback", func(t *testing.T) {
		enc := &mockAudioEncoder{}
		s, dir := newDownloadTestServer(t, enc)
		s.getter = &testGetter{
			records: map[int64]*pbrc.Record{
				6004: makeReleaseRecord(2004, 6004, 3, "File"),
			},
		}
		zipBytes := buildTestZip(t, map[string][]byte{
			"song10.wav": makeFakeWav("p3"),
			"song2.wav":  makeFakeWav("p2"),
			"song1.wav":  makeFakeWav("p1"),
		})
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write(zipBytes)
		}))
		defer srv.Close()

		if _, err := s.DownloadRelease(context.Background(), &pb.DownloadReleaseRequest{
			ReleaseId: 2004, InstanceId: 6004, DownloadUrl: srv.URL + "/nat.zip",
		}); err != nil {
			t.Fatalf("DownloadRelease failed: %v", err)
		}
		st := waitForTerminalStatus(t, s, 2004)
		if st.GetState() != pb.DownloadState_DOWNLOAD_COMPLETED {
			t.Fatalf("expected completed, got %v (%s)", st.GetState(), st.GetErrorMessage())
		}
		for i, want := range []string{"p1", "p2", "p3"} {
			got, _ := os.ReadFile(filepath.Join(dir, "2004", fmt.Sprintf("track%02d.cdda.wav", i+1)))
			if !bytes.HasSuffix(got, []byte(want)) {
				t.Errorf("track %d got %q, want suffix %q", i+1, string(got), want)
			}
		}
	})

	t.Run("zip_slip_rejected", func(t *testing.T) {
		enc := &mockAudioEncoder{}
		s, dir := newDownloadTestServer(t, enc)
		zipBytes := buildTestZip(t, map[string][]byte{
			"../escape.wav": makeFakeWav("evil"),
			"01 Valid.wav":  makeFakeWav("good"),
		})
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write(zipBytes)
		}))
		defer srv.Close()

		if _, err := s.DownloadRelease(context.Background(), &pb.DownloadReleaseRequest{
			ReleaseId: 2005, InstanceId: 6005, DownloadUrl: srv.URL + "/slip.zip",
		}); err != nil {
			t.Fatalf("DownloadRelease failed: %v", err)
		}
		st := waitForTerminalStatus(t, s, 2005)
		if st.GetState() != pb.DownloadState_DOWNLOAD_FAILED_PERMANENT {
			t.Fatalf("expected DOWNLOAD_FAILED_PERMANENT, got %v", st.GetState())
		}
		if _, err := os.Stat(filepath.Join(dir, "escape.wav")); !os.IsNotExist(err) {
			t.Errorf("zip-slip wrote outside staging dir")
		}
		if _, err := os.Stat(filepath.Join(dir, ".tmp-2005")); !os.IsNotExist(err) {
			t.Errorf("expected staging dir to be cleaned up")
		}
	})
}

func TestDownloadReleaseTrackCountMismatchQuarantine(t *testing.T) {
	enc := &mockAudioEncoder{}
	s, dir := newDownloadTestServer(t, enc)
	s.getter = &testGetter{
		records: map[int64]*pbrc.Record{
			7001: makeReleaseRecord(3001, 7001, 3, "File"),
		},
	}

	priorQuarantine := filepath.Join(dir, ".quarantine", "3001")
	if err := os.MkdirAll(priorQuarantine, 0755); err != nil {
		t.Fatalf("mkdir prior quarantine: %v", err)
	}
	if err := os.WriteFile(filepath.Join(priorQuarantine, "old.txt"), []byte("old"), 0644); err != nil {
		t.Fatalf("write old quarantine: %v", err)
	}

	zipBytes := buildTestZip(t, map[string][]byte{
		"01 One.wav": makeFakeWav("one"),
		"02 Two.wav": makeFakeWav("two"),
	})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(zipBytes)
	}))
	defer srv.Close()

	if _, err := s.DownloadRelease(context.Background(), &pb.DownloadReleaseRequest{
		ReleaseId: 3001, InstanceId: 7001, DownloadUrl: srv.URL + "/mismatch.zip",
	}); err != nil {
		t.Fatalf("DownloadRelease failed: %v", err)
	}
	st := waitForTerminalStatus(t, s, 3001)
	if st.GetState() != pb.DownloadState_DOWNLOAD_FAILED_PERMANENT {
		t.Fatalf("expected DOWNLOAD_FAILED_PERMANENT, got %v", st.GetState())
	}
	if st.GetTrackCount() != 2 {
		t.Errorf("expected track_count 2, got %d", st.GetTrackCount())
	}
	if st.GetQuarantinePath() != priorQuarantine {
		t.Errorf("expected quarantine_path %q, got %q", priorQuarantine, st.GetQuarantinePath())
	}
	if !strings.Contains(st.GetErrorMessage(), "3") || !strings.Contains(st.GetErrorMessage(), "2") {
		t.Errorf("expected error message to include expected 3 and actual 2, got %q", st.GetErrorMessage())
	}
	if _, err := os.Stat(filepath.Join(priorQuarantine, "old.txt")); !os.IsNotExist(err) {
		t.Errorf("expected prior quarantine file to be replaced")
	}
	for i := 1; i <= 2; i++ {
		if _, err := os.Stat(filepath.Join(priorQuarantine, fmt.Sprintf("track%02d.cdda.wav", i))); err != nil {
			t.Errorf("expected quarantined track %d: %v", i, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "3001")); !os.IsNotExist(err) {
		t.Errorf("target dir should not exist on mismatch")
	}
}

func TestDownloadReleaseAlreadyExistsIdempotency(t *testing.T) {
	s, dir := newDownloadTestServer(t, &mockAudioEncoder{})
	releaseDir := filepath.Join(dir, "4001")
	if err := os.MkdirAll(releaseDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	for _, f := range []string{"track01.cdda.flac", "track01.cdda.mp3", "track02.cdda.flac", "track02.cdda.mp3"} {
		if err := os.WriteFile(filepath.Join(releaseDir, f), []byte("data"), 0644); err != nil {
			t.Fatalf("write %s: %v", f, err)
		}
	}

	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
	}))
	defer srv.Close()

	resp, err := s.DownloadRelease(context.Background(), &pb.DownloadReleaseRequest{
		ReleaseId: 4001, InstanceId: 8001, DownloadUrl: srv.URL + "/dup.zip",
	})
	if err != nil {
		t.Fatalf("DownloadRelease failed: %v", err)
	}
	if resp.GetState() != pb.DownloadState_DOWNLOAD_ALREADY_EXISTS {
		t.Fatalf("expected DOWNLOAD_ALREADY_EXISTS, got %v", resp.GetState())
	}
	if atomic.LoadInt32(&hits) != 0 {
		t.Errorf("expected 0 HTTP requests, got %d", hits)
	}
}

func TestDownloadReleaseIncompleteExistingDir(t *testing.T) {
	t.Run("empty_dir", func(t *testing.T) {
		s, dir := newDownloadTestServer(t, &mockAudioEncoder{})
		if err := os.MkdirAll(filepath.Join(dir, "4002"), 0755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		var hits int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			atomic.AddInt32(&hits, 1)
		}))
		defer srv.Close()

		resp, err := s.DownloadRelease(context.Background(), &pb.DownloadReleaseRequest{
			ReleaseId: 4002, InstanceId: 8002, DownloadUrl: srv.URL + "/x.zip",
		})
		if err != nil {
			t.Fatalf("DownloadRelease failed: %v", err)
		}
		if resp.GetState() != pb.DownloadState_DOWNLOAD_FAILED_PERMANENT {
			t.Fatalf("expected DOWNLOAD_FAILED_PERMANENT, got %v", resp.GetState())
		}
		if atomic.LoadInt32(&hits) != 0 {
			t.Errorf("expected 0 HTTP requests, got %d", hits)
		}
	})

	t.Run("partial_tracks_preserved", func(t *testing.T) {
		s, dir := newDownloadTestServer(t, &mockAudioEncoder{})
		releaseDir := filepath.Join(dir, "4003")
		if err := os.MkdirAll(releaseDir, 0755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		sentinel := filepath.Join(releaseDir, "track01.cdda.flac")
		if err := os.WriteFile(sentinel, []byte("keep-me"), 0644); err != nil {
			t.Fatalf("write sentinel: %v", err)
		}

		resp, err := s.DownloadRelease(context.Background(), &pb.DownloadReleaseRequest{
			ReleaseId: 4003, InstanceId: 8003, DownloadUrl: "http://127.0.0.1:1/unused.zip",
		})
		if err != nil {
			t.Fatalf("DownloadRelease failed: %v", err)
		}
		if resp.GetState() != pb.DownloadState_DOWNLOAD_FAILED_PERMANENT {
			t.Fatalf("expected DOWNLOAD_FAILED_PERMANENT, got %v", resp.GetState())
		}
		got, err := os.ReadFile(sentinel)
		if err != nil || string(got) != "keep-me" {
			t.Errorf("existing file modified or removed: %q, err=%v", string(got), err)
		}
	})
}

func TestGetDownloadStatusRestartFallback(t *testing.T) {
	s, dir := newDownloadTestServer(t, &mockAudioEncoder{})

	st, err := s.GetDownloadStatus(context.Background(), &pb.GetDownloadStatusRequest{ReleaseId: 5001})
	if err != nil {
		t.Fatalf("GetDownloadStatus error: %v", err)
	}
	if st.GetState() != pb.DownloadState_DOWNLOAD_NOT_FOUND {
		t.Fatalf("expected DOWNLOAD_NOT_FOUND, got %v", st.GetState())
	}

	releaseDir := filepath.Join(dir, "5001")
	if err := os.MkdirAll(releaseDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(releaseDir, "track01.cdda.flac"), []byte("f"), 0644); err != nil {
		t.Fatalf("write flac: %v", err)
	}

	st, err = s.GetDownloadStatus(context.Background(), &pb.GetDownloadStatusRequest{ReleaseId: 5001})
	if err != nil {
		t.Fatalf("GetDownloadStatus error: %v", err)
	}
	if st.GetState() != pb.DownloadState_DOWNLOAD_NOT_FOUND {
		t.Fatalf("expected DOWNLOAD_NOT_FOUND for incomplete dir, got %v", st.GetState())
	}

	for _, f := range []string{"track01.cdda.mp3", "track02.cdda.flac", "track02.cdda.mp3"} {
		if err := os.WriteFile(filepath.Join(releaseDir, f), []byte("ok"), 0644); err != nil {
			t.Fatalf("write %s: %v", f, err)
		}
	}

	st, err = s.GetDownloadStatus(context.Background(), &pb.GetDownloadStatusRequest{ReleaseId: 5001})
	if err != nil {
		t.Fatalf("GetDownloadStatus error: %v", err)
	}
	if st.GetState() != pb.DownloadState_DOWNLOAD_COMPLETED || st.GetTrackCount() != 2 {
		t.Fatalf("expected DOWNLOAD_COMPLETED with 2 tracks, got %v (%d)", st.GetState(), st.GetTrackCount())
	}
}

func TestDownloadReleaseRetryableFailures(t *testing.T) {
	for _, code := range []int{http.StatusForbidden, http.StatusNotFound, http.StatusGone} {
		t.Run(fmt.Sprintf("http_%d", code), func(t *testing.T) {
			s, dir := newDownloadTestServer(t, &mockAudioEncoder{})
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(code)
			}))
			defer srv.Close()

			if _, err := s.DownloadRelease(context.Background(), &pb.DownloadReleaseRequest{
				ReleaseId: 6001, InstanceId: 9001, DownloadUrl: srv.URL + "/expired.zip",
			}); err != nil {
				t.Fatalf("DownloadRelease error: %v", err)
			}
			st := waitForTerminalStatus(t, s, 6001)
			if st.GetState() != pb.DownloadState_DOWNLOAD_FAILED_RETRYABLE {
				t.Fatalf("expected DOWNLOAD_FAILED_RETRYABLE, got %v", st.GetState())
			}
			if _, err := os.Stat(filepath.Join(dir, ".tmp-6001")); !os.IsNotExist(err) {
				t.Errorf("expected staging dir cleanup")
			}
		})
	}

	t.Run("transient_recordcollection_and_retry", func(t *testing.T) {
		enc := &mockAudioEncoder{}
		s, _ := newDownloadTestServer(t, enc)
		tg := &testGetter{
			errByID: map[int64]error{
				9002: status.Error(codes.Unavailable, "temporary outage"),
			},
		}
		s.getter = tg

		wavBytes := makeFakeWav("retry-ok")
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write(wavBytes)
		}))
		defer srv.Close()

		if _, err := s.DownloadRelease(context.Background(), &pb.DownloadReleaseRequest{
			ReleaseId: 6002, InstanceId: 9002, DownloadUrl: srv.URL + "/t.wav",
		}); err != nil {
			t.Fatalf("DownloadRelease error: %v", err)
		}
		st := waitForTerminalStatus(t, s, 6002)
		if st.GetState() != pb.DownloadState_DOWNLOAD_FAILED_RETRYABLE {
			t.Fatalf("expected DOWNLOAD_FAILED_RETRYABLE, got %v", st.GetState())
		}

		// Clear transient error and retry with fresh URL.
		tg.errByID = nil
		tg.records = map[int64]*pbrc.Record{
			9002: makeReleaseRecord(6002, 9002, 1, "File"),
		}
		resp, err := s.DownloadRelease(context.Background(), &pb.DownloadReleaseRequest{
			ReleaseId: 6002, InstanceId: 9002, DownloadUrl: srv.URL + "/t2.wav",
		})
		if err != nil {
			t.Fatalf("retry DownloadRelease error: %v", err)
		}
		if resp.GetState() != pb.DownloadState_DOWNLOAD_QUEUED {
			t.Fatalf("expected DOWNLOAD_QUEUED on retry, got %v", resp.GetState())
		}
		st = waitForTerminalStatus(t, s, 6002)
		if st.GetState() != pb.DownloadState_DOWNLOAD_COMPLETED {
			t.Fatalf("expected DOWNLOAD_COMPLETED on retry, got %v (%s)", st.GetState(), st.GetErrorMessage())
		}
	})

	t.Run("encoder_error_and_empty_output", func(t *testing.T) {
		for _, tc := range []struct {
			name string
			enc  *mockAudioEncoder
		}{
			{name: "flac_error", enc: &mockAudioEncoder{flacErr: fmt.Errorf("flac boom")}},
			{name: "mp3_error", enc: &mockAudioEncoder{mp3Err: fmt.Errorf("lame boom")}},
			{name: "empty_output", enc: &mockAudioEncoder{emptyOutput: true}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				s, dir := newDownloadTestServer(t, tc.enc)
				s.getter = &testGetter{
					records: map[int64]*pbrc.Record{
						9003: makeReleaseRecord(6003, 9003, 1, "File"),
					},
				}
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					_, _ = w.Write(makeFakeWav("audio"))
				}))
				defer srv.Close()

				if _, err := s.DownloadRelease(context.Background(), &pb.DownloadReleaseRequest{
					ReleaseId: 6003, InstanceId: 9003, DownloadUrl: srv.URL + "/t.wav",
				}); err != nil {
					t.Fatalf("DownloadRelease error: %v", err)
				}
				st := waitForTerminalStatus(t, s, 6003)
				if st.GetState() != pb.DownloadState_DOWNLOAD_FAILED_RETRYABLE {
					t.Fatalf("expected DOWNLOAD_FAILED_RETRYABLE, got %v (%s)", st.GetState(), st.GetErrorMessage())
				}
				if _, err := os.Stat(filepath.Join(dir, ".tmp-6003")); !os.IsNotExist(err) {
					t.Errorf("expected staging dir cleaned up")
				}
			})
		}
	})
}

func TestDownloadReleasePermanentFormatFailures(t *testing.T) {
	t.Run("unrecognized_magic", func(t *testing.T) {
		s, dir := newDownloadTestServer(t, &mockAudioEncoder{})
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("<html>not audio</html>"))
		}))
		defer srv.Close()

		if _, err := s.DownloadRelease(context.Background(), &pb.DownloadReleaseRequest{
			ReleaseId: 7001, InstanceId: 9101, DownloadUrl: srv.URL + "/bad",
		}); err != nil {
			t.Fatalf("DownloadRelease error: %v", err)
		}
		st := waitForTerminalStatus(t, s, 7001)
		if st.GetState() != pb.DownloadState_DOWNLOAD_FAILED_PERMANENT {
			t.Fatalf("expected DOWNLOAD_FAILED_PERMANENT, got %v", st.GetState())
		}
		if _, err := os.Stat(filepath.Join(dir, ".tmp-7001")); !os.IsNotExist(err) {
			t.Errorf("expected staging dir cleaned up")
		}
	})

	t.Run("zip_without_wav", func(t *testing.T) {
		s, dir := newDownloadTestServer(t, &mockAudioEncoder{})
		zipBytes := buildTestZip(t, map[string][]byte{
			"cover.jpg": []byte("jpg"),
			"notes.txt": []byte("txt"),
		})
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write(zipBytes)
		}))
		defer srv.Close()

		if _, err := s.DownloadRelease(context.Background(), &pb.DownloadReleaseRequest{
			ReleaseId: 7002, InstanceId: 9102, DownloadUrl: srv.URL + "/empty.zip",
		}); err != nil {
			t.Fatalf("DownloadRelease error: %v", err)
		}
		st := waitForTerminalStatus(t, s, 7002)
		if st.GetState() != pb.DownloadState_DOWNLOAD_FAILED_PERMANENT {
			t.Fatalf("expected DOWNLOAD_FAILED_PERMANENT, got %v", st.GetState())
		}
		if _, err := os.Stat(filepath.Join(dir, ".tmp-7002")); !os.IsNotExist(err) {
			t.Errorf("expected staging dir cleaned up")
		}
	})
}

func TestDownloadReleaseValidation(t *testing.T) {
	s, _ := newDownloadTestServer(t, &mockAudioEncoder{})

	invalidReqs := []*pb.DownloadReleaseRequest{
		{ReleaseId: 0, InstanceId: 1, DownloadUrl: "https://example.com/a.zip"},
		{ReleaseId: -1, InstanceId: 1, DownloadUrl: "https://example.com/a.zip"},
		{ReleaseId: 1, InstanceId: 0, DownloadUrl: "https://example.com/a.zip"},
		{ReleaseId: 1, InstanceId: -5, DownloadUrl: "https://example.com/a.zip"},
		{ReleaseId: 1, InstanceId: 1, DownloadUrl: "ftp://example.com/a.zip"},
		{ReleaseId: 1, InstanceId: 1, DownloadUrl: "not-a-url"},
		{ReleaseId: 1, InstanceId: 1, DownloadUrl: ""},
	}
	for i, req := range invalidReqs {
		_, err := s.DownloadRelease(context.Background(), req)
		if status.Code(err) != codes.InvalidArgument {
			t.Errorf("case %d: expected InvalidArgument, got %v", i, err)
		}
	}

	_, err := s.GetDownloadStatus(context.Background(), &pb.GetDownloadStatusRequest{ReleaseId: 0})
	if status.Code(err) != codes.InvalidArgument {
		t.Errorf("expected InvalidArgument for GetDownloadStatus(0), got %v", err)
	}
}

func TestDownloadReleaseDeduplicationAndSingleConcurrency(t *testing.T) {
	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	var active int32
	var maxConcurrent int32

	enc := &mockAudioEncoder{
		onFlacStart: func(wavPath string) {
			cur := atomic.AddInt32(&active, 1)
			for {
				prev := atomic.LoadInt32(&maxConcurrent)
				if cur <= prev || atomic.CompareAndSwapInt32(&maxConcurrent, prev, cur) {
					break
				}
			}
			defer atomic.AddInt32(&active, -1)

			if strings.Contains(wavPath, ".tmp-8001") {
				select {
				case <-firstStarted:
				default:
					close(firstStarted)
				}
				<-releaseFirst
			}
		},
	}

	s, _ := newDownloadTestServer(t, enc)
	s.getter = &testGetter{
		records: map[int64]*pbrc.Record{
			9201: makeReleaseRecord(8001, 9201, 1, "File"),
			9202: makeReleaseRecord(8002, 9202, 1, "File"),
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(makeFakeWav("data"))
	}))
	defer srv.Close()

	resp1, err := s.DownloadRelease(context.Background(), &pb.DownloadReleaseRequest{
		ReleaseId: 8001, InstanceId: 9201, DownloadUrl: srv.URL + "/1.wav",
	})
	if err != nil || resp1.GetState() != pb.DownloadState_DOWNLOAD_QUEUED {
		t.Fatalf("first DownloadRelease got (%v, %v)", resp1, err)
	}

	<-firstStarted

	// Re-submitting 8001 while in progress should return current state without re-enqueueing.
	dupResp, err := s.DownloadRelease(context.Background(), &pb.DownloadReleaseRequest{
		ReleaseId: 8001, InstanceId: 9201, DownloadUrl: srv.URL + "/1.wav",
	})
	if err != nil || dupResp.GetState() != pb.DownloadState_DOWNLOAD_IN_PROGRESS {
		t.Fatalf("expected DOWNLOAD_IN_PROGRESS on duplicate call, got (%v, %v)", dupResp, err)
	}

	// Enqueue 8002 while 8001 is blocked in encoder.
	resp2, err := s.DownloadRelease(context.Background(), &pb.DownloadReleaseRequest{
		ReleaseId: 8002, InstanceId: 9202, DownloadUrl: srv.URL + "/2.wav",
	})
	if err != nil || resp2.GetState() != pb.DownloadState_DOWNLOAD_QUEUED {
		t.Fatalf("second DownloadRelease got (%v, %v)", resp2, err)
	}

	// Verify 8002 remains queued while 8001 is still in progress.
	time.Sleep(50 * time.Millisecond)
	st2, err := s.GetDownloadStatus(context.Background(), &pb.GetDownloadStatusRequest{ReleaseId: 8002})
	if err != nil || st2.GetState() != pb.DownloadState_DOWNLOAD_QUEUED {
		t.Fatalf("expected 8002 to remain DOWNLOAD_QUEUED while 8001 active, got (%v, %v)", st2, err)
	}

	close(releaseFirst)

	st1 := waitForTerminalStatus(t, s, 8001)
	st2 = waitForTerminalStatus(t, s, 8002)
	if st1.GetState() != pb.DownloadState_DOWNLOAD_COMPLETED || st2.GetState() != pb.DownloadState_DOWNLOAD_COMPLETED {
		t.Fatalf("expected both completed, got %v and %v", st1.GetState(), st2.GetState())
	}
	if atomic.LoadInt32(&maxConcurrent) != 1 {
		t.Errorf("expected max concurrent encodings == 1, got %d", maxConcurrent)
	}
}

func TestDownloadReleaseFallbackToLiveRelease(t *testing.T) {
	t.Run("fallback_success", func(t *testing.T) {
		enc := &mockAudioEncoder{}
		s, dir := newDownloadTestServer(t, enc)
		const releaseID = int32(31692413)
		const instanceID = int64(9901)

		tg := &testGetter{
			records: map[int64]*pbrc.Record{
				instanceID: {
					Release: &pbgd.Release{
						Id:         releaseID,
						InstanceId: instanceID,
						FolderId:   1782105,
					},
					Metadata: &pbrc.ReleaseMetadata{
						GoalFolder: 1782105,
						FiledUnder: pbrc.ReleaseMetadata_FILE_DIGITAL,
					},
				},
			},
			releases: map[int32]*pbrc.Record{
				releaseID: {
					Release: makeReleaseRecord(releaseID, instanceID, 3, "File").GetRelease(),
				},
			},
		}
		s.getter = tg

		zipBytes := buildTestZip(t, map[string][]byte{
			"01 One.wav":   makeFakeWav("track1"),
			"02 Two.wav":   makeFakeWav("track2"),
			"03 Three.wav": makeFakeWav("track3"),
		})
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write(zipBytes)
		}))
		defer srv.Close()

		resp, err := s.DownloadRelease(context.Background(), &pb.DownloadReleaseRequest{
			ReleaseId:   releaseID,
			InstanceId:  instanceID,
			DownloadUrl: srv.URL + "/album.zip",
		})
		if err != nil {
			t.Fatalf("DownloadRelease failed: %v", err)
		}
		if resp.GetState() != pb.DownloadState_DOWNLOAD_QUEUED {
			t.Fatalf("expected DOWNLOAD_QUEUED, got %v", resp.GetState())
		}

		st := waitForTerminalStatus(t, s, releaseID)
		if st.GetState() != pb.DownloadState_DOWNLOAD_COMPLETED {
			t.Fatalf("expected DOWNLOAD_COMPLETED, got %v (%s)", st.GetState(), st.GetErrorMessage())
		}
		if st.GetTrackCount() != 3 {
			t.Fatalf("expected track_count 3, got %d", st.GetTrackCount())
		}
		if calls := atomic.LoadInt32(&tg.releaseCalls); calls != 1 {
			t.Errorf("expected 1 getRelease call, got %d", calls)
		}
		for i := 1; i <= 3; i++ {
			for _, ext := range []string{"wav", "flac", "mp3"} {
				p := filepath.Join(dir, fmt.Sprintf("%d", releaseID), fmt.Sprintf("track%02d.cdda.%s", i, ext))
				info, err := os.Stat(p)
				if err != nil || info.Size() == 0 {
					t.Fatalf("expected non-empty %s, err=%v", p, err)
				}
			}
		}
	})

	t.Run("fallback_transient_error_retryable", func(t *testing.T) {
		enc := &mockAudioEncoder{}
		s, dir := newDownloadTestServer(t, enc)
		const releaseID = int32(31692415)
		const instanceID = int64(9903)

		s.getter = &testGetter{
			records: map[int64]*pbrc.Record{
				instanceID: {
					Release: &pbgd.Release{Id: releaseID, InstanceId: instanceID},
				},
			},
			releaseErrByID: map[int32]error{
				releaseID: status.Error(codes.Unavailable, "discogs unavailable"),
			},
		}

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write(makeFakeWav("single"))
		}))
		defer srv.Close()

		if _, err := s.DownloadRelease(context.Background(), &pb.DownloadReleaseRequest{
			ReleaseId: releaseID, InstanceId: instanceID, DownloadUrl: srv.URL + "/t.wav",
		}); err != nil {
			t.Fatalf("DownloadRelease failed: %v", err)
		}

		st := waitForTerminalStatus(t, s, releaseID)
		if st.GetState() != pb.DownloadState_DOWNLOAD_FAILED_RETRYABLE {
			t.Fatalf("expected DOWNLOAD_FAILED_RETRYABLE, got %v (%s)", st.GetState(), st.GetErrorMessage())
		}
		if _, err := os.Stat(filepath.Join(dir, fmt.Sprintf(".tmp-%d", releaseID))); !os.IsNotExist(err) {
			t.Errorf("expected staging dir removed, err=%v", err)
		}
		if _, err := os.Stat(filepath.Join(dir, ".quarantine", fmt.Sprintf("%d", releaseID))); !os.IsNotExist(err) {
			t.Errorf("expected release not to be quarantined on transient error, err=%v", err)
		}
	})

	t.Run("fallback_permanent_error", func(t *testing.T) {
		enc := &mockAudioEncoder{}
		s, dir := newDownloadTestServer(t, enc)
		const releaseID = int32(31692416)
		const instanceID = int64(9904)

		s.getter = &testGetter{
			records: map[int64]*pbrc.Record{
				instanceID: {
					Release: &pbgd.Release{Id: releaseID, InstanceId: instanceID},
				},
			},
			releaseErrByID: map[int32]error{
				releaseID: status.Error(codes.NotFound, "release not found"),
			},
		}

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write(makeFakeWav("single"))
		}))
		defer srv.Close()

		if _, err := s.DownloadRelease(context.Background(), &pb.DownloadReleaseRequest{
			ReleaseId: releaseID, InstanceId: instanceID, DownloadUrl: srv.URL + "/t.wav",
		}); err != nil {
			t.Fatalf("DownloadRelease failed: %v", err)
		}

		st := waitForTerminalStatus(t, s, releaseID)
		if st.GetState() != pb.DownloadState_DOWNLOAD_FAILED_PERMANENT {
			t.Fatalf("expected DOWNLOAD_FAILED_PERMANENT, got %v (%s)", st.GetState(), st.GetErrorMessage())
		}
		if _, err := os.Stat(filepath.Join(dir, fmt.Sprintf(".tmp-%d", releaseID))); !os.IsNotExist(err) {
			t.Errorf("expected staging dir removed, err=%v", err)
		}
		if _, err := os.Stat(filepath.Join(dir, ".quarantine", fmt.Sprintf("%d", releaseID))); !os.IsNotExist(err) {
			t.Errorf("expected release not to be quarantined on NotFound error, err=%v", err)
		}
	})

	t.Run("fallback_track_count_mismatch_quarantines", func(t *testing.T) {
		enc := &mockAudioEncoder{}
		s, dir := newDownloadTestServer(t, enc)
		const releaseID = int32(31692417)
		const instanceID = int64(9905)

		s.getter = &testGetter{
			records: map[int64]*pbrc.Record{
				instanceID: {
					Release: &pbgd.Release{Id: releaseID, InstanceId: instanceID},
				},
			},
			releases: map[int32]*pbrc.Record{
				releaseID: {
					Release: makeReleaseRecord(releaseID, instanceID, 4, "File").GetRelease(),
				},
			},
		}

		zipBytes := buildTestZip(t, map[string][]byte{
			"01 One.wav": makeFakeWav("one"),
			"02 Two.wav": makeFakeWav("two"),
		})
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write(zipBytes)
		}))
		defer srv.Close()

		if _, err := s.DownloadRelease(context.Background(), &pb.DownloadReleaseRequest{
			ReleaseId: releaseID, InstanceId: instanceID, DownloadUrl: srv.URL + "/mismatch.zip",
		}); err != nil {
			t.Fatalf("DownloadRelease failed: %v", err)
		}

		st := waitForTerminalStatus(t, s, releaseID)
		wantQuarantine := filepath.Join(dir, ".quarantine", fmt.Sprintf("%d", releaseID))
		if st.GetState() != pb.DownloadState_DOWNLOAD_FAILED_PERMANENT {
			t.Fatalf("expected DOWNLOAD_FAILED_PERMANENT, got %v", st.GetState())
		}
		if st.GetTrackCount() != 2 {
			t.Errorf("expected track_count 2, got %d", st.GetTrackCount())
		}
		if st.GetQuarantinePath() != wantQuarantine {
			t.Errorf("expected quarantine_path %q, got %q", wantQuarantine, st.GetQuarantinePath())
		}
		if !strings.Contains(st.GetErrorMessage(), "4") || !strings.Contains(st.GetErrorMessage(), "2") {
			t.Errorf("expected error message to mention 4 and 2, got %q", st.GetErrorMessage())
		}
		for i := 1; i <= 2; i++ {
			if _, err := os.Stat(filepath.Join(wantQuarantine, fmt.Sprintf("track%02d.cdda.wav", i))); err != nil {
				t.Errorf("expected quarantined track %d: %v", i, err)
			}
		}
	})

	t.Run("fallback_live_release_zero_tracks", func(t *testing.T) {
		enc := &mockAudioEncoder{}
		s, dir := newDownloadTestServer(t, enc)
		const releaseID = int32(31692418)
		const instanceID = int64(9906)

		tg := &testGetter{
			records: map[int64]*pbrc.Record{
				instanceID: {
					Release: &pbgd.Release{Id: releaseID, InstanceId: instanceID},
				},
			},
			releases: map[int32]*pbrc.Record{
				releaseID: {
					Release: &pbgd.Release{Id: releaseID},
				},
			},
		}
		s.getter = tg

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write(makeFakeWav("single"))
		}))
		defer srv.Close()

		if _, err := s.DownloadRelease(context.Background(), &pb.DownloadReleaseRequest{
			ReleaseId: releaseID, InstanceId: instanceID, DownloadUrl: srv.URL + "/zero.wav",
		}); err != nil {
			t.Fatalf("DownloadRelease failed: %v", err)
		}

		st := waitForTerminalStatus(t, s, releaseID)
		if calls := atomic.LoadInt32(&tg.releaseCalls); calls != 1 {
			t.Errorf("expected 1 getRelease call, got %d", calls)
		}
		wantQuarantine := filepath.Join(dir, ".quarantine", fmt.Sprintf("%d", releaseID))
		if st.GetState() != pb.DownloadState_DOWNLOAD_FAILED_PERMANENT {
			t.Fatalf("expected DOWNLOAD_FAILED_PERMANENT, got %v", st.GetState())
		}
		if st.GetQuarantinePath() != wantQuarantine {
			t.Errorf("expected quarantine_path %q, got %q", wantQuarantine, st.GetQuarantinePath())
		}
		if !strings.Contains(st.GetErrorMessage(), "expected 0") || !strings.Contains(st.GetErrorMessage(), "extracted 1") {
			t.Errorf("expected error message to mention expected 0 and extracted 1, got %q", st.GetErrorMessage())
		}
	})
}
