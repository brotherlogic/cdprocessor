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
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/brotherlogic/cdprocessor/proto"
	pbe "github.com/brotherlogic/executor/proto"
	pbg "github.com/brotherlogic/goserver/proto"
	"github.com/brotherlogic/goserver/utils"
	pbrc "github.com/brotherlogic/recordcollection/proto"
	rcpb "github.com/brotherlogic/recordcollection/proto"
	pbvs "github.com/brotherlogic/versionserver/proto"
)

const (
	// KEY - where the config is stored
	KEY = "/github.com/brotherlogic/cdprocessor/config"
)

type ripper interface {
	ripToMp3(ctx context.Context, pathIn, pathOut string)
	ripToFlac(ctx context.Context, pathIn, pathOut string)
	runCommand(ctx context.Context, command []string) error
}

type prodRipper struct {
	server func() string
	log    func(s string)
	dial   func(ctx context.Context, server, host string) (*grpc.ClientConn, error)
}

type master interface {
	GetRipped(ctx context.Context, req *pb.GetRippedRequest) (*pb.GetRippedResponse, error)
}

type prodMaster struct {
	dial func(ctx context.Context, server string) (*grpc.ClientConn, error)
}

func (p *prodMaster) GetRipped(ctx context.Context, req *pb.GetRippedRequest) (*pb.GetRippedResponse, error) {
	conn, err := p.dial(ctx, "cdprocessor")
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	client := pb.NewCDProcessorClient(conn)
	return client.GetRipped(ctx, req)
}

func (s *Server) resolve() string {
	return s.Registry.Identifier
}

func (s *Server) fileExists(file string) bool {
	if s.forceCheck {
		return true
	}
	if _, err := os.Stat(file); os.IsNotExist(err) {
		return false
	}
	return true
}

func (pr *prodRipper) ripToMp3(ctx context.Context, pathIn, pathOut string) {
	conn, err := pr.dial(ctx, "executor", pr.server())
	if err != nil {
		return
	}
	defer conn.Close()

	client := pbe.NewExecutorServiceClient(conn)
	resp, err := client.Execute(ctx, &pbe.ExecuteRequest{Command: &pbe.Command{Binary: "lame", Parameters: []string{pathIn, pathOut}}})
	if err != nil {
		pr.log(fmt.Sprintf("MP3ed: %v", err))
	}
	pr.log(fmt.Sprintf("MP3: %v", resp))
}

func (pr *prodRipper) runCommand(ctx context.Context, command []string) error {
	conn, err := pr.dial(ctx, "executor", pr.server())
	if err != nil {
		return err
	}
	defer conn.Close()

	client := pbe.NewExecutorServiceClient(conn)
	pr.log(fmt.Sprintf("Running %v", command))
	_, err = client.QueueExecute(ctx, &pbe.ExecuteRequest{Command: &pbe.Command{Binary: command[0], Parameters: command[1:]}})
	return err
}

func (pr *prodRipper) ripToFlac(ctx context.Context, pathIn, pathOut string) {
	conn, err := pr.dial(ctx, "executor", pr.server())
	if err != nil {
		return
	}
	defer conn.Close()

	client := pbe.NewExecutorServiceClient(conn)
	resp, err := client.Execute(ctx, &pbe.ExecuteRequest{Command: &pbe.Command{Binary: "flac", Parameters: []string{"--best", pathIn}}})
	if err != nil {
		pr.log(fmt.Sprintf("Flaced: %v", err))
	}
	pr.log(fmt.Sprintf("FLAC: %v", resp))
}

type getter interface {
	getRecord(ctx context.Context, id int32) (*pbrc.Record, error)
	updateRecord(ctx context.Context, id int32, cdpath, filepath string) error
}

type prodGetter struct {
	dial       func(ctx context.Context, server string) (*grpc.ClientConn, error)
	log        func(in string)
	lastUpdate time.Time
}

