package main

import (
	"archive/zip"
	"bytes"
	"fmt"
	stdlibio "io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	pb "github.com/brotherlogic/cdprocessor/proto"
	"github.com/brotherlogic/goserver/utils"
	pbrc "github.com/brotherlogic/recordcollection/proto"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"golang.org/x/net/context"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var (
	downloadJobsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "cdprocessor_download_jobs_total",
		Help: "Total number of digital release download jobs by status",
	}, []string{"status"})

	trackFilePattern      = regexp.MustCompile(`^track(\d{2,})\.cdda\.(flac|mp3)$`)
	dirDiscPattern        = regexp.MustCompile(`(?i)(?:disc|cd|disk)\s*(\d+)`)
	discTrackExactPattern = regexp.MustCompile(`^(?:(?i)(?:cd|disc|disk)\s*)?(\d+)[-_](\d+)$`)
	dNtMPattern           = regexp.MustCompile(`(?i)^d(\d+)t(\d+)$`)
	leadingDiscTrackPat   = regexp.MustCompile(`^(?:(?i)(?:cd|disc|disk)\s*)?(\d+)[-_](\d+)(?:\s+|[._-]|$)`)
	pureShortDigitsPat    = regexp.MustCompile(`^\d{1,3}$`)
	discSegPattern        = regexp.MustCompile(`(?i)^(?:cd|disc|disk)\s*(\d+)$`)
	leadingTrackSepPat    = regexp.MustCompile(`^(\d+)(?:\s+|[._-])`)
	leadingTrackEndPat    = regexp.MustCompile(`^(\d+)(?:\s+|[._-]|$)`)
	trackWordPattern      = regexp.MustCompile(`(?i)^track\s*(\d+)`)
)

type audioEncoder interface {
	EncodeFlac(ctx context.Context, wavPath, flacPath string) error
	EncodeMP3(ctx context.Context, wavPath, mp3Path string) error
}

type execAudioEncoder struct{}

