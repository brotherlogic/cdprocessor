package main

import (
	"fmt"
	"log"
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
	return &pbrc.Record{Release: &pbgd.Release{
		Id: id,
		Tracklist: []*pbgd.Track{
			&pbgd.Track{TrackType: pbgd.Track_TRACK, Position: "1"},
			&pbgd.Track{Position: "2", SubTracks: []*pbgd.Track{
				&pbgd.Track{Position: "3", TrackType: pbgd.Track_TRACK}},
			},
		}},
		Metadata: &pbrc.ReleaseMetadata{FilePath: filepath},
	}, nil
}

func (t *testGetter) updateRecord(ctx context.Context, rec *pbrc.Record) {
	t.updates++
	if t.adjusted == nil {
		t.adjusted = make(map[int32]bool)
	}
	t.adjusted[rec.GetRelease().Id] = true
}

type testRipper struct{}

func (tr *testRipper) ripToMp3(ctx context.Context, pathIn, pathOut string) {
	log.Printf("Ripping %v -> %v", pathIn, pathOut)
}

func (tr *testRipper) ripToFlac(ctx context.Context, pathIn, pathOut string) {
	log.Printf("Ripping %v -> %v", pathIn, pathOut)
}

func InitTestServer(dir string) *Server {
	s := Init(dir, dir+"mp3")
	s.io = &testIo{dir: dir}
	s.rc = &testRc{}
	s.SkipLog = true
	s.buildConfig(context.Background())
	s.ripper = &testRipper{}
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
	s.SkipLog = true

	s.logMissing(context.Background())
}

func TestLogMissingFailOnBadLog(t *testing.T) {
	s := InitTestServer("testdata")
	s.io = &testIo{dir: "testdata"}
	s.rc = &testRc{}
	s.SkipLog = true

	s.logMissing(context.Background())
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

func TestRunMP3s(t *testing.T) {
	s := InitTestServer("testdata/")
	s.convertToMP3(context.Background())
	s.convertToFlac(context.Background())

	if s.ripCount != 1 || s.flacCount != 1 {
		t.Errorf("No rips occured")
	}
}

func TestFailOnVerify(t *testing.T) {
	s := InitTestServer("testdata/")
	tg := &testGetter{fail: true}
	s.getter = tg

	err := s.verify(context.Background(), 1234)

	if err == nil {
		t.Errorf("Failing verify passed")
	}

}

func TestVerifyMissingPath(t *testing.T) {
	s := InitTestServer("testdata/")
	tg := &testGetter{}
	s.getter = tg

	err := s.verify(context.Background(), 1234)

	if err != nil {
		t.Errorf("Verify failed: %v", err)
	}

}

func TestFailOnLink(t *testing.T) {
	s := InitTestServer("testdata/")
	tg := &testGetter{fail: true}
	s.getter = tg

	err := s.makeLinks(context.Background(), 1234)

	if err == nil {
		t.Errorf("Failing verify passed")
	}

}

func TestLink(t *testing.T) {
	s := InitTestServer("testdata/")
	tg := &testGetter{}
	s.getter = tg

	err := s.makeLinks(context.Background(), 1234)

	if err != nil {
		t.Errorf("Failing link passed")
	}

}
