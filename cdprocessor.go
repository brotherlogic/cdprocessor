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

	"github.com/brotherlogic/goserver"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/brotherlogic/cdprocessor/proto"
	pbe "github.com/brotherlogic/executor/proto"
	pbg "github.com/brotherlogic/goserver/proto"
	pbrc "github.com/brotherlogic/recordcollection/proto"
	pbvs "github.com/brotherlogic/versionserver/proto"
)

type ripper interface {
	ripToMp3(ctx context.Context, pathIn, pathOut string)
	ripToFlac(ctx context.Context, pathIn, pathOut string)
	runCommand(ctx context.Context, command []string) error
}

type prodRipper struct {
	server func() string
	log    func(s string)
	dial   func(server, host string) (*grpc.ClientConn, error)
}

type master interface {
	GetRipped(ctx context.Context, req *pb.GetRippedRequest) (*pb.GetRippedResponse, error)
}

type prodMaster struct {
	dial func(server string) (*grpc.ClientConn, error)
}

func (p *prodMaster) GetRipped(ctx context.Context, req *pb.GetRippedRequest) (*pb.GetRippedResponse, error) {
	conn, err := p.dial("cdprocessor")
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
	conn, err := pr.dial("executor", pr.server())
	if err != nil {
		return
	}
	defer conn.Close()

	client := pbe.NewExecutorServiceClient(conn)
	resp, err := client.Execute(ctx, &pbe.ExecuteRequest{Command: &pbe.Command{Binary: "lame", Parameters: []string{pathIn, pathOut}}})
	if err != nil {
		pr.log(fmt.Sprintf("MP3ed: %v", err))
	}
	pr.log(fmt.Sprintf("MP3: %v", resp.CommandOutput))
}

func (pr *prodRipper) runCommand(ctx context.Context, command []string) error {
	conn, err := pr.dial("executor", pr.server())
	if err != nil {
		return err
	}
	defer conn.Close()

	client := pbe.NewExecutorServiceClient(conn)
	pr.log(fmt.Sprintf("Running %v", command))
	_, err = client.Execute(ctx, &pbe.ExecuteRequest{Command: &pbe.Command{Binary: command[0], Parameters: command[1:]}})
	return err
}

func (pr *prodRipper) ripToFlac(ctx context.Context, pathIn, pathOut string) {
	conn, err := pr.dial("executor", pr.server())
	if err != nil {
		return
	}
	defer conn.Close()

	client := pbe.NewExecutorServiceClient(conn)
	resp, err := client.Execute(ctx, &pbe.ExecuteRequest{Command: &pbe.Command{Binary: "flac", Parameters: []string{"--best", pathIn}}})
	if err != nil {
		pr.log(fmt.Sprintf("Flaced: %v", err))
	}
	pr.log(fmt.Sprintf("FLAC: %v", resp.CommandOutput))
}

type getter interface {
	getRecord(ctx context.Context, id int32) ([]*pbrc.Record, error)
	updateRecord(ctx context.Context, rec *pbrc.Record) error
}

type prodGetter struct {
	dial       func(server string) (*grpc.ClientConn, error)
	log        func(in string)
	lastUpdate time.Time
}

func (rc *prodGetter) getRecord(ctx context.Context, id int32) ([]*pbrc.Record, error) {
	conn, err := rc.dial("recordcollection")
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	if time.Now().Sub(rc.lastUpdate) < time.Second*5 {
		time.Sleep(time.Second * 5)
	}
	rc.lastUpdate = time.Now()

	client := pbrc.NewRecordCollectionServiceClient(conn)
	resp, err := client.QueryRecords(ctx, &pbrc.QueryRecordsRequest{Query: &pbrc.QueryRecordsRequest_ReleaseId{id}})
	if err != nil {
		return nil, err
	}

	if len(resp.GetInstanceIds()) > 0 {
		records := []*pbrc.Record{}
		for _, id := range resp.GetInstanceIds() {
			rec, err := client.GetRecord(ctx, &pbrc.GetRecordRequest{InstanceId: id})
			if err != nil {
				return nil, err
			}
			records = append(records, rec.GetRecord())
		}
		return records, nil
	}

	return nil, status.Error(codes.InvalidArgument, fmt.Sprintf("Unable to locate record %v", id))
}

