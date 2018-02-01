package main

import (
	"fmt"

	"golang.org/x/net/context"
	
	pbcdp "github.com/brotherlogic/cdprocessor/proto"
)

func (s *Server) logMissing() {
	m, err := s.GetMissing(context.Background(), &pbcdp.GetMissingRequest{})
	if err != nil {
		s.Log(fmt.Sprintf("ERROR getting missing: %v", err))
		return
	}

	if len(m.Missing) > 0 {
		err := s.gh.recordMissing(m.Missing[0])
		if err != nil {
			s.Log(fmt.Sprintf("ERROR recording missing: %v", err))
			return
		}
	}
}
