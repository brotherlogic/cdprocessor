package main

import (
	"fmt"
	"sort"
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
	config, err := s.load(ctx)
	if err != nil {
		return nil, err
	}

	sort.Slice(config.ToGo, func(i, j int) bool {
		return config.ToGo[i] < config.ToGo[j]
	})

	var record *rcpb.Record
	if len(config.ToGo) > 0 {
		record, err = s.getter.getRecord(ctx, config.ToGo[0])
		if err != nil {
			return nil, err
		}
	}

	return &pbcdp.GetMissingResponse{Missing: []*rcpb.Record{record}}, nil
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

	err = s.makeLinks(ctx, req.GetInstanceId(), false, config)
	if err == nil {
		var ntogo []int32
		for _, togo := range config.ToGo {
			if togo != req.GetInstanceId() {
				ntogo = append(ntogo, togo)
			}
		}
		config.ToGo = ntogo
		err = s.save(ctx, config)
		if err != nil {
			return nil, err
		}
	} else {
		found := false
		for _, togo := range config.ToGo {
			if togo == req.GetInstanceId() {
				found = true
			}
		}
		if !found {
			config.ToGo = append(config.ToGo, req.GetInstanceId())
			serr := s.save(ctx, config)
			if serr != nil {
				return nil, serr
			}
		}
	}

	return &rcpb.ClientUpdateResponse{}, err
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