func (rc *prodGetter) updateRecord(ctx context.Context, rec *pbrc.Record) error {
	conn, err := rc.dial("recordcollection")
	if err != nil {
		return err
	}
	defer conn.Close()

	client := pbrc.NewRecordCollectionServiceClient(conn)
	_, err = client.UpdateRecord(ctx, &pbrc.UpdateRecordRequest{Update: rec})
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
	dial func(server string) (*grpc.ClientConn, error)
}

func (rc *prodRc) getRecordsInFolder(ctx context.Context, folder int32) ([]*pbrc.Record, error) {
	conn, err := rc.dial("recordcollection")
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
	forceCheck  bool
	master      master
	count       int64
}

// Init builds the server
func Init(dir string, mp3dir string) *Server {
	s := &Server{GoServer: &goserver.GoServer{},
		io:     &prodIo{dir: dir},
		rc:     &prodRc{},
		getter: &prodGetter{},
		dir:    dir,
		mp3dir: mp3dir,
	}
	s.rc = &prodRc{dial: s.DialMaster, log: s.Log}
	s.io = &prodIo{dir: dir, log: s.Log}
	s.getter = &prodGetter{log: s.Log, dial: s.DialMaster}
	s.ripper = &prodRipper{log: s.Log, server: s.resolve, dial: s.DialServer}
	s.master = &prodMaster{dial: s.DialMaster}

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

// Shutdown the server
func (s *Server) Shutdown(ctx context.Context) error {
	return nil
}

// Mote promotes/demotes this server
func (s *Server) Mote(ctx context.Context, master bool) error {
	s.buildConfig(ctx)

	masterCount := int64(len(s.rips))
	conn, err := s.DialMaster("versionserver")
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
		return fmt.Errorf("Not enough rips: %v vs %v", masterCount, v.Version.Value)
	}

	return nil
}

func (s *Server) writeCount(ctx context.Context) error {
	conn, err := s.DialMaster("versionserver")
	if err == nil {
		defer conn.Close()
		client := pbvs.NewVersionServerClient(conn)
		client.SetVersion(ctx, &pbvs.SetVersionRequest{Set: &pbvs.Version{Key: "github.com.brotherlogic.cdprocessor", Value: int64(len(s.rips)), Setter: "cdprocessor"}})
	}
	return err
}

// GetState gets the state of the server
func (s *Server) GetState() []*pbg.State {
	return []*pbg.State{
		&pbg.State{Key: "run_link_progress", Value: s.count},
		&pbg.State{Key: "run_link_total", Value: int64(len(s.rips))},
		&pbg.State{Key: "adjust", Value: int64(s.adjust)},
		&pbg.State{Key: "flacrips", Value: s.flacCount},
		&pbg.State{Key: "mp3rips", Value: s.ripCount},
	}
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
		s.RaiseIssue(ctx, "Problematic rips", fmt.Sprintf("The following ids (%v) are having issues", ids), false)
	}

	return nil
}

func (s *Server) runLink(ctx context.Context) error {
	s.count = int64(0)
	for _, rip := range s.rips {
		time.Sleep(time.Second)
		err := s.makeLinks(ctx, rip.Id, false)
		if err != nil {
			return err
		}
		s.count++
	}
	return nil
}

func main() {
	var quiet = flag.Bool("quiet", false, "Show all output")
	var dir = flag.String("dir", "/media/music/rips/", "Base directory for storage location")
	var mp3dir = flag.String("mp3", "/media/music/mp3s/", "Base directory for all mp3s location")
	flag.Parse()

	//Turn off logging
	if *quiet {
		log.SetFlags(0)
		log.SetOutput(ioutil.Discard)
	}
	server := Init(*dir, *mp3dir)
	server.PrepServer()
	server.Register = server

	server.RegisterServer("cdprocessor", false)
	server.RegisterRepeatingTask(server.logMissing, "log_missing", time.Hour)
	server.RegisterRepeatingTask(server.writeCount, "write_count", time.Hour)
	server.RegisterRepeatingTask(server.adjustExisting, "adjust_existing", time.Hour)
	server.RegisterRepeatingTask(server.convertToMP3, "rip_mp3s", time.Minute)
	server.RegisterRepeatingTask(server.convertToFlac, "rip_flacss", time.Minute)
	server.RegisterRepeatingTask(server.runVerify, "run_verify", time.Hour)
	server.RegisterRepeatingTask(server.runLink, "run_link", time.Hour)

	server.Serve()
}
