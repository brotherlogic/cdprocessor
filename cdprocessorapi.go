package main

import (
	"fmt"
	"time"

	"golang.org/x/net/context"

	pb "github.com/brotherlogic/cdprocessor/proto"
	pbcdp "github.com/brotherlogic/cdprocessor/proto"
	rcpb "github.com/brotherlogic/recordcollection/proto"
)

func (s *Server) updateMetrics(ctx context.Context, config *pb.Config) {
	last24 := 0
	s.CtxLog(ctx, fmt.Sprintf("SEEN METRICS %v", len(config.GetGoalFolder())))
	for key, date := range config.GetLastRipTime() {
		s.CtxLog(ctx, fmt.Sprintf("SEEN %v -> %v", key, config.GetGoalFolder()[key]))
		if val, ok := config.GetGoalFolder()[key]; ok && val != 1782105 {
			if time.Since(time.Unix(date, 0)) < time.Hour*18 {
				last24++
			}
		}
	}

	ripped24Hours.Set(float64(last24))
}

// GetRipped returns the ripped cds
func (s *Server) GetRipped(ctx context.Context, req *pbcdp.GetRippedRequest) (*pbcdp.GetRippedResponse, error) {
	return &pbcdp.GetRippedResponse{Ripped: s.rips}, nil
}

// GetMissing gets the missing rips
func (s *Server) GetMissing(ctx context.Context, req *pbcdp.GetMissingRequest) (*pbcdp.GetMissingResponse, error) {
	resp := &pbcdp.GetMissingResponse{}

	for _, id := range []int32{242018, 288751, 812802, 242017, 857449, 673768, 1782105, 7664293} {
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
	config, err := s.load(ctx)
	if err != nil {
		return nil, err
	}
	s.hack.Lock()
	defer s.hack.Unlock()
	switch req.Type {
	case pbcdp.ForceRequest_RECREATE_LINKS:
		return &pbcdp.ForceResponse{}, s.makeLinks(ctx, req.Id, true, config)
	}
	return nil, fmt.Errorf("Unknow force request")
}

// ClientUpdate on an updated record
func (s *Server) ClientUpdate(ctx context.Context, req *rcpb.ClientUpdateRequest) (*rcpb.ClientUpdateResponse, error) {
	//return &rcpb.ClientUpdateResponse{}, nil
	config, err := s.load(ctx)
	if err != nil {
		return nil, err
	}
	s.hack.Lock()
	defer s.hack.Unlock()
	return &rcpb.ClientUpdateResponse{}, s.makeLinks(ctx, req.GetInstanceId(), false, config)
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
