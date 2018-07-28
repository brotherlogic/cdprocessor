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
	"github.com/brotherlogic/goserver/utils"
	"golang.org/x/net/context"
	"google.golang.org/grpc"

	pb "github.com/brotherlogic/cdprocessor/proto"
	pbgh "github.com/brotherlogic/githubcard/proto"
	pbg "github.com/brotherlogic/goserver/proto"
	pbrc "github.com/brotherlogic/recordcollection/proto"
	pbvs "github.com/brotherlogic/versionserver/proto"
)

type getter interface {
	getRecord(ctx context.Context, id int32) *pbrc.Record
	updateRecord(ctx context.Context, rec *pbrc.Record)
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
	_, err = client.AddIssue(ctx, &pbgh.Issue{Title: "Rip CD", Body: r.GetRelease().Title, Service: "recordcollection"})
	return err
}

type io interface {
	readDir() ([]os.FileInfo, error)
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
}

func (i *prodIo) readDir() ([]os.FileInfo, error) {
	return ioutil.ReadDir(i.dir)
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
	io     io
	rc     rc
	gh     gh
	getter getter
}

// Init builds the server
func Init(dir string) *Server {
	s := &Server{GoServer: &goserver.GoServer{},
		io: &prodIo{dir: dir},
		rc: &prodRc{},
		gh: &prodGh{}}
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
func (s *Server) Mote(master bool) error {
	resp, err := s.GetRipped(context.Background(), &pb.GetRippedRequest{})
	if err != nil {
		return err
	}
	masterCount := len(resp.Ripped)
	servers, err := utils.ResolveAll("cdprocessor")
	if err != nil {
		return err
	}
	for _, s := range servers {
		conn, err := grpc.Dial(s.Ip+":"+strconv.Itoa(int(s.Port)), grpc.WithInsecure())
		defer conn.Close()
		if err == nil {
			client := pb.NewCDProcessorClient(conn)
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			val, err := client.GetRipped(ctx, &pb.GetRippedRequest{})
			if err == nil {
				if len(val.Ripped) > masterCount {
					return fmt.Errorf("Unable to mote, we have less ripped than %v", s.Identifier)
				}
			}
		}
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
	return []*pbg.State{
		&pbg.State{Key: "count", Value: int64(len(r.Ripped))},
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
	server.RegisterRepeatingTask(server.logMissing, time.Hour)
	server.RegisterRepeatingTask(server.writeCount, time.Hour)
	server.Serve()
}
