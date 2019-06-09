package main

import (
	"fmt"
	"log"
	"os"
	"testing"

	"golang.org/x/net/context"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/brotherlogic/cdprocessor/proto"
	pbgd "github.com/brotherlogic/godiscogs"
	pbrc "github.com/brotherlogic/recordcollection/proto"
)

type testMaster struct {
	fail  bool
	found int32
}

func (p *testMaster) GetRipped(ctx context.Context, req *pb.GetRippedRequest) (*pb.GetRippedResponse, error) {
	if p.fail {
		return nil, fmt.Errorf("Built to fail")
	}
	return &pb.GetRippedResponse{
		Ripped: []*pb.Rip{
			&pb.Rip{Id: p.found},
		},
	}, nil
}

type testGetter struct {
	fail     bool
	updates  int
	adjusted map[int32]bool
	override *pbrc.Record
}

func (t *testGetter) getRecord(ctx context.Context, id int32) (*pbrc.Record, error) {
	if t.fail {
		return nil, status.Error(codes.InvalidArgument, fmt.Sprintf("Built to fail"))
	}
	if t.override != nil {
		return t.override, nil
	}
	filepath := ""
	if t.adjusted[id] {
		filepath = fmt.Sprintf("%v", id)
	}
	return &pbrc.Record{Release: &pbgd.Release{Id: id,
		FormatQuantity: 1, Artists: []*pbgd.Artist{&pbgd.Artist{Name: "Hello"}},
		Formats: []*pbgd.Format{&pbgd.Format{Name: "CD", Qty: "2"}},
		Tracklist: []*pbgd.Track{&pbgd.Track{TrackType: pbgd.Track_TRACK, Position: "1"},
			&pbgd.Track{Position: "2", SubTracks: []*pbgd.Track{
				&pbgd.Track{Position: "3", TrackType: pbgd.Track_TRACK}}}}}, Metadata: &pbrc.ReleaseMetadata{FilePath: filepath},
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

func (tr *testRipper) runCommand(ctx context.Context, command []string) error {
	return nil
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
	s := InitTestServer("testdata/")
	tg := &testGetter{}
	s.getter = tg

	s.adjustExisting(context.Background())

	if tg.updates != 1 {
		t.Errorf("Update has not run: %v", tg.updates)
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

func TestRunMP3sWithNothing(t *testing.T) {
	s := InitTestServer("testempty/")
	s.convertToMP3(context.Background())
	s.convertToFlac(context.Background())

	if s.ripCount != 0 || s.flacCount != 0 {
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

	if err == nil {
		t.Errorf("Verify did not fail")
	}

}

func TestFailOnLink(t *testing.T) {
	s := InitTestServer("testdata/")
	tg := &testGetter{fail: true}
	s.getter = tg

	err := s.makeLinks(context.Background(), 1234, false)

	if err == nil {
		t.Errorf("Failing verify passed")
	}

}

func TestLink(t *testing.T) {
	s := InitTestServer("testdata/")
	tg := &testGetter{}
	s.getter = tg

	err := s.makeLinks(context.Background(), 12345, false)

	if err != nil {
		t.Errorf("Failing link passed: %v", err)
	}

}

func TestLinkBuildLinkError(t *testing.T) {
	s := InitTestServer("testdata/")
	tg := &testGetter{override: &pbrc.Record{Release: &pbgd.Release{Id: 12345,
		FormatQuantity: 2, Artists: []*pbgd.Artist{&pbgd.Artist{Name: "Hello"}},
		Formats: []*pbgd.Format{&pbgd.Format{Name: "CD", Qty: "2"}},
		Tracklist: []*pbgd.Track{&pbgd.Track{TrackType: pbgd.Track_TRACK, Position: "1"},
			&pbgd.Track{Position: "2", SubTracks: []*pbgd.Track{
				&pbgd.Track{Position: "3", TrackType: pbgd.Track_TRACK}}}}}, Metadata: &pbrc.ReleaseMetadata{FilePath: "blah"},
	}}
	s.getter = tg

	err := s.makeLinks(context.Background(), 12345, false)

	if err == nil {
		t.Errorf("Should have failed")
	}

}

func TestLinkForced(t *testing.T) {
	s := InitTestServer("testdata/")
	s.forceCheck = true
	tg := &testGetter{}
	s.getter = tg

	err := s.makeLinks(context.Background(), 1234, false)

	if err != nil {
		t.Errorf("Failing link passed")
	}

}

func TestExpand(t *testing.T) {
	s := expand("11")
	if s != "11" {
		t.Errorf("Poor expansion: %v", s)
	}
}

func TestFindMissing(t *testing.T) {
	s := InitTestServer("testdata/")
	s.master = &testMaster{}

	missing, err := s.findMissing(context.Background())

	if err != nil {
		t.Errorf("Failed: %v", err)
	}

	if missing == nil {
		t.Errorf("Oops")
	}
}

func TestFindMissingFail(t *testing.T) {
	s := InitTestServer("testdata/")
	s.master = &testMaster{fail: true}

	missing, err := s.findMissing(context.Background())

	if err == nil {
		t.Errorf("Did not fail: %v", missing)
	}
}

func TestFindMissingNone(t *testing.T) {
	err := os.RemoveAll("testdata/mp31234")
	err = os.RemoveAll("testdata/mp312345")

	s := InitTestServer("testdata/")
	s.master = &testMaster{found: int32(12345)}

	missing, err := s.findMissing(context.Background())

	if err != nil {
		t.Errorf("Failed: %v", err)
	}

	if missing != nil {
		t.Errorf("Found one: %v", missing)
	}
}

func TestForceRecreate(t *testing.T) {
	log.Printf("FORCING")
	s := InitTestServer("testdata/")
	s.getter = &testGetter{}
	_, err := s.Force(context.Background(), &pb.ForceRequest{Type: pb.ForceRequest_RECREATE_LINKS, Id: int32(12345)})
	if err != nil {
		t.Errorf("Recreate links failed: %v", err)
	}
}

func TestVerifyTrackMismatchFail(t *testing.T) {
	s := InitTestServer("testdata/")
	s.getter = &testGetter{
		override: &pbrc.Record{Release: &pbgd.Release{Id: 123, Tracklist: []*pbgd.Track{&pbgd.Track{}}}, Metadata: &pbrc.ReleaseMetadata{CdPath: "testmp3s/"}},
	}
	err := s.verify(context.Background(), int32(123))

	if err != nil {
		t.Errorf("Verify did not fail with tracklist mismatch %v", err)
	}
}
