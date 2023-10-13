package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/context"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pbcdp "github.com/brotherlogic/cdprocessor/proto"
	pbgd "github.com/brotherlogic/godiscogs/proto"
	pbrc "github.com/brotherlogic/recordcollection/proto"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	needsRip = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "cdprocessor_rips",
		Help: "The number of records needing a rip",
	})

	ripped24Hours = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "cdprocessor_rips_24",
		Help: "The number of records needing a rip",
	})
)

func (s *Server) findMissing(ctx context.Context) (*pbcdp.Rip, error) {
	localRip, err := s.GetRipped(ctx, &pbcdp.GetRippedRequest{})
	ripped, err := s.master.GetRipped(ctx, &pbcdp.GetRippedRequest{})
	if err != nil {
		return nil, err
	}

	for _, rip := range ripped.GetRipped() {
		found := false
		for _, local := range localRip.GetRipped() {
			if local.Id == rip.Id {
				found = true
			}
		}

		if !found {
			return rip, nil
		}
	}

	return nil, nil
}

// verifies the status of the ripped cd
func (s *Server) verify(ctx context.Context, ID int32) error {
	record, err := s.getter.getRecord(ctx, ID)
	if err != nil {
		return err
	}

	return s.verifyRecord(ctx, record)
}

func (s *Server) verifyRecord(ctx context.Context, record *pbrc.Record) error {

	t := time.Now()
	files, err := ioutil.ReadDir(record.GetMetadata().CdPath)
	count := 0
	trackSet := TrackExtract(record.GetRelease(), record.GetMetadata().GetGoalFolder() == 565206)
	for _, track := range trackSet {
		if track.Format == "CD" || track.Format == "CDr" || track.Format == "File" {
			count++
		}
	}
	s.CtxLog(ctx, fmt.Sprintf("Read dir and built trackset in %v", time.Now().Sub(t)))

	if count == 0 {
		count = len(trackSet)
	}

	s.CtxLog(ctx, fmt.Sprintf("Processing (%v): %v / %v", record.GetRelease().GetInstanceId(), len(files), count))
	config, err2 := s.load(ctx)
	if err2 != nil {
		return err2
	}
	s.adjustAlert(ctx, config, record, len(files) != count || err != nil)
	time.Sleep(time.Second * 2)
	s.CtxLog(ctx, fmt.Sprintf("Found %v files for %v, expected to see %v", len(files), record.GetRelease().GetId(), count))
	if len(files) != count || err != nil {
		files, err = ioutil.ReadDir(record.GetMetadata().CdPath)
		err = s.buildConfig(ctx)
		if err != nil {
			s.CtxLog(ctx, fmt.Sprintf("Bad config building: %v", err))
		}
		t = time.Now()
		err = s.convertToMP3(ctx, record.GetRelease().GetId())
		s.CtxLog(ctx, fmt.Sprintf("MP3 (%v) conversion in %v", record.GetRelease().GetId(), time.Now().Sub(t)))
		if err != nil {
			s.CtxLog(ctx, fmt.Sprintf("Bad ripping: %v", err))
		}
		t = time.Now()
		err = s.convertToFlac(ctx, record.GetRelease().GetId())
		s.CtxLog(ctx, fmt.Sprintf("Flac (%v) conversion in %v", record.GetRelease().GetId(), time.Now().Sub(t)))
		if err != nil {
			s.CtxLog(ctx, fmt.Sprintf("Bad flaccing: %v", err))
		}

		if len(files) != count || err != nil {
			s.makeLinks(ctx, record.GetRelease().GetInstanceId(), true)

			if len(files) > count {
				fstr := ""
				for _, file := range files {
					fstr += file.Name() + " ;"
				}
				s.CtxLog(ctx, fmt.Sprintf("%v (Expected %v)", fstr, count))
			}
			return status.Error(codes.DataLoss, fmt.Sprintf("Error reading %v/%v files for %v: (%v)", len(files), count, record.GetRelease().GetId(), err))
		}
	}

	return nil
}

func expand(v string) string {
	if len(v) == 1 {
		return "0" + v
	}
	return v
}

func computeArtist(rec *pbgd.Release) string {
	str := ""
	for _, artist := range rec.GetArtists() {
		str += fmt.Sprintf("%v, ", artist.Name)
	}

	return str[:len(str)-2]
}

