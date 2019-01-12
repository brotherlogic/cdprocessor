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
	pbgd "github.com/brotherlogic/godiscogs"
	pbrc "github.com/brotherlogic/recordcollection/proto"
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

func (rc *testRc) get(filter *pbrc.Record) (*pbrc.GetRecordsResponse, error) {
	if rc.failGet {
		return &pbrc.GetRecordsResponse{}, fmt.Errorf("Built to fail")
	}
	return &pbrc.GetRecordsResponse{Records: []*pbrc.Record{
		&pbrc.Record{Release: &pbgd.Release{Id: 12345, Formats: []*pbgd.Format{&pbgd.Format{Name: "CD"}}}},
		&pbrc.Record{Release: &pbgd.Release{Id: 12346, Formats: []*pbgd.Format{&pbgd.Format{Name: "CD"}}}},
	}}, nil
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

func (i *testIo) convert(name string) (int32, error) {
	if i.failConv {
		return -1, fmt.Errorf("Build to fail")
	}

	if strings.Contains(name, "_") {
		val, err := strconv.Atoi(name[:strings.Index(name, "_")])
		if err != nil {
			return -1, err
		}
		return int32(val), nil
	}

	val, err := strconv.Atoi(name)
	if err != nil {
		return -1, err
	}
	return int32(val), nil
}

func TestGetRipped(t *testing.T) {
	s := InitTestServer("testdata")
	ripped, err := s.GetRipped(context.Background(), &pbcdp.GetRippedRequest{})
	if err != nil {
		t.Fatalf("Error getting ripped: %v", err)
	}

	if len(ripped.GetRipped()) != 1 || ripped.GetRipped()[0].Id != 12345 {
		t.Errorf("Error in reading rips: %v", ripped)
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

func TestGetMissing(t *testing.T) {
	s := InitTestServer("testdata/")
	s.io = &testIo{dir: "testdata/"}
	s.rc = &testRc{}
	missing, err := s.GetMissing(context.Background(), &pbcdp.GetMissingRequest{})
	if err != nil {
		t.Fatalf("Error getting missing: %v", err)
	}

	if len(missing.GetMissing()) != 6 || missing.GetMissing()[0].GetRelease().Id != 12346 {
		for i := range missing.GetMissing() {
			t.Errorf("%v. Missing: %v", i, missing.GetMissing()[i].GetRelease().Id)
		}
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
