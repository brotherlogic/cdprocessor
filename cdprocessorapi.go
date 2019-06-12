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
	return &pbcdp.GetRippedResponse{Ripped: s.rips}, nil
}

//GetMissing gets the missing rips
func (s *Server) GetMissing(ctx context.Context, req *pbcdp.GetMissingRequest) (*pbcdp.GetMissingResponse, error) {
	resp := &pbcdp.GetMissingResponse{}

	for _, id := range []int32{242018, 288751, 812802, 242017, 857449, 673768, 1782105} {
		missing, err := s.rc.get(ctx, &pbrc.Record{Release: &pbgd.Release{FolderId: id}})
		if err != nil {
			return resp, err
		}

		ripped, _ := s.GetRipped(ctx, &pbcdp.GetRippedRequest{})

		for _, r := range missing.Records {

			hasCD := false
			for _, f := range r.GetRelease().GetFormats() {
				if f.Name == "CD" || f.Name == "File" {
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

//Force the processor to do something
func (s *Server) Force(ctx context.Context, req *pbcdp.ForceRequest) (*pbcdp.ForceResponse, error) {
	switch req.Type {
	case pbcdp.ForceRequest_RECREATE_LINKS:
		return &pbcdp.ForceResponse{}, s.makeLinks(ctx, req.Id, true)
	}
	return nil, fmt.Errorf("Unknow force request")

}
