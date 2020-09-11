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
	pbgd "github.com/brotherlogic/godiscogs"
	pbrc "github.com/brotherlogic/recordcollection/proto"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	needsRip = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "cdprocessor_rips",
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

	if len(record.GetMetadata().CdPath) == 0 {
		s.RaiseIssue("Missing MP3", fmt.Sprintf("%v [%v] is missing the CD Path: %v", record.GetRelease().Title, record.GetRelease().Id, record.GetMetadata()))
	}

	t := time.Now()
	files, err := ioutil.ReadDir(record.GetMetadata().CdPath)
	count := 0
	trackSet := TrackExtract(record.GetRelease())
	for _, track := range trackSet {
		if track.Format == "CD" || track.Format == "CDr" || track.Format == "File" {
			count++
		}
	}
	s.Log(fmt.Sprintf("Read dir and built trackset in %v", time.Now().Sub(t)))

	if count == 0 {
		count = len(trackSet)
	}

	s.Log(fmt.Sprintf("Processing (%v): %v / %v", record.GetRelease().GetInstanceId(), len(files), count))
	time.Sleep(time.Second * 2)
	if len(files) != count || err != nil {
		files, err = ioutil.ReadDir(record.GetMetadata().CdPath)
		t := time.Now()
		err = s.buildConfig(ctx)
		s.Log(fmt.Sprintf("Built config in %v", time.Now().Sub(t)))
		if err != nil {
			s.Log(fmt.Sprintf("Bad config building: %v", err))
		}
		t = time.Now()
		err = s.convertToMP3(ctx, record.GetRelease().GetId())
		s.Log(fmt.Sprintf("MP3 conversion in %v", time.Now().Sub(t)))
		if err != nil {
			s.Log(fmt.Sprintf("Bad ripping: %v", err))
		}
		t = time.Now()
		err = s.convertToFlac(ctx, record.GetRelease().GetId())
		s.Log(fmt.Sprintf("Flac conversion in %v", time.Now().Sub(t)))
		if err != nil {
			s.Log(fmt.Sprintf("Bad flaccing: %v", err))
		}

		if len(files) != count || err != nil {
			s.RaiseIssue(fmt.Sprintf("CD Rip Needd for %v", record.GetRelease().GetTitle()), fmt.Sprintf("https://www.discogs.com/madeup/release/%v", record.GetRelease().GetId()))
			s.makeLinks(ctx, record.GetRelease().GetInstanceId(), true)
			return status.Error(codes.DataLoss, fmt.Sprintf("Error reading %v/%v files (%v)", len(files), count, err))
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
	t := time.Now()
	record, err := s.getter.getRecord(ctx, ID)
	if err != nil {
		return err
	}
	s.Log(fmt.Sprintf("Got record in %v", time.Now().Sub(t)))

	// Don't process digital CDs
	if record.GetMetadata().GetGoalFolder() == 268147 ||
		record.GetMetadata().GetGoalFolder() == 1433217 {
		s.Log(fmt.Sprintf("Not processing digital CD (%v)", ID))
		return nil
	}

	match := false
	if record.GetMetadata().GetGoalFolder() != 242018 && record.GetMetadata().GetGoalFolder() != 1782105 && record.GetMetadata().GetGoalFolder() != 288751 {
		// Not a cd or a bandcamp or cd boxset
		for _, format := range record.GetRelease().GetFormats() {
			if format.GetName() == "File" || format.GetName() == "CD" || format.GetName() == "Cdr" {
				match = true
			}
		}
	} else {
		match = true
	}

	// This is not a CD we can process
	if !match {
		s.Log(fmt.Sprintf("Don't think %v is a CD", record.GetRelease().GetInstanceId()))
		return nil
	}

	if force || len(record.GetMetadata().CdPath) == 0 {

		if len(record.GetMetadata().CdPath) == 0 {
			t := time.Now()
			s.getter.updateRecord(ctx, record.GetRelease().GetInstanceId(), fmt.Sprintf("%v%v", s.mp3dir, record.GetRelease().Id), "")
			s.Log(fmt.Sprintf("Updated record in %v", time.Now().Sub(t)))
		}
		os.MkdirAll(fmt.Sprintf("%v%v", s.mp3dir, record.GetRelease().Id), os.ModePerm)
		os.MkdirAll(fmt.Sprintf("%v%v", s.flacdir, record.GetRelease().Id), os.ModePerm)

		t := time.Now()
		trackSet := TrackExtract(record.GetRelease())
		s.Log(fmt.Sprintf("Extracted tracks in %v", time.Now().Sub(t)))
		noTracks := false
		for _, track := range trackSet {
			if track.Format == "CD" || track.Format == "CDr" || track.Format == "File" {
				noTracks = true
			}
		}
		for _, track := range trackSet {
			if track.Format == "CD" || track.Format == "CDr" || track.Format == "File" || !noTracks {
				err := s.buildLink(ctx, track, record.GetRelease())
				if err != nil {
					return err
				}
			}
		}

		return s.getter.updateRecord(ctx, record.GetRelease().GetInstanceId(), fmt.Sprintf("%v%v", s.mp3dir, record.GetRelease().Id), "")
	}

	return s.verifyRecord(ctx, record)
}

func (s *Server) buildLink(ctx context.Context, track *TrackSet, record *pbgd.Release) error {
	// Verify that the track exists
	adder := ""
	if record.FormatQuantity > 1 {
		adder = fmt.Sprintf("_%v", track.Disk)
	}

	trackPath := fmt.Sprintf("%v%v%v/track%v.cdda.mp3", s.dir, record.Id, adder, expand(track.Position))

	if !s.fileExists(trackPath) {
		return fmt.Errorf("Missing Track: %v (from %+v -> %v+)", trackPath, track, track.tracks[0])
	}

	title := GetTitle(track)
	s.ripper.runCommand(ctx, []string{"ln", "-s", fmt.Sprintf("%v%v%v/track%v.cdda.mp3", s.dir, record.Id, adder, expand(track.Position)), fmt.Sprintf("%v%v/track%v-%v.cdda.mp3", s.mp3dir, record.Id, track.Disk, expand(track.Position))})
	s.ripper.runCommand(ctx, []string{"mp3info", "-n", fmt.Sprintf("%v", track.Position), fmt.Sprintf("%v%v/track%v-%v.cdda.mp3", s.mp3dir, record.Id, track.Disk, expand(track.Position))})
	s.ripper.runCommand(ctx, []string{"mp3info", "-t", fmt.Sprintf("%v", title), fmt.Sprintf("%v%v/track%v-%v.cdda.mp3", s.mp3dir, record.Id, track.Disk, expand(track.Position))})
	s.ripper.runCommand(ctx, []string{"mp3info", "-l", fmt.Sprintf("%v", record.Title), fmt.Sprintf("%v%v/track%v-%v.cdda.mp3", s.mp3dir, record.Id, track.Disk, expand(track.Position))})
	s.ripper.runCommand(ctx, []string{"mp3info", "-a", computeArtist(record), fmt.Sprintf("%v%v/track%v-%v.cdda.mp3", s.mp3dir, record.Id, track.Disk, expand(track.Position))})
	s.ripper.runCommand(ctx, []string{"eyeD3", fmt.Sprintf("--set-text-frame=TPOS:\"%v/%v\"", track.Disk, record.FormatQuantity), fmt.Sprintf("%v%v/track%v-%v.cdda.mp3", s.mp3dir, record.Id, track.Disk, expand(track.Position))})

	s.ripper.runCommand(ctx, []string{"ln", "-s", fmt.Sprintf("%v%v%v/track%v.cdda.flac", s.dir, record.Id, adder, expand(track.Position)), fmt.Sprintf("%v%v/%v-%v.cdda.flac", s.flacdir, record.Id, track.Disk, expand(track.Position))})
	s.ripper.runCommand(ctx, []string{"metaflac", fmt.Sprintf("--set-tag=artist=%v", computeArtist(record)), fmt.Sprintf("%v%v/%v-%v.cdda.flac", s.flacdir, record.Id, track.Disk, expand(track.Position))})
	s.ripper.runCommand(ctx, []string{"metaflac", fmt.Sprintf("--set-tag=tracknumber=%v", track.Position), fmt.Sprintf("%v%v/%v-%v.cdda.flac", s.flacdir, record.Id, track.Disk, expand(track.Position))})
	s.ripper.runCommand(ctx, []string{"metaflac", fmt.Sprintf("--set-tag=discnumber=%v", track.Disk), fmt.Sprintf("%v%v/%v-%v.cdda.flac", s.flacdir, record.Id, track.Disk, expand(track.Position))})
	s.ripper.runCommand(ctx, []string{"metaflac", fmt.Sprintf("--set-tag=title=%v", title), fmt.Sprintf("%v%v/%v-%v.cdda.flac", s.flacdir, record.Id, track.Disk, expand(track.Position))})
	s.ripper.runCommand(ctx, []string{"metaflac", fmt.Sprintf("--set-tag=album=%v", record.Title), fmt.Sprintf("%v%v/%v-%v.cdda.flac", s.flacdir, record.Id, track.Disk, expand(track.Position))})
	//s.ripper.runCommand(ctx, []string{"metaflac", fmt.Sprintf("--set-tag=album=\"%v\"", record.Title), fmt.Sprintf("%v%v/%v-%v.cdda.flac", s.flacdir, record.Id, track.Disk, expand(track.Position))})

	return nil
}

func (s *Server) convertToMP3(ctx context.Context, id int32) error {
	found := false
	for _, rip := range s.rips {
		for _, t := range rip.Tracks {
			if rip.Id == id {
				found = true

				if len(t.WavPath) > 0 && len(t.Mp3Path) == 0 {
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
				if len(t.WavPath) > 0 && len(t.FlacPath) == 0 {
					s.flacCount++
					s.ripper.ripToFlac(ctx, s.dir+t.WavPath, s.dir+t.WavPath[0:len(t.WavPath)-3]+"flac")
					s.buildConfig(ctx)
					return nil
				}
			}
		}
	}

	if !found {
		s.Log(fmt.Sprintf("Did not find any flacs for %v", id))
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
				s.Log(fmt.Sprintf("Unable to convert %v -> %v", name, err))
				return err
			}

			trackFiles, _ := s.io.readSubdir(f.Name())
			tracks := []*pbcdp.Track{}
			for _, tf := range trackFiles {
				if !tf.IsDir() {
					trackNumber, _ := strconv.Atoi(tf.Name()[5:7])

					var foundTrack *pbcdp.Track
					for _, t := range tracks {
						if int(t.TrackNumber) == trackNumber {
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

			if len(tracks) == 0 {
				s.RaiseIssue("Missing Tracks", fmt.Sprintf("%v disk %v has missing tracks", id, disk))
			}
			rips = append(rips, &pbcdp.Rip{Id: id, Path: f.Name(), Tracks: tracks})
		}
	}

	s.rips = rips
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