func (s *Server) makeLinks(ctx context.Context, ID int32, force bool) error {
	record, err := s.getter.getRecord(ctx, ID)
	if err != nil {
		if status.Convert(err).Code() == codes.OutOfRange {
			return nil
		}
		return err
	}

	// Skip records which aren't here yet
	if record.GetMetadata().GetDateArrived() == 0 && time.Since(time.Unix(record.GetMetadata().GetDateAdded(), 0)) < time.Hour*24*365 {
		s.CtxLog(ctx, "Skipping because it's not arrived yet")
		return nil
	}

	// Skip records which aren't in the listening pile
	if record.GetRelease().GetFolderId() != 812802 {
		s.CtxLog(ctx, "Skipping because it's not in the listening pile")
		return nil
	}

	// Skip SOLD_ARCHIVE records
	if !force && record.GetMetadata().GetCategory() == pbrc.ReleaseMetadata_SOLD_ARCHIVE {
		s.CtxLog(ctx, "Skipping because it's SOLD_ARCHIVE")
		return nil
	}

	// Skip records which aren't release yet
	val, err := time.Parse("2006-01-02", record.GetRelease().GetReleased())
	s.CtxLog(ctx, fmt.Sprintf("Got %v, %v", val, err))
	if err == nil && val.After(time.Now()) {
		s.CtxLog(ctx, "Skipping because it's UNRELEASED")
		return nil
	}

	// Skip boxed records
	if record.GetMetadata().GetBoxState() != pbrc.ReleaseMetadata_BOX_UNKNOWN &&
		record.GetMetadata().GetBoxState() != pbrc.ReleaseMetadata_OUT_OF_BOX {
		return nil
	}

	//Don't do anythng if we're in limbo folder
	if record.GetRelease().GetFolderId() == 3380098 && record.GetMetadata().GetMoveFolder() == 0 {
		return nil
	}

	config, err := s.load(ctx)
	if err != nil {
		return err
	}
	if time.Since(time.Unix(config.GetLastProcessTime()[record.GetRelease().GetInstanceId()], 0)) > time.Hour*24*7 {
		if config.GetLastProcessTime()[record.GetRelease().GetInstanceId()] > 0 {
			s.CtxLog(ctx, fmt.Sprintf("Setting force since %v", time.Since(time.Unix(config.GetLastProcessTime()[record.GetRelease().GetInstanceId()], 0))))
			force = true
		}
	}
	err = s.runLinks(ctx, ID, force, record)
	s.CtxLog(ctx, fmt.Sprintf("Error on run links: %v", err))

	if err != nil {
		return err
	}
	config.LastProcessTime[record.GetRelease().GetInstanceId()] = time.Now().Unix()

	if record.GetRelease().GetFolderId() != 812802 {
		return s.save(ctx, config)
	}

	if err != nil {
		return err
	}

	s.CtxLog(ctx, fmt.Sprintf("Adjust force and saving %v", time.Since(time.Unix(config.GetLastProcessTime()[record.GetRelease().GetInstanceId()], 0))))

	return s.save(ctx, config)
}

func (s *Server) runLinks(ctx context.Context, ID int32, force bool, record *pbrc.Record) error {
	s.CtxLog(ctx, fmt.Sprintf("Runnign linkes %v -> %v", ID, force))
	// Don't process digital CDs
	if record.GetMetadata().GetGoalFolder() == 268147 ||
		record.GetMetadata().GetGoalFolder() == 1433217 {
		s.CtxLog(ctx, fmt.Sprintf("Not processing digital CD (%v)", ID))
		return nil
	}

	match := false
	if record.GetMetadata().GetGoalFolder() != 242018 &&
		record.GetMetadata().GetGoalFolder() != 1782105 &&
		record.GetMetadata().GetGoalFolder() != 288751 &&
		record.GetMetadata().GetGoalFolder() != 2274270 &&
		record.GetMetadata().GetGoalFolder() != 565206 {
		// Not a cd or a bandcamp or cd boxset
		for _, format := range record.GetRelease().GetFormats() {
			if format.GetName() == "File" || format.GetName() == "CD" || format.GetName() == "CDr" || format.GetName() == "Cassette" || format.GetName() == "Memory Stick" {
				s.CtxLog(ctx, fmt.Sprintf("Matched %v on the format: %v", ID, format))
				match = true
			}
		}
	} else {
		s.CtxLog(ctx, fmt.Sprintf("Matched %v since it has the right goal folder: %v ( => %v)", ID, record.GetMetadata().GetGoalFolder(), force))
		match = true
	}

	// This is not a CD we can process
	if !match {
		s.CtxLog(ctx, fmt.Sprintf("Don't think %v is a rippable format", record.GetRelease().GetInstanceId()))
		return nil
	}

	if force || len(record.GetMetadata().CdPath) == 0 {

		if len(record.GetMetadata().CdPath) == 0 || strings.Contains(record.GetMetadata().CdPath, "mp3") {
			t := time.Now()
			s.getter.updateRecord(ctx, record.GetRelease().GetInstanceId(), fmt.Sprintf("%v%v", s.flacdir, record.GetRelease().Id), "")
			s.CtxLog(ctx, fmt.Sprintf("Updated record in %v", time.Now().Sub(t)))
		}
		os.MkdirAll(fmt.Sprintf("%v%v", s.mp3dir, record.GetRelease().Id), os.ModePerm)
		os.MkdirAll(fmt.Sprintf("%v%v", s.flacdir, record.GetRelease().Id), os.ModePerm)

		t := time.Now()
		trackSet := TrackExtract(record.GetRelease(), record.GetMetadata().GetGoalFolder() == 565206)
		s.CtxLog(ctx, fmt.Sprintf("Extracted %v tracks in %v", len(trackSet), time.Now().Sub(t)))
		noTracks := false
		for _, track := range trackSet {
			if track.Format == "CD" || track.Format == "CDr" || track.Format == "File" {
				noTracks = true
			}
		}
		for _, track := range trackSet {
			if track.Format == "CD" || track.Format == "CDr" || track.Format == "File" || !noTracks {
				err := s.buildLink(ctx, track, record)
				if err != nil {
					return err
				}
			} else {
				s.CtxLog(ctx, fmt.Sprintf("Skipping %v because %v", track.Position, track.Format))
			}
		}

		return s.getter.updateRecord(ctx, record.GetRelease().GetInstanceId(), fmt.Sprintf("%v%v", s.flacdir, record.GetRelease().Id), "")
	}

	return s.verifyRecord(ctx, record)
}

