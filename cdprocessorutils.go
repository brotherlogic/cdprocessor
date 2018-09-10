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
		rec := s.getter.getRecord(ctx, r.Id)
		if rec == nil {
			s.RaiseIssue(ctx, fmt.Sprintf("Nil record?: %v", r.Id))
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
