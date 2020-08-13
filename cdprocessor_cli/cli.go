package main

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	"github.com/brotherlogic/goserver/utils"
	"golang.org/x/net/context"

	pbcdp "github.com/brotherlogic/cdprocessor/proto"

	//Needed to pull in gzip encoding init
	_ "google.golang.org/grpc/encoding/gzip"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
	defer cancel()

	conn, err := utils.LFDialServer(ctx, "cdprocessor")
	if err != nil {
		log.Fatalf("Pah: %v", err)
	}
	defer conn.Close()

	registry := pbcdp.NewCDProcessorClient(conn)

	switch os.Args[1] {
	case "force":
		val, _ := strconv.Atoi(os.Args[2])
		resp, err := registry.Force(ctx, &pbcdp.ForceRequest{Type: pbcdp.ForceRequest_RECREATE_LINKS, Id: int32(val)})
		fmt.Printf("%v and %v\n", resp, err)
	case "got":
		resp, err := registry.GetRipped(ctx, &pbcdp.GetRippedRequest{})

		if err == nil {
			fmt.Printf("Got %v ripped records\n", len(resp.GetRipped()))
			i, _ := strconv.Atoi(os.Args[2])
			for _, missing := range resp.GetRipped() {
				if missing.Id == int32(i) {
					fmt.Printf("%v\n", missing)
				}
			}
		} else {
			fmt.Printf("Error: %v\n", err)
		}
	case "missing":
		resp, err := registry.GetMissing(ctx, &pbcdp.GetMissingRequest{})

		if err != nil {
			log.Fatalf("Error in request: %v", err)
		}

		for i, missing := range resp.Missing {
			fmt.Printf("%v. [%v] %v\n", i, missing.GetRelease().Id, missing.GetRelease().Title)
		}
	}
}