func (e *execAudioEncoder) EncodeFlac(ctx context.Context, wavPath, flacPath string) error {
	cmd := exec.CommandContext(ctx, "flac", "--best", wavPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("flac failed for %s: %w: %s", wavPath, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (e *execAudioEncoder) EncodeMP3(ctx context.Context, wavPath, mp3Path string) error {
	cmd := exec.CommandContext(ctx, "lame", wavPath, mp3Path)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("lame failed for %s: %w: %s", wavPath, err, strings.TrimSpace(string(out)))
	}
	return nil
}

type downloadJobState struct {
	state          pb.DownloadState
	trackCount     int32
	errorMessage   string
	quarantinePath string
}

type downloadTask struct {
	releaseID   int32
	instanceID  int64
	downloadURL string
}

type downloadManager struct {
	server     *Server
	mu         sync.Mutex
	jobs       map[int32]*downloadJobState
	pending    []downloadTask
	notify     chan struct{}
	stopCh     chan struct{}
	doneCh     chan struct{}
	started    bool
	stopped    bool
	stopOnce   sync.Once
	httpClient *http.Client
	encoder    audioEncoder
}

func newDownloadManager(s *Server) *downloadManager {
	return &downloadManager{
		server:     s,
		jobs:       make(map[int32]*downloadJobState),
		notify:     make(chan struct{}, 1),
		stopCh:     make(chan struct{}),
		doneCh:     make(chan struct{}),
		httpClient: &http.Client{Timeout: 30 * time.Minute},
		encoder:    &execAudioEncoder{},
	}
}

func (dm *downloadManager) ensureStartedLocked() {
	if !dm.started && !dm.stopped {
		dm.started = true
		go dm.workerLoop()
	}
}

func (dm *downloadManager) stop() {
	dm.stopOnce.Do(func() {
		dm.mu.Lock()
		dm.stopped = true
		started := dm.started
		dm.mu.Unlock()
		close(dm.stopCh)
		if started {
			<-dm.doneCh
		}
	})
}

func (dm *downloadManager) workerLoop() {
	defer close(dm.doneCh)
	for {
		dm.mu.Lock()
		if len(dm.pending) == 0 {
			dm.mu.Unlock()
			select {
			case <-dm.stopCh:
				return
			case <-dm.notify:
				continue
			}
		}
		task := dm.pending[0]
		dm.pending = dm.pending[1:]
		dm.mu.Unlock()

		select {
		case <-dm.stopCh:
			return
		default:
		}

		ctx, cancel := utils.ManualContext("cdprocessor-download", 30*time.Minute)
		watchDone := make(chan struct{})
		go func() {
			select {
			case <-dm.stopCh:
				cancel()
			case <-watchDone:
			}
		}()

		dm.processDownload(ctx, task)
		close(watchDone)
		cancel()
	}
}

func (dm *downloadManager) setJobState(releaseID int32, job *downloadJobState) {
	dm.mu.Lock()
	defer dm.mu.Unlock()
	dm.jobs[releaseID] = job
}

// DownloadRelease queues an asynchronous digital release download, unpack, validation, and encoding job.
func (s *Server) DownloadRelease(ctx context.Context, req *pb.DownloadReleaseRequest) (*pb.DownloadReleaseResponse, error) {
	releaseID := req.GetReleaseId()
	if releaseID <= 0 {
		return nil, status.Errorf(codes.InvalidArgument, "release_id must be positive, got %d", releaseID)
	}
	if req.GetInstanceId() <= 0 {
		return nil, status.Errorf(codes.InvalidArgument, "instance_id must be positive, got %d", req.GetInstanceId())
	}
	parsedURL, err := url.Parse(req.GetDownloadUrl())
	if err != nil || (parsedURL.Scheme != "http" && parsedURL.Scheme != "https") || parsedURL.Host == "" {
		return nil, status.Errorf(codes.InvalidArgument, "download_url must be a valid http or https URL")
	}

	if s.downloader == nil {
		s.downloader = newDownloadManager(s)
	}
	dm := s.downloader

	dm.mu.Lock()
	defer dm.mu.Unlock()

	if existing, ok := dm.jobs[releaseID]; ok {
		if existing.state == pb.DownloadState_DOWNLOAD_QUEUED || existing.state == pb.DownloadState_DOWNLOAD_IN_PROGRESS {
			return &pb.DownloadReleaseResponse{
				State:   existing.state,
				Message: fmt.Sprintf("download already %s", strings.ToLower(existing.state.String())),
			}, nil
		}
	}

	releaseDir := filepath.Join(s.dir, strconv.Itoa(int(releaseID)))
	exists, complete, _ := inspectReleaseDir(releaseDir)
	if exists {
		if complete {
			return &pb.DownloadReleaseResponse{
				State:   pb.DownloadState_DOWNLOAD_ALREADY_EXISTS,
				Message: "release already exists on disk",
			}, nil
		}
		msg := fmt.Sprintf("existing release directory %s is empty or incomplete", releaseDir)
		dm.jobs[releaseID] = &downloadJobState{
			state:        pb.DownloadState_DOWNLOAD_FAILED_PERMANENT,
			errorMessage: msg,
		}
		downloadJobsTotal.WithLabelValues("failed_permanent").Inc()
		return &pb.DownloadReleaseResponse{
			State:   pb.DownloadState_DOWNLOAD_FAILED_PERMANENT,
			Message: msg,
		}, nil
	}

	dm.jobs[releaseID] = &downloadJobState{
		state: pb.DownloadState_DOWNLOAD_QUEUED,
	}
	dm.pending = append(dm.pending, downloadTask{
		releaseID:   releaseID,
		instanceID:  req.GetInstanceId(),
		downloadURL: req.GetDownloadUrl(),
	})
	downloadJobsTotal.WithLabelValues("queued").Inc()
	dm.ensureStartedLocked()

	select {
	case dm.notify <- struct{}{}:
	default:
	}

	return &pb.DownloadReleaseResponse{
		State:   pb.DownloadState_DOWNLOAD_QUEUED,
		Message: "download queued",
	}, nil
}

// GetDownloadStatus returns the current status of a release download job or inspects disk after restarts.
func (s *Server) GetDownloadStatus(ctx context.Context, req *pb.GetDownloadStatusRequest) (*pb.GetDownloadStatusResponse, error) {
	releaseID := req.GetReleaseId()
	if releaseID <= 0 {
		return nil, status.Errorf(codes.InvalidArgument, "release_id must be positive, got %d", releaseID)
	}

	if s.downloader != nil {
		dm := s.downloader
		dm.mu.Lock()
		job, ok := dm.jobs[releaseID]
		var snapshot downloadJobState
		if ok {
			snapshot = *job
		}
		dm.mu.Unlock()

		if ok {
			return &pb.GetDownloadStatusResponse{
				State:          snapshot.state,
				TrackCount:     snapshot.trackCount,
				ErrorMessage:   snapshot.errorMessage,
				QuarantinePath: snapshot.quarantinePath,
			}, nil
		}
	}

	releaseDir := filepath.Join(s.dir, strconv.Itoa(int(releaseID)))
	exists, complete, trackCount := inspectReleaseDir(releaseDir)
	if exists && complete {
		return &pb.GetDownloadStatusResponse{
			State:      pb.DownloadState_DOWNLOAD_COMPLETED,
			TrackCount: int32(trackCount),
		}, nil
	}

	return &pb.GetDownloadStatusResponse{
		State: pb.DownloadState_DOWNLOAD_NOT_FOUND,
	}, nil
}

func inspectReleaseDir(releaseDir string) (exists bool, complete bool, trackCount int) {
	info, err := os.Stat(releaseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return false, false, 0
		}
		return true, false, 0
	}
	if !info.IsDir() {
		return true, false, 0
	}

	entries, err := os.ReadDir(releaseDir)
	if err != nil {
		return true, false, 0
	}

	flacTracks := make(map[int]bool)
	mp3Tracks := make(map[int]bool)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		matches := trackFilePattern.FindStringSubmatch(entry.Name())
		if matches == nil {
			continue
		}
		entryInfo, err := entry.Info()
		if err != nil || entryInfo.Size() <= 0 {
			continue
		}
		num, err := strconv.Atoi(matches[1])
		if err != nil || num <= 0 {
			continue
		}
		switch matches[2] {
		case "flac":
			flacTracks[num] = true
		case "mp3":
			mp3Tracks[num] = true
		}
	}

	if len(flacTracks) == 0 || len(flacTracks) != len(mp3Tracks) {
		return true, false, 0
	}
	for i := 1; i <= len(flacTracks); i++ {
		if !flacTracks[i] || !mp3Tracks[i] {
			return true, false, 0
		}
	}

	return true, true, len(flacTracks)
}

