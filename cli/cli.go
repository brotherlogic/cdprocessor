package main

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	"github.com/brotherlogic/goserver/utils"
	"golang.org/x/net/context"
	"google.golang.org/grpc"

	pbcdp "github.com/brotherlogic/cdprocessor/proto"

	//Needed to pull in gzip encoding init
	_ "google.golang.org/grpc/encoding/gzip"
)

func main() {
	host, port, err := utils.Resolve("cdprocessor")

	if err != nil {
		log.Fatalf("Unable to locate cdprocessor server")
	}

	conn, _ := grpc.Dial(host+":"+strconv.Itoa(int(port)), grpc.WithInsecure())
	defer conn.Close()

	registry := pbcdp.NewCDProcessorClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	switch os.Args[1] {
	case "got":
		resp, err := registry.GetRipped(ctx, &pbcdp.GetRippedRequest{})

		if err == nil {
			fmt.Printf("Got %v ripped records\n", len(resp.GetRippedIds()))
		} else {
			log.Fatalf("Error: %v", err)
		}
	}
}