func (rc *prodGetter) getRecord(ctx context.Context, id int32) (*pbrc.Record, error) {
	conn, err := rc.dial(ctx, "recordcollection")
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	client := pbrc.NewRecordCollectionServiceClient(conn)
	resp, err := client.GetRecord(ctx, &pbrc.GetRecordRequest{InstanceId: id})
	if err != nil {
		return nil, err
	}

	return resp.GetRecord(), err
}

func (rc *prodGetter) updateRecord(ctx context.Context, instanceID int32, cdpath, filepath string) error {
	conn, err := rc.dial(ctx, "recordcollection")
	if err != nil {
		return err
	}
	defer conn.Close()

	client := pbrc.NewRecordCollectionServiceClient(conn)
	_, err = client.UpdateRecord(ctx, &pbrc.UpdateRecordRequest{Reason: "cdprocessor update", Update: &pbrc.Record{Release: &pbgd.Release{InstanceId: instanceID}, Metadata: &pbrc.ReleaseMetadata{CdPath: cdpath, FilePath: filepath}}})
	return err
}

type io interface {
	readDir() ([]os.FileInfo, error)
	readSubdir(f string) ([]os.FileInfo, error)
	convert(name string) (int32, int32, error)
}

type rc interface {
	getRecordsInFolder(ctx context.Context, folder int32) ([]*pbrc.Record, error)
}

type prodRc struct {
	log  func(s string)
	dial func(ctx context.Context, server string) (*grpc.ClientConn, error)
}

