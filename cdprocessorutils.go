package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/context"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pbcdp "github.com/brotherlogic/cdprocessor/proto"
	pbgd "github.com/brotherlogic/godiscogs"
)

// verifies the status of the ripped cd
func (s *Server) verify(ctx context.Context, ID int32) error {
	record, err := s.getter.getRecord(ctx, ID)
	if err != nil {
		return err
	}

	if len(record.GetMetadata().CdPath) == 0 {
		s.RaiseIssue(ctx, "Missing MP3", fmt.Sprintf("%v [%v] is missing the CD Path", record.GetRelease().Title, ID), false)
	}

	return nil
}

func (s *Server) makeLinks(ctx context.Context, ID int32) error {
	record, err := s.getter.getRecord(ctx, ID)
	if err != nil {
		return err
	}

	if len(record.GetMetadata().CdPath) == 0 {
		os.MkdirAll(fmt.Sprintf("%v%v", s.mp3dir, record.GetRelease().Id), os.ModePerm)

		for _, track := range record.GetRelease().Tracklist {
			if track.TrackType == pbgd.Track_TRACK {
				command := fmt.Sprintf("ln -s %v%v/track%v.cdda.mp3 %v%v", s.dir, record.GetRelease().Id, track.Position, s.mp3dir, record.GetRelease().Id)
				s.Log(fmt.Sprintf("Converting: %v [%v]", command, track.Position))
			}
			for _, subtrack := range track.SubTracks {
				if subtrack.TrackType == pbgd.Track_TRACK {
					command := fmt.Sprintf("ln -s %v%v/track%v.cdda.mp3 %v%v", s.dir, record.GetRelease().Id, subtrack.Position, s.mp3dir, record.GetRelease().Id)
					s.Log(fmt.Sprintf("Converting: %v", command))
				}

			}

		}
	}

	return nil
}

func (s *Server) convertToMP3(ctx context.Context) {
	for _, rip := range s.rips {
		for _, t := range rip.Tracks {
			if len(t.WavPath) > 0 && len(t.Mp3Path) == 0 {
				s.ripCount++
				s.Log(fmt.Sprintf("Ripping %v -> %v", s.dir+t.WavPath, s.dir+t.WavPath[0:len(t.WavPath)-3]+"mp3"))
				s.ripper.ripToMp3(ctx, s.dir+t.WavPath, s.dir+t.WavPath[0:len(t.WavPath)-3]+"mp3")
				s.buildConfig(ctx)
				return
			}
		}
	}
}

func (s *Server) convertToFlac(ctx context.Context) {
	for _, rip := range s.rips {
		for _, t := range rip.Tracks {
			if len(t.WavPath) > 0 && len(t.FlacPath) == 0 {
				s.flacCount++
				s.Log(fmt.Sprintf("Flaccing %v -> %v", s.dir+t.WavPath, s.dir+t.WavPath[0:len(t.WavPath)-3]+"mp3"))
				s.ripper.ripToMp3(ctx, s.dir+t.WavPath, s.dir+t.WavPath[0:len(t.WavPath)-3]+"flac")
				s.buildConfig(ctx)
				return
			}
		}
	}
}

func (s *Server) buildConfig(ctx context.Context) {
	files, err := s.io.readDir()
	if err != nil {
		return
	}

	rips := []*pbcdp.Rip{}
	for _, f := range files {
		if f.IsDir() && f.Name() != "lost+found" {
			name := f.Name()
			id, err := s.io.convert(name)
			if err != nil {
				s.Log(fmt.Sprintf("Unable to convert %v -> %v", name, err))
				return
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
						foundTrack = &pbcdp.Track{TrackNumber: int32(trackNumber)}
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
}

func (s *Server) adjustExisting(ctx context.Context) {
	t := time.Now()
	m, _ := s.GetRipped(ctx, &pbcdp.GetRippedRequest{})

	for _, r := range m.Ripped {
		rec, err := s.getter.getRecord(ctx, r.Id)
		if err != nil {
			e, ok := status.FromError(err)
			if !ok || e.Code() == codes.InvalidArgument {
				s.RaiseIssue(ctx, "Nil Record Fail", fmt.Sprintf("Nil record?: %v -> %v", r.Id, err), false)
			}
		} else {
			if rec.GetMetadata().FilePath == "" {
				rec.GetMetadata().FilePath = r.Path
				s.getter.updateRecord(ctx, rec)
				s.adjust++
				break
			}
		}
	}

	s.lastRunTime = time.Now().Sub(t)
}

func (s *Server) logMissing(ctx context.Context) {
	m, _ := s.GetMissing(context.Background(), &pbcdp.GetMissingRequest{})

	if len(m.Missing) > 0 {
		s.RaiseIssue(ctx, "Rip CD", fmt.Sprintf("%v [%v]", m.Missing[0].GetRelease().Title, m.Missing[0].GetRelease().Id), false)
		return
	}
}
