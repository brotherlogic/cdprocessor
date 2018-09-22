package main

import (
	"fmt"

	"golang.org/x/net/context"

	pbcdp "github.com/brotherlogic/cdprocessor/proto"
)

func (s *Server) adjustExisting(ctx context.Context) {
	m, err := s.GetRipped(ctx, &pbcdp.GetRippedRequest{})

	if err != nil {
		return
	}

	for _, r := range m.Ripped {
		rec, err := s.getter.getRecord(ctx, r.Id)
		if err != nil {
			s.RaiseIssue(ctx, "Nil Record Fail", fmt.Sprintf("Nil record?: %v -> %v", r.Id, err), false)
		} else {
			if rec.GetMetadata().FilePath != r.Path {
				rec.GetMetadata().FilePath = r.Path
				s.getter.updateRecord(ctx, rec)
			}
		}
	}
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