func prepend(val string) string {
	if len(val) == 1 {
		return fmt.Sprintf("0%v", val)
	} else {
		return fmt.Sprintf("%v", val)
	}
}

func (s *Server) buildLink(ctx context.Context, track *TrackSet, record *pbrc.Record) error {
	s.CtxLog(ctx, fmt.Sprintf("Building links: %v", track))
	// Verify that the track exists
	adder := ""
	if record.GetRelease().FormatQuantity > 1 {
		adder = fmt.Sprintf("_%v", track.Disk)
	}

	trackPath := fmt.Sprintf("%v%v%v/track%v.cdda.flac", s.dir, record.GetRelease().Id, adder, expand(track.Position))

	if !s.fileExists(trackPath) {
		s.CtxLog(ctx, fmt.Sprintf("Track %v does not exist", trackPath))
		s.verifyRecord(ctx, record)
		return fmt.Errorf("Missing Track: %v (from %+v -> %v+)", trackPath, track, track.tracks[0])
	}

	if len(record.GetRelease().GetImages()) > 0 {
		s.ripper.runCommand(ctx, []string{"wget", record.GetRelease().GetImages()[0].GetUri(), "-O", fmt.Sprintf("%v%v%v/cover.jpg", s.dir, record.GetRelease().Id, adder)}, false)
	}

	title := GetTitle(track)
	oldmp3 := fmt.Sprintf("%v%v%v/track%v.cdda.mp3", s.dir, record.GetRelease().Id, adder, expand(track.Position))
	s.ripper.runCommand(ctx, []string{"ln", "-s", fmt.Sprintf("%v%v%v/track%v.cdda.mp3", s.dir, record.GetRelease().Id, adder, expand(track.Position)), fmt.Sprintf("%v%v/track%v-%v.cdda.mp3", s.mp3dir, record.GetRelease().Id, track.Disk, expand(track.Position))}, false)
	s.ripper.runCommand(ctx, []string{"mp3info", "-n", fmt.Sprintf("%v", track.Position), fmt.Sprintf("%v%v/track%v-%v.cdda.mp3", s.mp3dir, record.GetRelease().Id, track.Disk, expand(track.Position))}, false)
	s.ripper.runCommand(ctx, []string{"mp3info", "-t", fmt.Sprintf("%v", title), fmt.Sprintf("%v%v/track%v-%v.cdda.mp3", s.mp3dir, record.GetRelease().Id, track.Disk, expand(track.Position))}, false)
	s.ripper.runCommand(ctx, []string{"mp3info", "-l", fmt.Sprintf("%v", record.GetRelease().Title), fmt.Sprintf("%v%v/track%v-%v.cdda.mp3", s.mp3dir, record.GetRelease().Id, track.Disk, expand(track.Position))}, false)
	s.ripper.runCommand(ctx, []string{"mp3info", "-a", computeArtist(record.GetRelease()), fmt.Sprintf("%v%v/track%v-%v.cdda.mp3", s.mp3dir, record.GetRelease().Id, track.Disk, expand(track.Position))}, false)

	s.ripper.runCommand(ctx, []string{"eyeD3", fmt.Sprintf("--text-frame=TPOS:\"%v/%v\"", track.Disk, record.GetRelease().FormatQuantity), fmt.Sprintf("%v%v/track%v-%v.cdda.mp3", s.mp3dir, record.GetRelease().Id, track.Disk, expand(track.Position))}, false)
	s.ripper.runCommand(ctx, []string{"eyeD3", "--to-v2.4", oldmp3}, false)
	s.ripper.runCommand(ctx, []string{"eyeD3", "--add-image", fmt.Sprintf("%v%v%v/cover.jpg:FRONT_COVER", s.dir, record.GetRelease().Id, adder), oldmp3}, false)

	oldfile := fmt.Sprintf("%v%v%v/track%v.cdda.flac", s.dir, record.GetRelease().Id, adder, expand(track.Position))
	//newfile := fmt.Sprintf("%v%v/%v-%v.cdda.flac", s.flacdir, record.Id, track.Disk, expand(track.Position))
	s.ripper.runCommand(ctx, []string{"ln", "-s", fmt.Sprintf("%v%v%v/track%v.cdda.flac", s.dir, record.GetRelease().Id, adder, expand(track.Position)), fmt.Sprintf("%v%v/%v-%v.cdda.flac", s.flacdir, record.GetRelease().Id, track.Disk, expand(track.Position))}, false)
	s.ripper.runCommand(ctx, []string{"metaflac", "--remove-tag=artist", fmt.Sprintf("--set-tag=artist=%v", computeArtist(record.GetRelease())), oldfile}, true)
	s.ripper.runCommand(ctx, []string{"metaflac", fmt.Sprintf("--set-tag=tracknumber=%v", track.Position), oldfile}, true)
	s.ripper.runCommand(ctx, []string{"metaflac", fmt.Sprintf("--set-tag=discnumber=%v", prepend(track.Disk)), oldfile}, true)
	s.ripper.runCommand(ctx, []string{"metaflac", "--remove-tag=title", fmt.Sprintf("--set-tag=title=%v", title), oldfile}, true)
	s.ripper.runCommand(ctx, []string{"metaflac", "--remove-tag=album", fmt.Sprintf("--set-tag=album=%v", record.GetRelease().Title), oldfile}, true)
	if len(record.GetRelease().GetImages()) > 0 {
		s.ripper.runCommand(ctx, []string{"metaflac", fmt.Sprintf("--import-picture-from=%v%v%v/cover.jpg", s.dir, record.GetRelease().Id, adder), oldfile}, true)
	}
	//s.ripper.runCommand(ctx, []string{"metaflac", fmt.Sprintf("--set-tag=album=\"%v\"", record.Title), fmt.Sprintf("%v%v/%v-%v.cdda.flac", s.flacdir, record.Id, track.Disk, expand(track.Position))})

	return nil
}

