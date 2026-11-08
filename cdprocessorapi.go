package main

import (
	"fmt"
	"sort"
	"time"

	"golang.org/x/net/context"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/brotherlogic/cdprocessor/proto"
	pbcdp "github.com/brotherlogic/cdprocessor/proto"
	rcpb "github.com/brotherlogic/recordcollection/proto"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	togo = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "cdprocessor_togo",
		Help: "The number of records needing a rip",
	})
)

func (s *Server) updateMetrics(ctx context.Context, config *pb.Config) {
	last24 := 0
	for key, date := range config.GetLastRipTime() {
		if val, ok := config.GetGoalFolder()[key]; ok && val != 1782105 {
			if time.Since(time.Unix(date, 0)) < time.Hour*18 {
				last24++
			}
		}
	}

	ripped24Hours.Set(float64(last24))

	togo.Set(float64(len(config.GetToGo())))
}

func (s *Server) cleanConfig(config *pb.Config) bool {
	changed := false
	if config.LastProcessTime == nil {
		config.LastProcessTime = make(map[int64]int64)
	}

	for k, v := range config.LastProcessTime {
		if k < 0 {
			nk := int64(uint32(k))
			if v > config.LastProcessTime[nk] {
				config.LastProcessTime[nk] = v
			}
			delete(config.LastProcessTime, k)
			changed = true
		}
	}

	seen := make(map[int64]bool)
	var ntogo []int64
	for _, id := range config.ToGo {
		nid := id
		if nid < 0 {
			nid = int64(uint32(nid))
			changed = true
			if config.LastProcessTime[nid] > 0 {
				continue
			}
		}
		if !seen[nid] {
			seen[nid] = true
			ntogo = append(ntogo, nid)
		} else {
			changed = true
		}
	}
	config.ToGo = ntogo
	return changed
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

	for len(config.ToGo) > 0 {
		record, err := s.getter.getRecord(ctx, config.ToGo[0])
		if err != nil {
			code := status.Code(err)
			if code == codes.OutOfRange || code == codes.InvalidArgument || code == codes.NotFound {
				config.ToGo = config.ToGo[1:]
				if err := s.save(ctx, config); err != nil {
					return nil, err
				}
				continue
			}

			return nil, err
		}

		if len(record.GetMetadata().GetCdPath()) > 0 {
			config.ToGo = config.ToGo[1:]
			if err := s.save(ctx, config); err != nil {
				return nil, err
			}
			continue
		}

		return &pbcdp.GetMissingResponse{Missing: []*rcpb.Record{record}}, nil
	}

	return &pbcdp.GetMissingResponse{Missing: []*rcpb.Record{}}, nil
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

	instanceID := req.GetInstanceId()
	if instanceID < 0 {
		instanceID = int64(uint32(instanceID))
	}

	err = s.makeLinks(ctx, instanceID, false, config)
	if err == nil {
		var ntogo []int64
		for _, togo := range config.ToGo {
			if togo != instanceID {
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
			if togo == instanceID {
				found = true
			}
		}
		if !found {
			config.ToGo = append(config.ToGo, instanceID)
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
