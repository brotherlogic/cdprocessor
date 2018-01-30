package main

import (
	"fmt"

	"golang.org/x/net/context"

	pbcdp "github.com/brotherlogic/cdprocessor/proto"
)

//GetRipped returns the ripped cds
func (s *Server) GetRipped(ctx context.Context, req *pbcdp.GetRippedRequest) (*pbcdp.GetRippedResponse, error) {
	files, err := s.io.readDir()
	if err != nil {
		return &pbcdp.GetRippedResponse{}, err
	}

	resp := &pbcdp.GetRippedResponse{RippedIds: make([]int32, 0)}
	for _, f := range files {
		if f.IsDir() {
			name := f.Name()
			id, err := s.io.convert(name)
			if err != nil {
				return &pbcdp.GetRippedResponse{}, fmt.Errorf("Unable to convert %v -> %v", name, err)
			}
			resp.RippedIds = append(resp.RippedIds, id)
		}
	}

	return resp, nil
}
