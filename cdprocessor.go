package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	pbgd "github.com/brotherlogic/godiscogs"
	"github.com/brotherlogic/goserver"
	"github.com/brotherlogic/goserver/utils"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/brotherlogic/cdprocessor/proto"
	pbgh "github.com/brotherlogic/githubcard/proto"
	pbg "github.com/brotherlogic/goserver/proto"
	pbrc "github.com/brotherlogic/recordcollection/proto"
	pbvs "github.com/brotherlogic/versionserver/proto"
)

type getter interface {
	getRecord(ctx context.Context, id int32) (*pbrc.Record, error)
	updateRecord(ctx context.Context, rec *pbrc.Record)
}

type prodGetter struct {
	log func(in string)
}

func (rc *prodGetter) getRecord(ctx context.Context, id int32) (*pbrc.Record, error) {
	host, port, err := utils.Resolve("recordcollection")

	if err != nil {
		return nil, err
	}

	conn, err := grpc.Dial(host+":"+strconv.Itoa(int(port)), grpc.WithInsecure())
	defer conn.Close()
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	client := pbrc.NewRecordCollectionServiceClient(conn)
	resp, err := client.GetRecords(ctx, &pbrc.GetRecordsRequest{Filter: &pbrc.Record{Release: &pbgd.Release{Id: id}}})
	if err != nil {
		return nil, err
	}

	if len(resp.GetRecords()) == 0 {
		return nil, status.Error(codes.InvalidArgument, fmt.Sprintf("Unable to locate record %v", id))
	}

	return resp.GetRecords()[0], nil
}

func (rc *prodGetter) updateRecord(ctx context.Context, rec *pbrc.Record) {
	host, port, err := utils.Resolve("recordcollection")

	if err != nil {
		return
	}

	conn, err := grpc.Dial(host+":"+strconv.Itoa(int(port)), grpc.WithInsecure())
	defer conn.Close()
	if err != nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	client := pbrc.NewRecordCollectionServiceClient(conn)
	_, err = client.UpdateRecord(ctx, &pbrc.UpdateRecordRequest{Update: rec})
	rc.log(fmt.Sprintf("Updated %v (%v)", rec.GetRelease().Id, err))
}

type gh interface {
	recordMissing(r *pbrc.Record) error
}

type prodGh struct{}

func (gh *prodGh) recordMissing(r *pbrc.Record) error {
	host, port, err := utils.Resolve("githubcard")

	if err != nil {
		return err
	}

	conn, err := grpc.Dial(host+":"+strconv.Itoa(int(port)), grpc.WithInsecure())
	defer conn.Close()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	client := pbgh.NewGithubClient(conn)
	_, err = client.AddIssue(ctx, &pbgh.Issue{Title: "Rip CD", Body: fmt.Sprintf("%v [%v]", r.GetRelease().Title, r.GetRelease().Id), Service: "recordcollection"})
	return err
}

type io interface {
	readDir() ([]os.FileInfo, error)
	readSubdir(f string) ([]os.FileInfo, error)
	convert(name string) (int32, error)
}

type rc interface {
	get(filter *pbrc.Record) (*pbrc.GetRecordsResponse, error)
}

type prodRc struct{}