func (dm *downloadManager) processDownload(ctx context.Context, task downloadTask) {
	s := dm.server
	dm.setJobState(task.releaseID, &downloadJobState{
		state: pb.DownloadState_DOWNLOAD_IN_PROGRESS,
	})

	stagingDir := filepath.Join(s.dir, fmt.Sprintf(".tmp-%d", task.releaseID))
	failRetryable := func(msg string) {
		_ = os.RemoveAll(stagingDir)
		s.CtxLog(ctx, fmt.Sprintf("Download %d failed (retryable): %s", task.releaseID, msg))
		dm.setJobState(task.releaseID, &downloadJobState{
			state:        pb.DownloadState_DOWNLOAD_FAILED_RETRYABLE,
			errorMessage: msg,
		})
		downloadJobsTotal.WithLabelValues("failed_retryable").Inc()
	}
	failPermanent := func(msg string) {
		_ = os.RemoveAll(stagingDir)
		s.CtxLog(ctx, fmt.Sprintf("Download %d failed (permanent): %s", task.releaseID, msg))
		dm.setJobState(task.releaseID, &downloadJobState{
			state:        pb.DownloadState_DOWNLOAD_FAILED_PERMANENT,
			errorMessage: msg,
		})
		downloadJobsTotal.WithLabelValues("failed_permanent").Inc()
	}

	if err := os.MkdirAll(s.dir, 0755); err != nil {
		failRetryable(fmt.Sprintf("create base dir: %v", err))
		return
	}
	if err := os.RemoveAll(stagingDir); err != nil {
		failRetryable(fmt.Sprintf("clean staging dir: %v", err))
		return
	}
	if err := os.MkdirAll(stagingDir, 0755); err != nil {
		failRetryable(fmt.Sprintf("create staging dir: %v", err))
		return
	}

	downloadPath := filepath.Join(stagingDir, "download.part")
	if err := dm.downloadToFile(ctx, task.downloadURL, downloadPath); err != nil {
		failRetryable(err.Error())
		return
	}

	header, err := readMagicHeader(downloadPath)
	if err != nil {
		failRetryable(fmt.Sprintf("read downloaded header: %v", err))
		return
	}

	var trackCount int
	switch {
	case isZipMagic(header):
		trackCount, err = extractAndStageZip(downloadPath, stagingDir)
		if err != nil {
			failPermanent(err.Error())
			return
		}
		_ = os.Remove(downloadPath)
	case isWavMagic(header):
		dest := filepath.Join(stagingDir, "track01.cdda.wav")
		if err := os.Rename(downloadPath, dest); err != nil {
			failRetryable(fmt.Sprintf("rename single wav: %v", err))
			return
		}
		trackCount = 1
	default:
		failPermanent("unrecognized download format")
		return
	}

	rec, err := s.getter.getRecord(ctx, task.instanceID)
	if err != nil {
		code := status.Code(err)
		if code == codes.NotFound || code == codes.InvalidArgument {
			failPermanent(fmt.Sprintf("recordcollection lookup failed for instance %d: %v", task.instanceID, err))
			return
		}
		failRetryable(fmt.Sprintf("recordcollection lookup failed for instance %d: %v", task.instanceID, err))
		return
	}

	expected := expectedTrackCount(rec)
	if trackCount != expected {
		quarantineDir := filepath.Join(s.dir, ".quarantine", strconv.Itoa(int(task.releaseID)))
		if err := os.MkdirAll(filepath.Dir(quarantineDir), 0755); err != nil {
			failRetryable(fmt.Sprintf("create quarantine parent dir: %v", err))
			return
		}
		if err := os.RemoveAll(quarantineDir); err != nil {
			failRetryable(fmt.Sprintf("clear prior quarantine dir: %v", err))
			return
		}
		if err := os.Rename(stagingDir, quarantineDir); err != nil {
			failRetryable(fmt.Sprintf("move staging dir to quarantine: %v", err))
			return
		}
		msg := fmt.Sprintf("track count mismatch: expected %d from Discogs, extracted %d", expected, trackCount)
		s.CtxLog(ctx, fmt.Sprintf("Download %d quarantined at %s: %s", task.releaseID, quarantineDir, msg))
		dm.setJobState(task.releaseID, &downloadJobState{
			state:          pb.DownloadState_DOWNLOAD_FAILED_PERMANENT,
			trackCount:     int32(trackCount),
			errorMessage:   msg,
			quarantinePath: quarantineDir,
		})
		downloadJobsTotal.WithLabelValues("failed_permanent").Inc()
		downloadJobsTotal.WithLabelValues("quarantined").Inc()
		return
	}

	for i := 1; i <= trackCount; i++ {
		wavPath := filepath.Join(stagingDir, fmt.Sprintf("track%02d.cdda.wav", i))
		flacPath := filepath.Join(stagingDir, fmt.Sprintf("track%02d.cdda.flac", i))
		mp3Path := filepath.Join(stagingDir, fmt.Sprintf("track%02d.cdda.mp3", i))

		if err := dm.encoder.EncodeFlac(ctx, wavPath, flacPath); err != nil {
			failRetryable(err.Error())
			return
		}
		if err := dm.encoder.EncodeMP3(ctx, wavPath, mp3Path); err != nil {
			failRetryable(err.Error())
			return
		}
		if err := verifyNonEmptyFiles(wavPath, flacPath, mp3Path); err != nil {
			failRetryable(err.Error())
			return
		}
	}

	targetDir := filepath.Join(s.dir, strconv.Itoa(int(task.releaseID)))
	exists, complete, existingCount := inspectReleaseDir(targetDir)
	if exists {
		if complete {
			_ = os.RemoveAll(stagingDir)
			dm.setJobState(task.releaseID, &downloadJobState{
				state:      pb.DownloadState_DOWNLOAD_ALREADY_EXISTS,
				trackCount: int32(existingCount),
			})
			return
		}
		failPermanent(fmt.Sprintf("existing release directory %s is empty or incomplete", targetDir))
		return
	}

	quarantineDir := filepath.Join(s.dir, ".quarantine", strconv.Itoa(int(task.releaseID)))
	_ = os.RemoveAll(quarantineDir)

	if err := os.Rename(stagingDir, targetDir); err != nil {
		failRetryable(fmt.Sprintf("commit release dir: %v", err))
		return
	}

	if s.hack != nil {
		s.hack.Lock()
	}
	if err := s.buildConfig(ctx); err != nil {
		s.CtxLog(ctx, fmt.Sprintf("Failed to rebuild config after download %d: %v", task.releaseID, err))
	}
	if s.hack != nil {
		s.hack.Unlock()
	}

	dm.setJobState(task.releaseID, &downloadJobState{
		state:      pb.DownloadState_DOWNLOAD_COMPLETED,
		trackCount: int32(trackCount),
	})
	downloadJobsTotal.WithLabelValues("completed").Inc()
}