func (s *Server) convertToMP3(ctx context.Context, id int32) error {
	found := false
	for _, rip := range s.rips {
		for _, t := range rip.Tracks {
			if rip.Id == id {
				found = true

				if len(t.WavPath) > 0 && len(t.Mp3Path) == 0 && strings.Contains(t.WavPath, "track") {
					s.CtxLog(ctx, fmt.Sprintf("Missing MP3: %v", s.dir+t.WavPath))
					s.ripCount++
					s.ripper.ripToMp3(ctx, s.dir+t.WavPath, s.dir+t.WavPath[0:len(t.WavPath)-3]+"mp3")
					s.buildConfig(ctx)
					return nil
				}
			}
		}
	}
	if !found {
		return fmt.Errorf("Unable to locate rip for %v", id)
	}

	return nil
}

func (s *Server) convertToFlac(ctx context.Context, id int32) error {
	time.Sleep(time.Second * 2)
	found := false
	for _, rip := range s.rips {
		if rip.Id == id {
			found = true

			for _, t := range rip.Tracks {
				if len(t.WavPath) > 0 && len(t.FlacPath) == 0 && strings.Contains(t.WavPath, "track") {
					s.CtxLog(ctx, fmt.Sprintf("Missing FLAC: %v", s.dir+t.WavPath))
					s.flacCount++
					s.ripper.ripToFlac(ctx, s.dir+t.WavPath, s.dir+t.WavPath[0:len(t.WavPath)-3]+"flac")
					s.buildConfig(ctx)
					return nil
				}
			}
		}
	}

	if !found {
		s.CtxLog(ctx, fmt.Sprintf("Did not find any flacs for %v", id))
	}
	return nil
}

