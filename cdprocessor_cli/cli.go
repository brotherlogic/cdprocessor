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
				for i, missing := range resp.GetRipped() {
					fmt.Printf("%v. %v\n", i, missing.Id)
				}
			} else {
				fmt.Printf("%v: Error: %v\n", e.Identifier, err)
			}
		case "missing":
			if e.Master {
				resp, err := registry.GetMissing(ctx, &pbcdp.GetMissingRequest{})

				if err != nil {
					log.Fatalf("Error in request: %v", err)
				}

				for i, missing := range resp.Missing {
					fmt.Printf("%v. [%v] %v\n", i, missing.GetRelease().Id, missing.GetRelease().Title)
				}
			}
		}
	}
}