func (dm *downloadManager) downloadToFile(ctx context.Context, downloadURL, destPath string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return fmt.Errorf("create download request: %w", err)
	}
	client := dm.httpClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Minute}
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("http download error: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("http download failed with status %d", resp.StatusCode)
	}

	f, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("create staging download file: %w", err)
	}

	_, copyErr := stdlibio.Copy(f, resp.Body)
	closeErr := f.Close()
	if copyErr != nil {
		return fmt.Errorf("stream download body: %w", copyErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close staging download file: %w", closeErr)
	}
	return nil
}

func readMagicHeader(filePath string) ([]byte, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	buf := make([]byte, 12)
	n, err := stdlibio.ReadFull(f, buf)
	if err != nil && err != stdlibio.EOF && err != stdlibio.ErrUnexpectedEOF {
		return nil, err
	}
	return buf[:n], nil
}

func isZipMagic(header []byte) bool {
	return len(header) >= 4 && bytes.Equal(header[:4], []byte{'P', 'K', 0x03, 0x04})
}

func isWavMagic(header []byte) bool {
	return len(header) >= 12 && bytes.Equal(header[:4], []byte("RIFF")) && bytes.Equal(header[8:12], []byte("WAVE"))
}

func verifyNonEmptyFiles(paths ...string) error {
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			return fmt.Errorf("expected output file missing %s: %w", p, err)
		}
		if info.Size() <= 0 {
			return fmt.Errorf("expected output file empty %s", p)
		}
	}
	return nil
}