func (s *Server) buildConfig(ctx context.Context) error {
	files, err := s.io.readDir()
	if err != nil {
		return err
	}

	rips := []*pbcdp.Rip{}
	for _, f := range files {
		if f.IsDir() && f.Name() != "lost+found" {
			name := f.Name()
			id, disk, err := s.io.convert(name)
			if err != nil {
				s.CtxLog(ctx, fmt.Sprintf("Unable to convert %v -> %v", name, err))
				return err
			}

			trackFiles, _ := s.io.readSubdir(f.Name())
			tracks := []*pbcdp.Track{}
			for _, tf := range trackFiles {
				if !tf.IsDir() && strings.Contains(tf.Name(), "track") {
					trackNumber, _ := strconv.ParseInt(tf.Name()[5:7], 10, 32)

					var foundTrack *pbcdp.Track
					for _, t := range tracks {
						if t.TrackNumber == int32(trackNumber) {
							foundTrack = t
						}
					}
					if foundTrack == nil {
						foundTrack = &pbcdp.Track{TrackNumber: int32(trackNumber), Disk: disk}
						tracks = append(tracks, foundTrack)
					}

					if strings.HasSuffix(tf.Name(), "wav") {
						foundTrack.WavPath = f.Name() + "/" + tf.Name()
					} else if strings.HasSuffix(tf.Name(), "mp3") {
						foundTrack.Mp3Path = f.Name() + "/" + tf.Name()
					} else if strings.HasSuffix(tf.Name(), "flac") {
						foundTrack.FlacPath = f.Name() + "/" + tf.Name()
					}
				}
			}

			rips = append(rips, &pbcdp.Rip{Id: id, Path: f.Name(), Tracks: tracks})
		}
	}

	s.rips = rips
	return nil
}

func (s *Server) adjustAlert(ctx context.Context, config *pbcdp.Config, r *pbrc.Record, needs bool) error {
	number, alreadySeen := config.GetIssueMapping()[r.GetRelease().GetId()]
	s.CtxLog(ctx, fmt.Sprintf("ALERT %v and %v for %v from %v (%v)", number, alreadySeen, r.GetRelease().GetId(), config.GetIssueMapping(), needs))
	if needs && !alreadySeen {
		issue, err := s.ImmediateIssue(ctx, fmt.Sprintf("CD Rip Need for %v", r.GetRelease().GetTitle()), fmt.Sprintf("https://www.discogs.com/madeup/release/%v", r.GetRelease().GetId()), false, true)
		if err != nil {
			return err
		}
		s.CtxLog(ctx, fmt.Sprintf("Adding issue %v -> %v", r.GetRelease(), issue))
		config.IssueMapping[r.GetRelease().GetId()] = issue.GetNumber()

		return s.save(ctx, config)
	}

	if alreadySeen && !needs {
		err := s.DeleteIssue(ctx, number)
		if err != nil && status.Convert(err).Code() != codes.NotFound {
			return err
		}
		delete(config.IssueMapping, r.GetRelease().GetId())

		// Update rip time
		config.GetLastRipTime()[r.GetRelease().GetId()] = time.Now().Unix()
		s.updateMetrics(config)
		return s.save(ctx, config)
	}

	return nil
}

func (s *Server) adjustExisting(ctx context.Context) error {
	t := time.Now()
	m, _ := s.GetRipped(ctx, &pbcdp.GetRippedRequest{})

	for _, r := range m.Ripped {
		rec, err := s.getter.getRecord(ctx, r.Id)
		if err != nil {
			e, ok := status.FromError(err)
			if !ok || e.Code() == codes.InvalidArgument {
				s.RaiseIssue("Nil Record Fail", fmt.Sprintf("Nil record?: %v -> %v", r.Id, err))
			}
		}

		s.getter.updateRecord(ctx, rec.GetRelease().GetInstanceId(), "", r.Path)
		s.adjust++
		break
	}

	s.lastRunTime = time.Now().Sub(t)

	return nil
}

func (s *Server) logMissing(ctx context.Context) error {
	m, _ := s.GetMissing(context.Background(), &pbcdp.GetMissingRequest{})

	needsRip.Set(float64(len(m.Missing)))
	if len(m.Missing) > 0 {
		s.RaiseIssue("Rip CD", fmt.Sprintf("%v [%v]", m.Missing[0].GetRelease().Title, m.Missing[0].GetRelease().Id))
	}

	return nil
}