func (rc *prodRc) get(filter *pbrc.Record) (*pbrc.GetRecordsResponse, error) {
	host, port, err := utils.Resolve("recordcollection")

	if err != nil {
		return &pbrc.GetRecordsResponse{}, err
	}

	conn, err := grpc.Dial(host+":"+strconv.Itoa(int(port)), grpc.WithInsecure())
	defer conn.Close()
	if err != nil {
		return &pbrc.GetRecordsResponse{}, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	client := pbrc.NewRecordCollectionServiceClient(conn)
	return client.GetRecords(ctx, &pbrc.GetRecordsRequest{Filter: filter})
}

type prodIo struct {
	dir string
	log func(s string)
}

func (i *prodIo) readDir() ([]os.FileInfo, error) {
	return ioutil.ReadDir(i.dir)
}

func (i *prodIo) readSubdir(f string) ([]os.FileInfo, error) {
	return ioutil.ReadDir(i.dir + f)
}

func (i *prodIo) convert(name string) (int32, error) {
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

//Server main server type
type Server struct {
	*goserver.GoServer
	io          io
	rc          rc
	gh          gh
	getter      getter
	lastRunTime time.Duration
	adjust      int
}

// Init builds the server
func Init(dir string) *Server {
	s := &Server{GoServer: &goserver.GoServer{},
		io:     &prodIo{dir: dir},
		rc:     &prodRc{},
		gh:     &prodGh{},
		getter: &prodGetter{},
	}
	s.io = &prodIo{dir: dir, log: s.Log}
	s.getter = &prodGetter{log: s.Log}
	return s
}

// DoRegister does RPC registration
func (s *Server) DoRegister(server *grpc.Server) {
	pb.RegisterCDProcessorServer(server, s)
}

// ReportHealth alerts if we're not healthy
func (s *Server) ReportHealth() bool {
	return true
}

// Mote promotes/demotes this server
func (s *Server) Mote(ctx context.Context, master bool) error {
	resp, err := s.GetRipped(context.Background(), &pb.GetRippedRequest{})
	if err != nil {
		return err
	}
	masterCount := int64(len(resp.Ripped))
	ip, port, err := utils.Resolve("versionserver")
	if err != nil {
		return err
	}

	conn, err := grpc.Dial(ip+":"+strconv.Itoa(int(port)), grpc.WithInsecure())
	if err != nil {
		return err
	}
	defer conn.Close()

	client := pbvs.NewVersionServerClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	v, err := client.GetVersion(ctx, &pbvs.GetVersionRequest{Key: "github.com.brotherlogic.cdprocessor"})
	if err != nil {
		return err
	}

	s.Log(fmt.Sprintf("Cannot Mote %v vs %v", masterCount, v.Version.Value))
	if masterCount < v.Version.Value {
		return fmt.Errorf("Not enough rips: %v", masterCount)
	}

	return nil
}

func (s *Server) writeCount(ctx context.Context) {
	resp, err := s.GetRipped(ctx, &pb.GetRippedRequest{})
	if err == nil {
		ip, port, err := utils.Resolve("versionserver")
		if err == nil {
			conn, err := grpc.Dial(ip+":"+strconv.Itoa(int(port)), grpc.WithInsecure())
			defer conn.Close()
			if err == nil {
				client := pbvs.NewVersionServerClient(conn)
				client.SetVersion(ctx, &pbvs.SetVersionRequest{Set: &pbvs.Version{Key: "github.com.brotherlogic.cdprocessor", Value: int64(len(resp.Ripped)), Setter: "cdprocessor"}})
			}
		}
	}
}

// GetState gets the state of the server
func (s *Server) GetState() []*pbg.State {
	r, _ := s.GetRipped(context.Background(), &pb.GetRippedRequest{})
	m, _ := s.GetMissing(context.Background(), &pb.GetMissingRequest{})

	wavs := float64(0)
	mp3s := float64(0)
	tracks := 0
	for _, rip := range r.Ripped {
		tracks += len(rip.Tracks)
		for _, t := range rip.Tracks {
			if len(t.WavPath) > 0 {
				wavs++
			}
			if len(t.Mp3Path) > 0 {
				mp3s++
			}
		}
	}

	return []*pbg.State{
		&pbg.State{Key: "count", Value: int64(len(r.Ripped))},
		&pbg.State{Key: "missing", Value: int64(len(m.Missing))},
		&pbg.State{Key: "adjust_time", Text: fmt.Sprintf("%v", s.lastRunTime)},
		&pbg.State{Key: "adjust", Value: int64(s.adjust)},
		&pbg.State{Key: "tracks", Value: int64(tracks)},
		&pbg.State{Key: "wavs", Fraction: wavs / float64(tracks)},
		&pbg.State{Key: "mp3s", Fraction: mp3s / float64(tracks)},
	}
}

func main() {
	var quiet = flag.Bool("quiet", false, "Show all output")
	var dir = flag.String("dir", "/media/music/", "Base directory for storage location")
	flag.Parse()

	//Turn off logging
	if *quiet {
		log.SetFlags(0)
		log.SetOutput(ioutil.Discard)
	}
	server := Init(*dir)
	server.PrepServer()
	server.Register = server

	server.RegisterServer("cdprocessor", false)
	server.RegisterRepeatingTask(server.logMissing, "log_missing", time.Hour)
	server.RegisterRepeatingTask(server.writeCount, "write_count", time.Hour)
	server.RegisterRepeatingTask(server.adjustExisting, "adjust_existing", time.Minute)
	server.Serve()
}
