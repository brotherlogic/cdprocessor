package main

import (
	"fmt"

	"golang.org/x/net/context"

	pbcdp "github.com/brotherlogic/cdprocessor/proto"
	pbgd "github.com/brotherlogic/godiscogs"
	pbrc "github.com/brotherlogic/recordcollection/proto"
)

//GetRipped returns the ripped cds
func (s *Server) GetRipped(ctx context.Context, req *pbcdp.GetRippedRequest) (*pbcdp.GetRippedResponse, error) {
	files, err := s.io.readDir()
	if err != nil {
		return &pbcdp.GetRippedResponse{}, err
	}

	resp := &pbcdp.GetRippedResponse{Ripped: make([]*pbcdp.Rip, 0)}
	for _, f := range files {
		if f.IsDir() && f.Name() != "lost+found" {
			name := f.Name()
			id, err := s.io.convert(name)
			if err != nil {
				return &pbcdp.GetRippedResponse{}, fmt.Errorf("Unable to convert %v -> %v", name, err)
			}

			trackFiles, _ := s.io.readSubdir(f.Name())
			tracks := []*pbcdp.Track{}
			for _, tf := range trackFiles {
				tracks = append(tracks, &pbcdp.Track{WavPath: f.Name() + "/" + tf.Name()})
			}

			s.Log(fmt.Sprintf("Added %v tracks", len(tracks)))

			resp.Ripped = append(resp.Ripped, &pbcdp.Rip{Id: id, Path: f.Name(), Tracks: tracks})
		}
	}

	return resp, nil
}

//GetMissing gets the missing rips
func (s *Server) GetMissing(ctx context.Context, req *pbcdp.GetMissingRequest) (*pbcdp.GetMissingResponse, error) {
	resp := &pbcdp.GetMissingResponse{}

	for _, id := range []int32{242018, 288751, 812802, 242017, 857449, 673768} {
		missing, err := s.rc.get(&pbrc.Record{Release: &pbgd.Release{FolderId: id}})
		if err != nil {
			return resp, err
		}

		ripped, err := s.GetRipped(ctx, &pbcdp.GetRippedRequest{})
		if err != nil {
			return resp, err
		}

		for _, r := range missing.Records {

			hasCD := false
			for _, f := range r.GetRelease().GetFormats() {
				if f.Name == "CD" {
					hasCD = true
				}
			}

			if hasCD {
				found := false
				for _, ri := range ripped.GetRipped() {
					if ri.Id == r.GetRelease().Id {
						found = true
					}
				}
				if !found {
					resp.Missing = append(resp.GetMissing(), r)
				}
			}
		}
	}

	return resp, nil
}
