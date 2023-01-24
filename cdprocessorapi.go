package main

import (
	"fmt"

	"golang.org/x/net/context"

	pbcdp "github.com/brotherlogic/cdprocessor/proto"
	rcpb "github.com/brotherlogic/recordcollection/proto"
)

// GetRipped returns the ripped cds
func (s *Server) GetRipped(ctx context.Context, req *pbcdp.GetRippedRequest) (*pbcdp.GetRippedResponse, error) {
	return &pbcdp.GetRippedResponse{Ripped: s.rips}, nil
}

// GetMissing gets the missing rips
func (s *Server) GetMissing(ctx context.Context, req *pbcdp.GetMissingRequest) (*pbcdp.GetMissingResponse, error) {
	resp := &pbcdp.GetMissingResponse{}

	for _, id := range []int32{242018, 288751, 812802, 242017, 857449, 673768, 1782105} {
		missing, err := s.rc.getRecordsInFolder(ctx, id)
		if err != nil {
			return resp, err
		}

		ripped, _ := s.GetRipped(ctx, &pbcdp.GetRippedRequest{})

		for _, r := range missing {
			hasCD := false
			for _, f := range r.GetRelease().GetFormats() {
				if f.Name == "CD" || f.Name == "File" || f.Name == "CDr" {
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

// Force the processor to do something
func (s *Server) Force(ctx context.Context, req *pbcdp.ForceRequest) (*pbcdp.ForceResponse, error) {
	switch req.Type {
	case pbcdp.ForceRequest_RECREATE_LINKS:
		return &pbcdp.ForceResponse{}, s.makeLinks(ctx, req.Id, true)
	}
	return nil, fmt.Errorf("Unknow force request")
}

// ClientUpdate on an updated record
func (s *Server) ClientUpdate(ctx context.Context, req *rcpb.ClientUpdateRequest) (*rcpb.ClientUpdateResponse, error) {
	//return &rcpb.ClientUpdateResponse{}, nil
	return &rcpb.ClientUpdateResponse{}, s.makeLinks(ctx, req.GetInstanceId(), false)
}

func (s *Server) GetOutstanding(ctx context.Context, req *pbcdp.GetOutstandingRequest) (*pbcdp.GetOutstandingResponse, error) {
	config, err := s.load(ctx)
	if err != nil {
		return nil, err
	}

	var nums []int32
	for _, issue := range config.GetIssueMapping() {
		nums = append(nums, issue)
	}

	return &pbcdp.GetOutstandingResponse{Ids: nums}, nil
}
