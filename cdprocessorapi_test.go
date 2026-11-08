package main

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"strconv"
	"strings"
	"testing"

	pbcdp "github.com/brotherlogic/cdprocessor/proto"
	pbgd "github.com/brotherlogic/godiscogs/proto"
	pbrc "github.com/brotherlogic/recordcollection/proto"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type testGh struct {
	count int
	fail  bool
}

func (gh *testGh) recordMissing(r *pbrc.Record) error {
	if gh.fail {
		return fmt.Errorf("Built to fail")
	}
	gh.count++
	return nil
}

type testRc struct {
	failGet bool
}

func (rc *testRc) getRecordsInFolder(ctx context.Context, folder int32) ([]*pbrc.Record, error) {
	if rc.failGet {
		return nil, fmt.Errorf("Built to fail")
	}
	return []*pbrc.Record{
		&pbrc.Record{Release: &pbgd.Release{Id: 12345, Formats: []*pbgd.Format{&pbgd.Format{Name: "CD"}}}},
		&pbrc.Record{Release: &pbgd.Release{Id: 12346, Formats: []*pbgd.Format{&pbgd.Format{Name: "CD"}}}},
	}, nil
}

type testIo struct {
	dir      string
	failRead bool
	failConv bool
}

func (i *testIo) readDir() ([]os.FileInfo, error) {
	if i.failRead {
		return make([]os.FileInfo, 0), fmt.Errorf("Build to fail")
	}

	return ioutil.ReadDir(i.dir)
}

func (i *testIo) readSubdir(f string) ([]os.FileInfo, error) {
	return ioutil.ReadDir(i.dir + f)
}

func (i *testIo) convert(name string) (int32, int32, error) {
	if i.failConv {
		return -1, -1, fmt.Errorf("Build to fail")
	}

	if strings.Contains(name, "_") {
		val, err := strconv.Atoi(name[:strings.Index(name, "_")])
		if err != nil {
			return -1, -1, err
		}
		dval, err := strconv.Atoi(name[strings.Index(name, "_")+1:])
		if err != nil {
			return -1, -1, err
		}
		return int32(val), int32(dval), nil
	}

	val, err := strconv.Atoi(name)
	if err != nil {
		return -1, -1, err
	}
	return int32(val), 1, nil
}

func TestGetRipped(t *testing.T) {
	s := InitTestServer("testdata/")
	ripped, err := s.GetRipped(context.Background(), &pbcdp.GetRippedRequest{})
	if err != nil {
		t.Fatalf("Error getting ripped: %v", err)
	}

	if len(ripped.GetRipped()) != 1 || ripped.GetRipped()[0].Id != 12345 {
		t.Errorf("Error in reading rips: %v -> %v", ripped, err)
	}
}

func TestGetFailRead(t *testing.T) {
	s := InitTestServer("testdata/")
	s.io = &testIo{dir: "testdata/", failRead: true}
	s.rips = []*pbcdp.Rip{}
	s.buildConfig(context.Background())
	rips, _ := s.GetRipped(context.Background(), &pbcdp.GetRippedRequest{})
	if len(rips.Ripped) != 0 {
		t.Fatalf("Bad read did not fail: %v", rips)
	}
}

func TestGetFailConvert(t *testing.T) {
	s := InitTestServer("testdata/")
	s.io = &testIo{dir: "testdata/", failConv: true}

	s.rips = []*pbcdp.Rip{}
	s.buildConfig(context.Background())

	rips, _ := s.GetRipped(context.Background(), &pbcdp.GetRippedRequest{})
	if len(rips.Ripped) != 0 {
		t.Fatalf("Bad read did not fail: %v", rips)
	}
}

func TestGetMissingFailGet(t *testing.T) {
	s := InitTestServer("testdata/")
	s.io = &testIo{dir: "testdata/"}
	s.rc = &testRc{failGet: true}
	missing, err := s.GetMissing(context.Background(), &pbcdp.GetMissingRequest{})
	if err == nil {
		t.Fatalf("Should have failed: %v", missing)
	}
}

func TestUnkownForce(t *testing.T) {
	s := InitTestServer("testdata/")
	_, err := s.Force(context.Background(), &pbcdp.ForceRequest{})
	if err == nil {
		t.Errorf("Empty force should have failed")
	}
}

