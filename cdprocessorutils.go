package main

import (
	"fmt"
	"time"

	"golang.org/x/net/context"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pbcdp "github.com/brotherlogic/cdprocessor/proto"
)

func (s *Server) adjustExisting(ctx context.Context) {
	t := time.Now()
	m, err := s.GetRipped(ctx, &pbcdp.GetRippedRequest{})

	if err != nil {
		return
	}

	for _, r := range m.Ripped {
		rec, err := s.getter.getRecord(ctx, r.Id)
		if err != nil {
			e, ok := status.FromError(err)
			if ok && e.Code() == codes.InvalidArgument {
				s.RaiseIssue(ctx, "Nil Record Fail", fmt.Sprintf("Nil record?: %v -> %v", r.Id, err), false)
			}
		} else {
			if rec.GetMetadata().FilePath != r.Path {
				rec.GetMetadata().FilePath = r.Path
				s.getter.updateRecord(ctx, rec)
			}
		}
	}

	s.lastRunTime = time.Now().Sub(t)
}

func (s *Server) logMissing(ctx context.Context) {
	m, err := s.GetMissing(context.Background(), &pbcdp.GetMissingRequest{})
	if err != nil {
		return
	}

	if len(m.Missing) > 0 {
		err := s.gh.recordMissing(m.Missing[0])
		if err != nil {
			return
		}
	}
}
