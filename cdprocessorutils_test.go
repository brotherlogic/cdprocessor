package main

import (
	"fmt"
	"testing"

	"golang.org/x/net/context"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pbrc "github.com/brotherlogic/recordcollection/proto"
)

type testGetter struct {
	fail    bool
	updates int
}

func (t *testGetter) getRecord(ctx context.Context, id int32) (*pbrc.Record, error) {
	if t.fail {
		return nil, status.Error(codes.InvalidArgument, fmt.Sprintf("Built to fail"))
	}
	return &pbrc.Record{Metadata: &pbrc.ReleaseMetadata{FilePath: ""}}, nil
}

func (t *testGetter) updateRecord(ctx context.Context, rec *pbrc.Record) {
	t.updates++
}

func InitTestServer() *Server {
	s := Init("testdata")
	s.io = &testIo{dir: "testdata"}
	s.rc = &testRc{}
	gh := &testGh{}
	s.gh = gh
	s.SkipLog = true
	return s
}

func TestAdjust(t *testing.T) {
	s := InitTestServer()
	tg := &testGetter{}
	s.getter = tg

	s.adjustExisting(context.Background())

	if tg.updates != 1 {
		t.Errorf("Update has not run!")
	}
}

func TestAdjustFail(t *testing.T) {
	s := InitTestServer()
	tg := &testGetter{}
	s.io = &testIo{failRead: true}
	s.getter = tg

	s.adjustExisting(context.Background())

	if tg.updates != 0 {
		t.Errorf("Update has run!")
	}
}

func TestAdjustFailOnFailedGet(t *testing.T) {
	s := InitTestServer()
	tg := &testGetter{fail: true}
	s.io = &testIo{dir: "testdata"}
	s.getter = tg

	s.adjustExisting(context.Background())

	if tg.updates != 0 {
		t.Errorf("Update has run!")
	}
}

func TestLogMissing(t *testing.T) {
	s := Init("testdata")
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
	s := Init("testdata")
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
	s := Init("testdata")
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
