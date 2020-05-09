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
	records, err := s.getter.getRecord(ctx, ID)
	if err != nil {
		return err
	}

	for _, record := range records {
		err := s.verifyRecord(ctx, record)
		if err != nil {
			return err
		}
	}

	return nil
}

func (s *Server) verifyRecord(ctx context.Context, record *pbrc.Record) error {

	if len(record.GetMetadata().CdPath) == 0 {
		s.RaiseIssue(ctx, "Missing MP3", fmt.Sprintf("%v [%v] is missing the CD Path: %v", record.GetRelease().Title, record.GetRelease().Id, record.GetMetadata()), false)
	}

	files, err := ioutil.ReadDir(record.GetMetadata().CdPath)
	if len(files) == 0 || err != nil {
		s.Force(ctx, &pbcdp.ForceRequest{Type: pbcdp.ForceRequest_RECREATE_LINKS, Id: record.GetRelease().Id})
		files, err = ioutil.ReadDir(record.GetMetadata().CdPath)
		if len(files) == 0 || err != nil {
			return status.Error(codes.DataLoss, fmt.Sprintf("Error reading %v files (%v)", len(files), err))
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
	if val, ok := s.config.LastProcessTime[ID]; ok {
		if time.Now().Sub(time.Unix(val, 0)) < time.Hour*24 && !force {
			return status.Error(codes.ResourceExhausted, "This has been linked recently")
		}
	}

	records, err := s.getter.getRecord(ctx, ID)
	if err != nil {
		return err
	}

	record := records[0]

	if force || len(record.GetMetadata().CdPath) == 0 {
		s.Log(fmt.Sprintf("Making links for %v", ID))
		os.MkdirAll(fmt.Sprintf("%v%v", s.mp3dir, record.GetRelease().Id), os.ModePerm)

		trackSet := TrackExtract(record.GetRelease())
		s.Log(fmt.Sprintf("Building %v tracks", len(trackSet)))
		for _, track := range trackSet {
			if track.Format == "CD" || track.Format == "CDr" || track.Format == "File" {
				err := s.buildLink(ctx, track, record.GetRelease())
				if err != nil {
					return err
				}
			}
		}

		// We need to update *all* the associated records
		for _, rec := range records {
			err := s.getter.updateRecord(ctx, rec.GetRelease().GetInstanceId(), fmt.Sprintf("%v%v", s.mp3dir, record.GetRelease().Id), "")
			if err != nil {
				return err
			}
		}
	}

	s.config.LastProcessTime[ID] = time.Now().Unix()
	s.save(ctx)
	return nil
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

	return nil
}

func (s *Server) convertToMP3(ctx context.Context) error {
	for _, rip := range s.rips {
		for _, t := range rip.Tracks {
			if len(t.WavPath) > 0 && len(t.Mp3Path) == 0 {
				s.ripCount++
				s.Log(fmt.Sprintf("Ripping %v -> %v", s.dir+t.WavPath, s.dir+t.WavPath[0:len(t.WavPath)-3]+"mp3"))
				s.ripper.ripToMp3(ctx, s.dir+t.WavPath, s.dir+t.WavPath[0:len(t.WavPath)-3]+"mp3")
				s.buildConfig(ctx)
				return nil
			}
		}
	}
	return nil
}

func (s *Server) convertToFlac(ctx context.Context) error {
	for _, rip := range s.rips {
		for _, t := range rip.Tracks {
			if len(t.WavPath) > 0 && len(t.FlacPath) == 0 {
				s.flacCount++
				s.Log(fmt.Sprintf("Flaccing %v -> %v", s.dir+t.WavPath, s.dir+t.WavPath[0:len(t.WavPath)-3]+"flac"))
				s.ripper.ripToFlac(ctx, s.dir+t.WavPath, s.dir+t.WavPath[0:len(t.WavPath)-3]+"flac")
				s.buildConfig(ctx)
				return nil
			}
		}
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
				s.RaiseIssue(ctx, "Missing Tracks", fmt.Sprintf("%v disk %v has missing tracks", id, disk), false)
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
		recs, err := s.getter.getRecord(ctx, r.Id)
		if err != nil {
			e, ok := status.FromError(err)
			if !ok || e.Code() == codes.InvalidArgument {
				s.RaiseIssue(ctx, "Nil Record Fail", fmt.Sprintf("Nil record?: %v -> %v", r.Id, err), false)
			}
		}
		for _, rec := range recs {
			if rec.GetMetadata().FilePath == "" {
				s.getter.updateRecord(ctx, rec.GetRelease().GetInstanceId(), "", r.Path)
				s.adjust++
				break
			}
		}
	}

	s.lastRunTime = time.Now().Sub(t)

	return nil
}

func (s *Server) logMissing(ctx context.Context) error {
	m, _ := s.GetMissing(context.Background(), &pbcdp.GetMissingRequest{})

	if len(m.Missing) > 0 {
		s.RaiseIssue(ctx, "Rip CD", fmt.Sprintf("%v [%v]", m.Missing[0].GetRelease().Title, m.Missing[0].GetRelease().Id), false)
	}

	return nil
}