func TestGetMissingPrunesNegativeAndInvalid(t *testing.T) {
	s := InitTestServer("testdata/")
	ctx := context.Background()

	// -2105682100 is int32(2189285196)
	err := s.save(ctx, &pbcdp.Config{
		LastProcessTime: map[int64]int64{
			-2105682100: 100,
			2189285196:  200,
		},
		ToGo: []int64{-2105682100, 38309811, 2147483647, 2202219509, 2202219509},
	})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	s.getter = &testGetter{
		errByID: map[int64]error{
			38309811:   status.Error(codes.InvalidArgument, "not found"),
			2147483647: status.Error(codes.OutOfRange, "deleted"),
		},
		records: map[int64]*pbrc.Record{
			2202219509: &pbrc.Record{
				Release:  &pbgd.Release{Id: 99999, InstanceId: 2202219509, Title: "Valid Missing"},
				Metadata: &pbrc.ReleaseMetadata{},
			},
		},
	}

	resp, err := s.GetMissing(ctx, &pbcdp.GetMissingRequest{})
	if err != nil {
		t.Fatalf("GetMissing failed: %v", err)
	}
	if len(resp.GetMissing()) != 1 || resp.GetMissing()[0].GetRelease().GetInstanceId() != 2202219509 {
		t.Fatalf("Unexpected missing response: %v", resp.GetMissing())
	}

	cfg, err := s.load(ctx)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if len(cfg.GetToGo()) != 1 || cfg.GetToGo()[0] != 2202219509 {
		t.Errorf("Expected ToGo to contain only [2202219509], got %v", cfg.GetToGo())
	}
	if _, ok := cfg.GetLastProcessTime()[-2105682100]; ok {
		t.Errorf("Expected negative key to be removed from LastProcessTime: %v", cfg.GetLastProcessTime())
	}
}

func TestGetMissingPrunesAlreadyProcessedCdPath(t *testing.T) {
	s := InitTestServer("testdata/")
	ctx := context.Background()

	// Negative ID not in LastProcessTime, but its record already has CdPath set
	err := s.save(ctx, &pbcdp.Config{
		ToGo: []int64{-2105682100},
	})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	s.getter = &testGetter{
		records: map[int64]*pbrc.Record{
			2189285196: &pbrc.Record{
				Release:  &pbgd.Release{Id: 38309811, InstanceId: 2189285196, Title: "Tarkovsky"},
				Metadata: &pbrc.ReleaseMetadata{CdPath: "/home/simon/music/flacs/38309811"},
			},
		},
	}

	resp, err := s.GetMissing(ctx, &pbcdp.GetMissingRequest{})
	if err != nil {
		t.Fatalf("GetMissing failed: %v", err)
	}
	if len(resp.GetMissing()) != 0 {
		t.Fatalf("Expected no missing records, got: %v", resp.GetMissing())
	}

	cfg, err := s.load(ctx)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if len(cfg.GetToGo()) != 0 {
		t.Errorf("Expected empty ToGo, got %v", cfg.GetToGo())
	}
}

func TestClientUpdatePrunesInvalidAndNegative(t *testing.T) {
	s := InitTestServer("testdata/")
	ctx := context.Background()

	err := s.save(ctx, &pbcdp.Config{
		ToGo: []int64{38309811, -2105682100},
	})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	s.getter = &testGetter{
		errByID: map[int64]error{
			38309811: status.Error(codes.InvalidArgument, "not found"),
		},
		records: map[int64]*pbrc.Record{
			2189285196: &pbrc.Record{
				Release:  &pbgd.Release{Id: 38309811, InstanceId: 2189285196, FolderId: 123},
				Metadata: &pbrc.ReleaseMetadata{DateArrived: 100},
			},
		},
	}

	// ClientUpdate on non-existent ID should remove it from ToGo, not re-add it
	_, err = s.ClientUpdate(ctx, &pbrc.ClientUpdateRequest{InstanceId: 38309811})
	if err != nil {
		t.Fatalf("ClientUpdate for 38309811 failed: %v", err)
	}

	// ClientUpdate on 2189285196 should remove normalized -2105682100 from ToGo
	_, err = s.ClientUpdate(ctx, &pbrc.ClientUpdateRequest{InstanceId: 2189285196})
	if err != nil {
		t.Fatalf("ClientUpdate for 2189285196 failed: %v", err)
	}

	cfg, err := s.load(ctx)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if len(cfg.GetToGo()) != 0 {
		t.Errorf("Expected ToGo to be empty after ClientUpdates, got %v", cfg.GetToGo())
	}
}
