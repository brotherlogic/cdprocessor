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
	pbgd "github.com/brotherlogic/godiscogs"
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
	getRecord(ctx context.Context, id int32) (*pbrc.Record, error)
	updateRecord(ctx context.Context, rec *pbrc.Record)
}

type prodGetter struct {
	dial func(server string) (*grpc.ClientConn, error)
	log  func(in string)
}

func (rc *prodGetter) getRecord(ctx context.Context, id int32) (*pbrc.Record, error) {
	conn, err := rc.dial("recordcollection")
	if err != nil {
		return nil, err
	}
	defer conn.Close()

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
	conn, err := rc.dial("recordcollection")
	if err != nil {
		return
	}
	defer conn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	client := pbrc.NewRecordCollectionServiceClient(conn)
	_, err = client.UpdateRecord(ctx, &pbrc.UpdateRecordRequest{Update: rec})
	rc.log(fmt.Sprintf("Updated %v (%v)", rec.GetRelease().Id, err))
}

type io interface {
	readDir() ([]os.FileInfo, error)
	readSubdir(f string) ([]os.FileInfo, error)
	convert(name string) (int32, error)
}

type rc interface {
	get(filter *pbrc.Record) (*pbrc.GetRecordsResponse, error)
}

type prodRc struct {
	dial func(server string) (*grpc.ClientConn, error)
}

func (rc *prodRc) get(filter *pbrc.Record) (*pbrc.GetRecordsResponse, error) {
	conn, err := rc.dial("recordcollection")
	if err != nil {
		return &pbrc.GetRecordsResponse{}, err
	}
	defer conn.Close()

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
	s.rc = &prodRc{dial: s.DialMaster}
	s.io = &prodIo{dir: dir, log: s.Log}
	s.getter = &prodGetter{log: s.Log, dial: s.DialMaster}
	s.ripper = &prodRipper{log: s.Log, server: s.resolve, dial: s.DialServer}

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
		return fmt.Errorf("Not enough rips: %v", masterCount)
	}

	return nil
}

func (s *Server) writeCount(ctx context.Context) {
	conn, err := s.DialMaster("versionserver")
	if err == nil {
		defer conn.Close()
		client := pbvs.NewVersionServerClient(conn)
		client.SetVersion(ctx, &pbvs.SetVersionRequest{Set: &pbvs.Version{Key: "github.com.brotherlogic.cdprocessor", Value: int64(len(s.rips)), Setter: "cdprocessor"}})
	}
}

// GetState gets the state of the server
func (s *Server) GetState() []*pbg.State {
	r, _ := s.GetRipped(context.Background(), &pb.GetRippedRequest{})
	m, _ := s.GetMissing(context.Background(), &pb.GetMissingRequest{})

	wavs := int64(0)
	mp3s := int64(0)
	flacs := int64(0)
	for _, rip := range r.Ripped {
		for _, t := range rip.Tracks {
			if len(t.WavPath) > 0 {
				wavs++
			}
			if len(t.Mp3Path) > 0 {
				mp3s++
			}
			if len(t.FlacPath) > 0 {
				flacs++
			}

		}
	}

	missing := 0
	for _, miss := range m.Missing {
		missing = int(miss.GetRelease().Id)
	}

	return []*pbg.State{
		&pbg.State{Key: "count", Value: int64(len(r.Ripped))},
		&pbg.State{Key: "missing", Value: int64(len(m.Missing))},
		&pbg.State{Key: "missing_one", Value: int64(missing)},
		&pbg.State{Key: "adjust", Value: int64(s.adjust)},
		&pbg.State{Key: "wavs", Value: wavs},
		&pbg.State{Key: "mp3s", Value: mp3s},
		&pbg.State{Key: "flacs", Value: flacs},
		&pbg.State{Key: "mp3rips", Value: s.ripCount},
		&pbg.State{Key: "flacrips", Value: s.flacCount},
	}
}

func (s *Server) runVerify(ctx context.Context) {
	s.verify(ctx, 1161277)
}

func (s *Server) runLink(ctx context.Context) {
	for _, rip := range s.rips {
		s.makeLinks(ctx, rip.Id)
	}
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
	server.RegisterRepeatingTask(server.convertToMP3, "rip_mp3s", time.Minute*1)
	server.RegisterRepeatingTask(server.convertToFlac, "rip_flacss", time.Minute*1)
	server.RegisterRepeatingTask(server.runVerify, "run_verify", time.Minute*5)
	server.RegisterRepeatingTask(server.runLink, "run_link", time.Minute*5)

	server.Serve()
}