func expectedTrackCount(rec *pbrc.Record) int {
	if rec == nil || rec.GetRelease() == nil {
		return 0
	}
	trackSet := TrackExtract(rec.GetRelease(), rec.GetMetadata().GetGoalFolder() == 565206)
	count := 0
	for _, track := range trackSet {
		if track.Format == "CD" || track.Format == "CDr" || track.Format == "File" {
			count++
		}
	}
	if count == 0 {
		count = len(trackSet)
	}
	return count
}

type zipWavEntry struct {
	file      *zip.File
	cleanPath string
}

type trackSortKey struct {
	hasKey bool
	disc   int
	track  int
}

func isUnsafeZipPath(name string) bool {
	if strings.HasPrefix(name, "/") || strings.HasPrefix(name, "\\") || filepath.IsAbs(name) {
		return true
	}
	norm := strings.ReplaceAll(name, "\\", "/")
	if len(norm) >= 2 && norm[1] == ':' {
		return true
	}
	for _, seg := range strings.Split(norm, "/") {
		if seg == ".." {
			return true
		}
	}
	return false
}

func isIgnoredZipEntry(cleanPath string) bool {
	for _, seg := range strings.Split(cleanPath, "/") {
		if strings.EqualFold(seg, "__MACOSX") {
			return true
		}
	}
	base := path.Base(cleanPath)
	if strings.HasPrefix(base, "._") || strings.HasPrefix(base, ".") {
		return true
	}
	return !strings.EqualFold(path.Ext(base), ".wav")
}

