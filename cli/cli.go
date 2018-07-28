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
	entries, err := utils.ResolveAll("cdprocessor")

	if err != nil {
		log.Fatalf("Unable to locate cdprocessor server: %v", err)
	}

	for _, e := range entries {
		conn, _ := grpc.Dial(e.Ip+":"+strconv.Itoa(int(e.Port)), grpc.WithInsecure())
		defer conn.Close()

		registry := pbcdp.NewCDProcessorClient(conn)

		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()

		switch os.Args[1] {
		case "got":
			resp, err := registry.GetRipped(ctx, &pbcdp.GetRippedRequest{})

			if err == nil {
				fmt.Printf("%v: Got %v ripped records\n", e.Identifier, len(resp.GetRipped()))
			} else {
				fmt.Printf("%v: Error: %v\n", e.Identifier, err)
			}
		}
	}
}