func (rc *prodRc) getRecordsInFolder(ctx context.Context, folder int32) ([]*pbrc.Record, error) {
	conn, err := rc.dial(ctx, "recordcollection")
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	client := pbrc.NewRecordCollectionServiceClient(conn)
	ids, err := client.QueryRecords(ctx, &pbrc.QueryRecordsRequest{Query: &pbrc.QueryRecordsRequest_FolderId{folder}})
	if err != nil {
		return nil, err
	}

	recs := []*pbrc.Record{}
	for _, id := range ids.GetInstanceIds() {
		rec, err := client.GetRecord(ctx, &pbrc.GetRecordRequest{InstanceId: id})
		if err != nil {
			return nil, err
		}
		recs = append(recs, rec.GetRecord())
	}

	return recs, nil
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

func (i *prodIo) convert(name string) (int32, int32, error) {
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

//Server main server type
type Server struct {
	*goserver.GoServer
	io          io
	rc          rc
	getter      getter
	lastRunTime time.Duration
	adjust      int
	rips        []*pb.Rip
	ripCount    int64
	flacCount   int64
	dir         string
	ripper      ripper
	mp3dir      string
	flacdir     string
	forceCheck  bool
	master      master
	count       int64
	config      *pb.Config
}

// Init builds the server
func Init(dir string, mp3dir string, flacdir string) *Server {
	s := &Server{GoServer: &goserver.GoServer{},
		io:      &prodIo{dir: dir},
		rc:      &prodRc{},
		getter:  &prodGetter{},
		dir:     dir,
		mp3dir:  mp3dir,
		flacdir: flacdir,
	}
	s.rc = &prodRc{dial: s.FDialServer, log: s.Log}
	s.io = &prodIo{dir: dir, log: s.Log}
	s.getter = &prodGetter{log: s.Log, dial: s.FDialServer}
	s.ripper = &prodRipper{log: s.Log, server: s.resolve, dial: s.FDialSpecificServer}
	s.master = &prodMaster{dial: s.FDialServer}

	return s
}

func (s *Server) save(ctx context.Context) {
	s.KSclient.Save(ctx, KEY, s.config)
}

func (s *Server) load(ctx context.Context) error {
	config := &pb.Config{}
	data, _, err := s.KSclient.Read(ctx, KEY, config)

	if err != nil {
		return err
	}

	s.config = data.(*pb.Config)

	if s.config.LastProcessTime == nil {
		s.config.LastProcessTime = make(map[int32]int64)
	}

	return nil
}

// DoRegister does RPC registration
func (s *Server) DoRegister(server *grpc.Server) {
	pb.RegisterCDProcessorServer(server, s)
	rcpb.RegisterClientUpdateServiceServer(server, s)
}

// ReportHealth alerts if we're not healthy
func (s *Server) ReportHealth() bool {
	return true
}

// Shutdown the server
func (s *Server) Shutdown(ctx context.Context) error {
	return nil
}

// Mote promotes/demotes this server
func (s *Server) Mote(ctx context.Context, master bool) error {
	s.buildConfig(ctx)

	masterCount := int64(len(s.rips))
	conn, err := s.FDialServer(ctx, "versionserver")
	if err != nil {
		return err
	}
	defer conn.Close()

	client := pbvs.NewVersionServerClient(conn)
	v, err := client.GetVersion(ctx, &pbvs.GetVersionRequest{Key: "github.com.brotherlogic.cdprocessor"})
	if err != nil {
		return err
	}

	if masterCount < v.Version.Value {
		return status.Errorf(codes.Unavailable, "Not enough rips: %v vs %v", masterCount, v.Version.Value)
	}

	return s.load(ctx)
}

func (s *Server) writeCount(ctx context.Context) error {
	conn, err := s.FDialServer(ctx, "versionserver")
	if err == nil {
		defer conn.Close()
		client := pbvs.NewVersionServerClient(conn)
		client.SetVersion(ctx, &pbvs.SetVersionRequest{Set: &pbvs.Version{Key: "github.com.brotherlogic.cdprocessor", Value: int64(len(s.rips)), Setter: "cdprocessor"}})
	}
	return err
}

// GetState gets the state of the server
func (s *Server) GetState() []*pbg.State {
	return []*pbg.State{}
}

func (s *Server) runVerify(ctx context.Context) error {
	ids := []int32{}
	for _, rip := range s.rips {
		err := s.verify(ctx, rip.Id)
		st, ok := status.FromError(err)
		if ok && st.Code() == codes.DataLoss {
			ids = append(ids, rip.Id)
		}
	}

	if len(ids) > 0 {
		s.RaiseIssue("Problematic rips", fmt.Sprintf("The following ids (%v) are having issues", ids))
	}

	return nil
}

func (s *Server) runLink(ctx context.Context) error {
	s.count = int64(0)
	for _, rip := range s.rips {
		err := s.makeLinks(ctx, rip.Id, false)
		st := status.Convert(err)
		if st.Code() != codes.ResourceExhausted && err != nil {
			return err
		}
		s.count++
	}
	return nil
}

func main() {
	var quiet = flag.Bool("quiet", false, "Show all output")
	var dir = flag.String("dir", "/media/raid/music/rips/", "Base directory for storage location")
	var mp3dir = flag.String("mp3", "/media/raid/music/mp3s/", "Base directory for all mp3s location")
	var flacdir = flag.String("mp3", "/media/raid/music/flacs/", "Base directory for all flacs location")
	var init = flag.Bool("init", false, "Prep server")
	flag.Parse()

	//Turn off logging
	if *quiet {
		log.SetFlags(0)
		log.SetOutput(ioutil.Discard)
	}
	server := Init(*dir, *mp3dir, *flacdir)
	server.PrepServer()
	server.Register = server

	err := server.RegisterServerV2("cdprocessor", false, true)
	if err != nil {
		return
	}

	if *init {
		ctx, cancel := utils.BuildContext("cdprocessor", "cdprocessor")
		defer cancel()

		mapper := make(map[int32]int64)
		mapper[1] = 1
		err := server.KSclient.Save(ctx, KEY, &pb.Config{LastProcessTime: mapper})
		fmt.Printf("Initialised: %v\n", err)
		return
	}

	server.Serve()
}
