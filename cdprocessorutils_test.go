package main

import (
	"fmt"
	"log"
	"os"
	"testing"

	keystoreclient "github.com/brotherlogic/keystore/client"
	"golang.org/x/net/context"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/brotherlogic/cdprocessor/proto"
	pbgd "github.com/brotherlogic/godiscogs/proto"
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
	fail       bool
	failUpdate bool
	updates    int
	adjusted   map[int32]bool
	override   *pbrc.Record
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
	return &pbrc.Record{Release: &pbgd.Release{Id: id, InstanceId: id, FolderId: 812802,
		FormatQuantity: 1, Artists: []*pbgd.Artist{&pbgd.Artist{Name: "Hello"}},
		Formats: []*pbgd.Format{&pbgd.Format{Name: "CD", Qty: "2"}},
		Tracklist: []*pbgd.Track{&pbgd.Track{TrackType: pbgd.Track_TRACK, Position: "1"},
			&pbgd.Track{Position: "2", SubTracks: []*pbgd.Track{
				&pbgd.Track{Position: "3", TrackType: pbgd.Track_TRACK}}}}}, Metadata: &pbrc.ReleaseMetadata{FilePath: filepath},
	}, nil
}

func (t *testGetter) updateRecord(ctx context.Context, id int32, cdpath, filepath string) error {
	log.Printf("HERE %v", id)
	if t.failUpdate {
		return fmt.Errorf("Built to fail")
	}
	t.updates++
	if t.adjusted == nil {
		t.adjusted = make(map[int32]bool)
	}
	t.adjusted[id] = true
	return nil
}

type testRipper struct{}

func (tr *testRipper) ripToMp3(ctx context.Context, pathIn, pathOut string) {
	log.Printf("Ripping %v -> %v", pathIn, pathOut)
}

func (tr *testRipper) runCommand(ctx context.Context, command []string, delete bool) error {
	return nil
}

func (tr *testRipper) ripToFlac(ctx context.Context, pathIn, pathOut string) {
	log.Printf("Ripping %v -> %v", pathIn, pathOut)
}
func InitTestServer(dir string) *Server {
	s := Init(dir, dir+"mp3", dir+"flac")
	s.io = &testIo{dir: dir}
	s.rc = &testRc{}
	s.getter = &testGetter{}
	s.SkipLog = true
	s.SkipIssue = true
	s.buildConfig(context.Background())
	s.ripper = &testRipper{}
	s.GoServer.KSclient = *keystoreclient.GetTestClient(".test")
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

func TestRunMP3sWithNothing(t *testing.T) {
	s := InitTestServer("testempty/")
	s.convertToMP3(context.Background(), 123)
	s.convertToFlac(context.Background(), 123)

	if s.ripCount != 0 || s.flacCount != 0 {
		t.Errorf("no rips occured")
	}
}

func TestFailOnVerify(t *testing.T) {
	s := InitTestServer("testdata/")
	tg := &testGetter{fail: true}
	s.getter = tg

	err := s.verify(context.Background(), 1234, &pb.Config{})

	if err == nil {
		t.Errorf("Failing verify passed")
	}

}

func TestVerifyMissingPath(t *testing.T) {
	s := InitTestServer("testdata/")
	tg := &testGetter{}
	s.getter = tg

	err := s.verify(context.Background(), 1234, &pb.Config{GoalFolder: make(map[int32]int32), LastRipTime: make(map[int32]int64), IssueMapping: map[int32]int32{}})

	if err == nil {
		t.Errorf("Verify did not fail")
	}

}

func TestFailOnLink(t *testing.T) {
	s := InitTestServer("testdata/")
	tg := &testGetter{fail: true}
	s.getter = tg

	err := s.makeLinks(context.Background(), 1234, false, &pb.Config{})

	if err == nil {
		t.Errorf("Failing verify passed")
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
