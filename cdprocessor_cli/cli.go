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
		if e.Master {
			conn, _ := grpc.Dial(e.Ip+":"+strconv.Itoa(int(e.Port)), grpc.WithInsecure())
			defer conn.Close()

			registry := pbcdp.NewCDProcessorClient(conn)

			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()

			switch os.Args[1] {
			case "force":
				val, _ := strconv.Atoi(os.Args[2])
				resp, err := registry.Force(ctx, &pbcdp.ForceRequest{Type: pbcdp.ForceRequest_RECREATE_LINKS, Id: int32(val)})
				fmt.Printf("%v and %v\n", resp, err)
			case "got":
				resp, err := registry.GetRipped(ctx, &pbcdp.GetRippedRequest{})

				if err == nil {
					fmt.Printf("%v: Got %v ripped records\n", e.Identifier, len(resp.GetRipped()))
					i, _ := strconv.Atoi(os.Args[2])
					for _, missing := range resp.GetRipped() {
						if missing.Id == int32(i) {
							fmt.Printf("%v\n", missing)
						}
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
}