func extractAndStageZip(zipPath, stagingDir string) (int, error) {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return 0, fmt.Errorf("open zip archive: %w", err)
	}
	defer r.Close()

	var entries []zipWavEntry
	for _, f := range r.File {
		if isUnsafeZipPath(f.Name) {
			return 0, fmt.Errorf("zip-slip path detected: %s", f.Name)
		}
		if f.FileInfo().IsDir() {
			continue
		}
		norm := strings.ReplaceAll(f.Name, "\\", "/")
		cleanPath := path.Clean(norm)
		if isIgnoredZipEntry(cleanPath) {
			continue
		}
		entries = append(entries, zipWavEntry{
			file:      f,
			cleanPath: cleanPath,
		})
	}

	if len(entries) == 0 {
		return 0, fmt.Errorf("no wav files found in zip archive")
	}

	sortZipWavEntries(entries)

	for i, entry := range entries {
		destPath := filepath.Join(stagingDir, fmt.Sprintf("track%02d.cdda.wav", i+1))
		if err := copyZipEntry(entry.file, destPath); err != nil {
			return 0, fmt.Errorf("extract %s to %s: %w", entry.cleanPath, destPath, err)
		}
	}

	return len(entries), nil
}

func copyZipEntry(zf *zip.File, destPath string) error {
	rc, err := zf.Open()
	if err != nil {
		return err
	}
	defer rc.Close()

	out, err := os.Create(destPath)
	if err != nil {
		return err
	}

	_, copyErr := stdlibio.Copy(out, rc)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func sortZipWavEntries(entries []zipWavEntry) {
	type indexedEntry struct {
		entry zipWavEntry
		key   trackSortKey
	}
	items := make([]indexedEntry, len(entries))
	for i, e := range entries {
		items[i] = indexedEntry{
			entry: e,
			key:   parseZipTrackKey(e.cleanPath),
		}
	}

	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i], items[j]
		if a.key.hasKey != b.key.hasKey {
			return a.key.hasKey
		}
		if a.key.hasKey && b.key.hasKey {
			if a.key.disc != b.key.disc {
				return a.key.disc < b.key.disc
			}
			if a.key.track != b.key.track {
				return a.key.track < b.key.track
			}
		}
		return naturalCompare(a.entry.cleanPath, b.entry.cleanPath) < 0
	})

	for i, item := range items {
		entries[i] = item.entry
	}
}

