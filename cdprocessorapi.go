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
		if f.IsDir() {
			name := f.Name()
			id, err := s.io.convert(name)
			if err != nil {
				return &pbcdp.GetRippedResponse{}, fmt.Errorf("Unable to convert %v -> %v", name, err)
			}
			resp.Ripped = append(resp.Ripped, &pbcdp.Rip{Id: id, Path: f.Name()})
		}
	}

	return resp, nil
}

//GetMissing gets the missing rips
func (s *Server) GetMissing(ctx context.Context, req *pbcdp.GetMissingRequest) (*pbcdp.GetMissingResponse, error) {
	resp := &pbcdp.GetMissingResponse{}

	missing, err := s.rc.get(&pbrc.Record{Release: &pbgd.Release{FolderId: 242018}})
	if err != nil {
		return resp, err
	}

	ripped, err := s.GetRipped(ctx, &pbcdp.GetRippedRequest{})
	if err != nil {
		return resp, err
	}

	for _, r := range missing.Records {
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

	return resp, nil
}
