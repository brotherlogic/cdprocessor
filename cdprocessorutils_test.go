package main

import (
	"fmt"
	"testing"

	"golang.org/x/net/context"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pbgd "github.com/brotherlogic/godiscogs"
	pbrc "github.com/brotherlogic/recordcollection/proto"
)

type testGetter struct {
	fail     bool
	updates  int
	adjusted map[int32]bool
}

func (t *testGetter) getRecord(ctx context.Context, id int32) (*pbrc.Record, error) {
	if t.fail {
		return nil, status.Error(codes.InvalidArgument, fmt.Sprintf("Built to fail"))
	}
	filepath := ""
	if t.adjusted[id] {
		filepath = fmt.Sprintf("%v", id)
	}
	return &pbrc.Record{Release: &pbgd.Release{Id: id}, Metadata: &pbrc.ReleaseMetadata{FilePath: filepath}}, nil
}

func (t *testGetter) updateRecord(ctx context.Context, rec *pbrc.Record) {
	t.updates++
	if t.adjusted == nil {
		t.adjusted = make(map[int32]bool)
	}
	t.adjusted[rec.GetRelease().Id] = true
}

func InitTestServer(dir string) *Server {
	s := Init(dir)
	s.io = &testIo{dir: dir}
	s.rc = &testRc{}
	gh := &testGh{}
	s.gh = gh
	s.SkipLog = true
	return s
}

func TestAdjust(t *testing.T) {
	s := InitTestServer("testdata")
	tg := &testGetter{}
	s.getter = tg

	s.adjustExisting(context.Background())

	if tg.updates != 1 {
		t.Errorf("Update has not run!")
	}
}

func TestAdjustFail(t *testing.T) {
	s := InitTestServer("testdata")
	tg := &testGetter{}
	s.io = &testIo{failRead: true}
	s.getter = tg

	s.adjustExisting(context.Background())

	if tg.updates != 0 {
		t.Errorf("Update has run!")
	}
}

func TestAdjustFailOnFailedGet(t *testing.T) {
	s := InitTestServer("testdata")
	tg := &testGetter{fail: true}
	s.io = &testIo{dir: "testdata"}
	s.getter = tg

	s.adjustExisting(context.Background())

	if tg.updates != 0 {
		t.Errorf("Update has run!")
	}
}

func TestLogMissing(t *testing.T) {
	s := InitTestServer("testdata")
	s.io = &testIo{dir: "testdata"}
	s.rc = &testRc{}
	gh := &testGh{}
	s.gh = gh
	s.SkipLog = true

	s.logMissing(context.Background())

	if gh.count != 1 {
		t.Errorf("Missing has not been logged")
	}
}

func TestLogMissingFailOnMissing(t *testing.T) {
	s := InitTestServer("testdata")
	s.io = &testIo{dir: "testdata", failRead: true}
	s.rc = &testRc{}
	gh := &testGh{}
	s.gh = gh
	s.SkipLog = true

	s.logMissing(context.Background())

	if gh.count > 0 {
		t.Errorf("Failing missing has not failed log")
	}

}

func TestLogMissingFailOnBadLog(t *testing.T) {
	s := InitTestServer("testdata")
	s.io = &testIo{dir: "testdata"}
	s.rc = &testRc{}
	gh := &testGh{fail: true}
	s.gh = gh
	s.SkipLog = true

	s.logMissing(context.Background())

	if gh.count > 0 {
		t.Errorf("Failing missing has not failed log:")
	}

}

func TestMultiAdjustPasses(t *testing.T) {
	s := InitTestServer("testmulti")
	getter := &testGetter{adjusted: make(map[int32]bool)}
	s.getter = getter

	for i := 0; i < 3; i++ {
		s.adjustExisting(context.Background())
	}

	if len(getter.adjusted) != 2 {
		t.Errorf("Not enough records have been adjusted: %v", getter.adjusted)
	}
}
