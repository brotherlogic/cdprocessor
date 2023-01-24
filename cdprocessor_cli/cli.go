package main

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	"github.com/brotherlogic/goserver/utils"

	pbcdp "github.com/brotherlogic/cdprocessor/proto"
	pbrc "github.com/brotherlogic/recordcollection/proto"

	//Needed to pull in gzip encoding init
	_ "google.golang.org/grpc/encoding/gzip"
)

func main() {
	ctx, cancel := utils.ManualContext("cdproc-cli", time.Hour)
	defer cancel()

	conn, err := utils.LFDialServer(ctx, "cdprocessor")
	if err != nil {
		log.Fatalf("Pah: %v", err)
	}
	defer conn.Close()

	registry := pbcdp.NewCDProcessorClient(conn)

	switch os.Args[1] {
	case "sforce":
		val, _ := strconv.ParseInt(os.Args[2], 10, 32)
		client := pbrc.NewClientUpdateServiceClient(conn)
		resp, err := client.ClientUpdate(ctx, &pbrc.ClientUpdateRequest{InstanceId: int32(val)})

		fmt.Printf("%v and %v\n", resp, err)
	case "gforce":
		val, _ := strconv.ParseInt(os.Args[2], 10, 32)
		conn, err := utils.LFDialServer(ctx, "recordcollection")
		if err != nil {
			log.Fatalf("Bad dial: %v", err)
		}
		client := pbrc.NewRecordCollectionServiceClient(conn)
		ids, err := client.QueryRecords(ctx, &pbrc.QueryRecordsRequest{Query: &pbrc.QueryRecordsRequest_ReleaseId{int32(val)}})
		for _, id := range ids.GetInstanceIds() {
			resp, err := registry.Force(ctx, &pbcdp.ForceRequest{Type: pbcdp.ForceRequest_RECREATE_LINKS, Id: int32(id)})
			fmt.Printf("%v and %v\n", resp, err)
		}
	case "force":
		val, _ := strconv.ParseInt(os.Args[2], 10, 32)
		resp, err := registry.Force(ctx, &pbcdp.ForceRequest{Type: pbcdp.ForceRequest_RECREATE_LINKS, Id: int32(val)})
		fmt.Printf("%v and %v\n", resp, err)
	case "got":
		resp, err := registry.GetRipped(ctx, &pbcdp.GetRippedRequest{})

		if err == nil {
			fmt.Printf("Got %v ripped records\n", len(resp.GetRipped()))
			i, _ := strconv.ParseInt(os.Args[2], 10, 32)
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
