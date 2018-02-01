package main

import (
	"context"
	"fmt"
	"io/ioutil"
	"log"
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
	fail bool
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
		&pbrc.Record{Release: &pbgd.Release{Id: 12345}},
		&pbrc.Record{Release: &pbgd.Release{Id: 12346}},
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

	log.Printf("WHAT: %v", i.dir)
	return ioutil.ReadDir(i.dir)
}

func (i *testIo) convert(name string) (int32, error) {
	log.Printf("HERE: %v", name)
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
	s := Init("testdata")
	ripped, err := s.GetRipped(context.Background(), &pbcdp.GetRippedRequest{})
	if err != nil {
		t.Fatalf("Error getting ripped: %v", err)
	}

	if len(ripped.GetRippedIds()) != 1 || ripped.GetRippedIds()[0] != 12345 {
		t.Errorf("Error in reading rips: %v", ripped)
	}
}

func TestGetFailRead(t *testing.T) {
	s := Init("testdata")
	s.io = &testIo{dir: "testdata", failRead: true}
	_, err := s.GetRipped(context.Background(), &pbcdp.GetRippedRequest{})
	if err == nil {
		t.Fatalf("Bad read did not fail: %v", err)
	}
}

func TestGetFailConvert(t *testing.T) {
	s := Init("testdata")
	s.io = &testIo{dir: "testdata", failConv: true}
	_, err := s.GetRipped(context.Background(), &pbcdp.GetRippedRequest{})
	if err == nil {
		t.Fatalf("Bad read did not fail: %v", err)
	}
}

func TestGetMissing(t *testing.T) {
	s := Init("testdata")
	s.io = &testIo{dir: "testdata"}
	s.rc = &testRc{}
	missing, err := s.GetMissing(context.Background(), &pbcdp.GetMissingRequest{})
	if err != nil {
		t.Fatalf("Error getting missing: %v", err)
	}

	if len(missing.GetMissing()) != 1 || missing.GetMissing()[0].GetRelease().Id != 12346 {
		t.Errorf("Rips reported missing: %v", missing)
	}
}

func TestGetMissingFailGet(t *testing.T) {
	s := Init("testdata")
	s.io = &testIo{dir: "testdata"}
	s.rc = &testRc{failGet: true}
	missing, err := s.GetMissing(context.Background(), &pbcdp.GetMissingRequest{})
	if err == nil {
		t.Fatalf("Should have failed: %v", missing)
	}
}

func TestGetMissingFailGetRipped(t *testing.T) {
	s := Init("testdata")
	s.io = &testIo{dir: "testdata", failConv: true}
	s.rc = &testRc{}
	missing, err := s.GetMissing(context.Background(), &pbcdp.GetMissingRequest{})
	if err == nil {
		t.Fatalf("Should have failed: %v", missing)
	}
}