func parseZipTrackKey(cleanPath string) trackSortKey {
	dirPart, fileName := path.Split(cleanPath)
	dirDisc := 1
	if dirPart != "" {
		dirSegs := strings.Split(strings.Trim(dirPart, "/"), "/")
		for i := len(dirSegs) - 1; i >= 0; i-- {
			if m := dirDiscPattern.FindStringSubmatch(dirSegs[i]); m != nil {
				if d, err := strconv.Atoi(m[1]); err == nil && d > 0 && d < 100 {
					dirDisc = d
					break
				}
			}
		}
	}

	stem := strings.TrimSuffix(fileName, path.Ext(fileName))
	rawSegs := strings.Split(stem, " - ")
	segs := make([]string, len(rawSegs))
	for i, s := range rawSegs {
		segs[i] = strings.TrimSpace(s)
	}

	if len(segs) >= 2 {
		if d, t, ok := parseDiscTrackPair(segs[0], discTrackExactPattern); ok {
			return trackSortKey{hasKey: true, disc: d, track: t}
		}
		if d, t, ok := parseDiscTrackPair(segs[0], dNtMPattern); ok {
			return trackSortKey{hasKey: true, disc: d, track: t}
		}

		lastSeg := segs[len(segs)-1]
		if m := discSegPattern.FindStringSubmatch(segs[len(segs)-2]); m != nil {
			if d, err := strconv.Atoi(m[1]); err == nil && d > 0 && d < 100 {
				if tm := leadingTrackEndPat.FindStringSubmatch(lastSeg); tm != nil {
					if t, err := strconv.Atoi(tm[1]); err == nil && t > 0 && t < 1000 {
						return trackSortKey{hasKey: true, disc: d, track: t}
					}
				}
			}
		}
		if len(segs) >= 3 {
			if m := discSegPattern.FindStringSubmatch(segs[len(segs)-3]); m != nil {
				if d, err := strconv.Atoi(m[1]); err == nil && d > 0 && d < 100 {
					if pureShortDigitsPat.MatchString(segs[len(segs)-2]) {
						if t, err := strconv.Atoi(segs[len(segs)-2]); err == nil && t > 0 && t < 1000 {
							return trackSortKey{hasKey: true, disc: d, track: t}
						}
					}
				}
			}
		}

		if pureShortDigitsPat.MatchString(segs[0]) && (len(segs) == 2 || !leadingTrackSepPat.MatchString(lastSeg)) {
			if t, err := strconv.Atoi(segs[0]); err == nil && t > 0 && t < 1000 {
				return trackSortKey{hasKey: true, disc: dirDisc, track: t}
			}
		}

		if d, t, ok := parseDiscTrackPair(lastSeg, leadingDiscTrackPat); ok {
			return trackSortKey{hasKey: true, disc: d, track: t}
		}
		if m := leadingTrackSepPat.FindStringSubmatch(lastSeg); m != nil {
			if t, err := strconv.Atoi(m[1]); err == nil && t > 0 && t < 1000 {
				return trackSortKey{hasKey: true, disc: dirDisc, track: t}
			}
		}
	}

	if d, t, ok := parseDiscTrackPair(stem, dNtMPattern); ok {
		return trackSortKey{hasKey: true, disc: d, track: t}
	}
	if d, t, ok := parseDiscTrackPair(stem, leadingDiscTrackPat); ok {
		return trackSortKey{hasKey: true, disc: d, track: t}
	}
	if m := trackWordPattern.FindStringSubmatch(stem); m != nil {
		if t, err := strconv.Atoi(m[1]); err == nil && t > 0 && t < 1000 {
			return trackSortKey{hasKey: true, disc: dirDisc, track: t}
		}
	}
	if m := leadingTrackEndPat.FindStringSubmatch(stem); m != nil {
		if t, err := strconv.Atoi(m[1]); err == nil && t > 0 && t < 1000 {
			return trackSortKey{hasKey: true, disc: dirDisc, track: t}
		}
	}

	return trackSortKey{}
}

func parseDiscTrackPair(s string, re *regexp.Regexp) (int, int, bool) {
	m := re.FindStringSubmatch(s)
	if m == nil {
		return 0, 0, false
	}
	d, err1 := strconv.Atoi(m[1])
	t, err2 := strconv.Atoi(m[2])
	if err1 != nil || err2 != nil || d <= 0 || d >= 100 || t <= 0 || t >= 1000 {
		return 0, 0, false
	}
	return d, t, true
}

func naturalCompare(a, b string) int {
	ra := []rune(a)
	rb := []rune(b)
	ia, ib := 0, 0
	for ia < len(ra) && ib < len(rb) {
		da := unicode.IsDigit(ra[ia])
		db := unicode.IsDigit(rb[ib])
		if da && db {
			ja := ia
			for ja < len(ra) && unicode.IsDigit(ra[ja]) {
				ja++
			}
			jb := ib
			for jb < len(rb) && unicode.IsDigit(rb[jb]) {
				jb++
			}
			numA := strings.TrimLeft(string(ra[ia:ja]), "0")
			numB := strings.TrimLeft(string(rb[ib:jb]), "0")
			if len(numA) != len(numB) {
				if len(numA) < len(numB) {
					return -1
				}
				return 1
			}
			if numA != numB {
				if numA < numB {
					return -1
				}
				return 1
			}
			if (ja - ia) != (jb - ib) {
				if (ja - ia) < (jb - ib) {
					return -1
				}
				return 1
			}
			ia, ib = ja, jb
			continue
		}

		ca := unicode.ToLower(ra[ia])
		cb := unicode.ToLower(rb[ib])
		if ca != cb {
			if ca < cb {
				return -1
			}
			return 1
		}
		ia++
		ib++
	}

	if len(ra) != len(rb) {
		if len(ra) < len(rb) {
			return -1
		}
		return 1
	}
	return strings.Compare(a, b)
}
