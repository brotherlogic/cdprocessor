package main

import (
	"io/ioutil"
	"log"
	"testing"

	pbgd "github.com/brotherlogic/godiscogs/proto"
	pbrc "github.com/brotherlogic/recordcollection/proto"
	"google.golang.org/protobuf/proto"
)

func TestTrackExtract(t *testing.T) {
	r := &pbgd.Release{
		Title:   "Testing",
		Formats: []*pbgd.Format{&pbgd.Format{Name: "7\""}},
		Tracklist: []*pbgd.Track{
			&pbgd.Track{Title: "Hello", Position: "1A", TrackType: pbgd.Track_TRACK},
			&pbgd.Track{Title: "There", Position: "1B", TrackType: pbgd.Track_TRACK},
		},
	}

	tracks := TrackExtract(r, false)

	if len(tracks) != 1 {
		t.Fatalf("Tracks not extracted")
	}

	if GetTitle(tracks[0]) != "Hello / There" {
		t.Errorf("Unable to get track title: %v", GetTitle(tracks[0]))
	}

}

func TestTrackExtractWithVideo(t *testing.T) {
	r := &pbgd.Release{
		Title:   "Testing",
		Formats: []*pbgd.Format{&pbgd.Format{Name: "7\""}},
		Tracklist: []*pbgd.Track{
			&pbgd.Track{Title: "Hello", Position: "1", TrackType: pbgd.Track_TRACK},
			&pbgd.Track{Title: "There", Position: "Video", TrackType: pbgd.Track_TRACK},
		},
	}

	tracks := TrackExtract(r, false)

	if len(tracks) != 1 {
		t.Fatalf("Tracks not extracted")
	}

	if GetTitle(tracks[0]) != "Hello" {
		t.Errorf("Unable to get track title: %v", GetTitle(tracks[0]))
	}

}

func TestRunExtract(t *testing.T) {
	data, _ := ioutil.ReadFile("cdtests/1018055.file")

	release := &pbgd.Release{}
	proto.Unmarshal(data, release)

	tracks := TrackExtract(release, false)

	if len(tracks) != 13 {
		t.Errorf("Wrong number of tracks: %v", len(tracks))
	}

	for _, tr := range tracks {
		if tr.Position == "9" {
			if GetTitle(tr) != "Town Called Crappy / Solicitor In Studio" {
				t.Errorf("Bad title: %v", GetTitle(tr))
			}
		}
	}

}

func TestRunExtractTatay(t *testing.T) {
	data, _ := ioutil.ReadFile("cdtests/565473.file")

	release := &pbgd.Release{}
	proto.Unmarshal(data, release)

	tracks := TrackExtract(release, false)

	if len(tracks) != 13 {
		t.Errorf("Wrong number of tracks: %v", len(tracks))
	}

	found := false
	for _, tr := range tracks {
		if tr.Position == "13" {
			found = true
			if GetTitle(tr) != "Anna Apera / Gegin Nos / Silff Ffenest / Backward Dog" {
				t.Errorf("Bad title: %v", GetTitle(tr))
			}
		}
	}

	if !found {
		t.Errorf("Track 13 was not found: %+v", tracks[len(tracks)-1])
		for i, tr := range tracks {
			log.Printf("%v. %v (%v-%v)", i, len(tr.tracks), tr.Format, tr.Disk)
			for j, trs := range tr.tracks {
				log.Printf(" %v. %v", j, trs.Title)
			}
		}
	}

}

func TestRunExtractLiveVariousYears(t *testing.T) {
	data, err := ioutil.ReadFile("cdtests/1997688.file")
	if err != nil {
		t.Fatalf("Error: %v", err)
	}

	record := &pbrc.Record{}
	proto.Unmarshal(data, record)

	tracks := TrackExtract(record.GetRelease(), false)

	if len(tracks) != 14 {
		t.Errorf("Wrong number of tracks: %v, from %v", len(tracks), len(record.GetRelease().Tracklist))
	}
}

func TestRunExtractSplitDecisionBand(t *testing.T) {
	data, err := ioutil.ReadFile("cdtests/10313832.data")
	if err != nil {
		t.Fatalf("Error: %v", err)
	}

	record := &pbrc.Record{}
	proto.Unmarshal(data, record)

	tracks := TrackExtract(record.GetRelease(), false)

	if len(tracks) != 24 {
		t.Errorf("Wrong number of tracks: %v, from %v", len(tracks), len(record.GetRelease().Tracklist))
		for i, t := range tracks {
			log.Printf("%v. %v", i, len(t.tracks))
			for j, tr := range t.tracks {
				log.Printf(" %v. %v", j, tr.Title)
			}
		}
	}

	if tracks[23].Format != "CD" {
		t.Errorf("Format was not extracted %+v", tracks[23])
	}
}

func TestRunExtractElektra(t *testing.T) {
	data, err := ioutil.ReadFile("cdtests/4467031.data")
	if err != nil {
		t.Fatalf("Error: %v", err)
	}

	record := &pbrc.Record{}
	proto.Unmarshal(data, record)

	tracks := TrackExtract(record.GetRelease(), false)

	if len(tracks) != 117 {
		t.Errorf("Wrong number of tracks: %v, from %v", len(tracks), len(record.GetRelease().Tracklist))
		for i, t := range tracks {
			log.Printf("%v. %v", i, len(t.tracks))
			for j, tr := range t.tracks {
				log.Printf(" %v. %v", j, tr.Title)
			}
		}
	}

	if tracks[0].Position != "1" || tracks[0].Disk != "1" {
		t.Errorf("Format was not extracted %+v", tracks[0])
	}
}

func TestRunExtractSunRa(t *testing.T) {
	data, err := ioutil.ReadFile("cdtests/1075530.data")
	if err != nil {
		t.Fatalf("Error: %v", err)
	}

	record := &pbrc.Record{}
	proto.Unmarshal(data, record)

	tracks := TrackExtract(record.GetRelease(), false)

	if len(tracks) != 12 || tracks[6].Position != "1" || tracks[6].Disk != "2" {
		for i, tr := range tracks {
			log.Printf("%v. %v (%v-%v)", i, len(tr.tracks), tr.Format, tr.Disk)
			for j, trs := range tr.tracks {
				log.Printf(" %v. %v", j, trs.Title)
			}
		}
		t.Errorf("Bad spec")
	}
}

func TestRunExtractBehindCounter(t *testing.T) {
	data, err := ioutil.ReadFile("cdtests/10404409.data")
	if err != nil {
		t.Fatalf("Error: %v", err)
	}

	record := &pbrc.Record{}
	proto.Unmarshal(data, record)

	tracks := TrackExtract(record.GetRelease(), false)

	if len(tracks) != 39 {
		t.Errorf("Wrong number of tracks: %v, from %v", len(tracks), len(record.GetRelease().Tracklist))
		for i, t := range tracks {
			log.Printf("%v. %v", i, len(t.tracks))
			for j, tr := range t.tracks {
				log.Printf(" %v. %v", j, tr.Title)
			}
		}
	}

	if tracks[0].Format != "Vinyl" || tracks[0].Disk != "1" {
		t.Errorf("First track poor extract (LP, D1): %+v", tracks[0])
	}

	if tracks[38].Position != "10" || tracks[38].Format != "CD" || tracks[38].Disk != "5" {
		t.Errorf("Format was not extracted %+v", tracks[38])
	}
}

func TestRunExtractARMAchines(t *testing.T) {
	data, err := ioutil.ReadFile("cdtests/11060000.data")
	if err != nil {
		t.Fatalf("Error: %v", err)
	}

	record := &pbrc.Record{}
	proto.Unmarshal(data, record)

	tracks := TrackExtract(record.GetRelease(), false)

	if len(tracks) != 100 {
		t.Errorf("Wrong number of tracks: %v, from %v", len(tracks), len(record.GetRelease().Tracklist))
		for i, tr := range tracks {
			log.Printf("%v. %v (%v-%v)", i, len(tr.tracks), tr.Format, tr.Disk)
			for j, trs := range tr.tracks {
				log.Printf(" %v. %v", j, trs.Title)
			}
		}

	}

	if tracks[99].Disk != "10" {
		t.Errorf("Bad disk extract %+v:", tracks[0])
		t.Errorf("Bad disk extract %+v:", tracks[99])
	}
}

func TestRunExtractInfotainment(t *testing.T) {
	data, err := ioutil.ReadFile("cdtests/1310779.data")
	if err != nil {
		t.Fatalf("Error: %v", err)
	}

	record := &pbrc.Record{}
	proto.Unmarshal(data, record)

	tracks := TrackExtract(record.GetRelease(), false)

	if len(tracks) != (12 + 19) {
		t.Errorf("Wrong number of tracks: %v, from %v", len(tracks), len(record.GetRelease().Tracklist))
		for i, t := range tracks {
			log.Printf("%v. %v", i, len(t.tracks))
			for j, tr := range t.tracks {
				log.Printf(" %v. %v", j, tr.Title)
			}
		}
	}

	if tracks[12+19-1].Disk != "2" {
		t.Errorf("Bad disk extract %+v:", tracks[12+19-1])
	}
}

func TestRunExtractLegend(t *testing.T) {
	data, err := ioutil.ReadFile("cdtests/2194660.data")
	if err != nil {
		t.Fatalf("Error: %v", err)
	}

	record := &pbrc.Record{}
	proto.Unmarshal(data, record)

	tracks := TrackExtract(record.GetRelease(), false)

	if len(tracks) != (16 + 13 + 11) {
		t.Errorf("Wrong number of tracks: %v, from %v", len(tracks), len(record.GetRelease().Tracklist))
		for i, t := range tracks {
			log.Printf("%v. %v", i, len(t.tracks))
			for j, tr := range t.tracks {
				log.Printf(" %v. %v", j, tr.Title)
			}
		}
	}

	if tracks[16+13+11-1].Position != "11" {
		t.Errorf("Bad track: %+v", tracks[16+13+11-1])
	}
}

func TestRunExtractSensitive(t *testing.T) {
	data, err := ioutil.ReadFile("cdtests/2417842.data")
	if err != nil {
		t.Fatalf("Error: %v", err)
	}

	record := &pbrc.Record{}
	proto.Unmarshal(data, record)

	tracks := TrackExtract(record.GetRelease(), false)

	if len(tracks) != (14 + 16 + 19 + 16 + 15) {
		t.Errorf("Wrong number of tracks: %v, from %v", len(tracks), len(record.GetRelease().Tracklist))
		for i, t := range tracks {
			log.Printf("%v. %v", i, len(t.tracks))
			for j, tr := range t.tracks {
				log.Printf(" %v. %v", j, tr.Title)
			}
		}
	}
}

func TestRunExtractWillie(t *testing.T) {
	data, err := ioutil.ReadFile("cdtests/3101236.data")
	if err != nil {
		t.Fatalf("Error: %v", err)
	}

	record := &pbrc.Record{}
	proto.Unmarshal(data, record)

	tracks := TrackExtract(record.GetRelease(), false)

	if len(tracks) != (15 + 2) {
		for i, tr := range tracks {
			log.Printf("%v. %v", i, len(tr.tracks))
			for j, trs := range tr.tracks {
				log.Printf(" %v. %v", j, trs.Title)
			}
		}
		t.Fatalf("Wrong number of tracks: %v - should be 17", len(tracks))
	}

	if tracks[15+2-1].Format == "CD" {
		t.Errorf("Bad track (should be Vinyl): %+v", tracks[15+2-1])
	}

}

func TestRunExtractInAHole(t *testing.T) {
	data, err := ioutil.ReadFile("cdtests/4605230.data")
	if err != nil {
		t.Fatalf("Error: %v", err)
	}

	record := &pbrc.Record{}
	proto.Unmarshal(data, record)

	tracks := TrackExtract(record.GetRelease(), false)

	if len(tracks) != (11 + 11) {
		for i, tr := range tracks {
			log.Printf("%v. %v", i, len(tr.tracks))
			for j, trs := range tr.tracks {
				log.Printf(" %v. %v", j, trs.Title)
			}
		}
		t.Fatalf("Wrong number of tracks: %v - should be 22", len(tracks))
	}

	if tracks[11+11-1].Disk != "2" {
		t.Errorf("Bad track: %+v", tracks[11+11-1])
	}

}

func TestRunExtractFiveAlbums(t *testing.T) {
	data, err := ioutil.ReadFile("cdtests/4841901.data")

	if err != nil {
		t.Fatalf("Error: %v", err)
	}

	record := &pbrc.Record{}
	proto.Unmarshal(data, record)

	tracks := TrackExtract(record.GetRelease(), false)

	if len(tracks) != (12 + 18 + 7 + 15 + 14) {
		for i, tr := range tracks {
			log.Printf("%v. %v", i, len(tr.tracks))
			for j, trs := range tr.tracks {
				log.Printf(" %v. %v", j, trs.Title)
			}
		}
		t.Fatalf("Wrong number of tracks: %v - should be 22", len(tracks))
	}

}

func TestRunExtractWitchTrials(t *testing.T) {
	data, err := ioutil.ReadFile("cdtests/603365.data")

	if err != nil {
		t.Fatalf("Error: %v", err)
	}

	record := &pbrc.Record{}
	proto.Unmarshal(data, record)

	tracks := TrackExtract(record.GetRelease(), false)

	if len(tracks) != (20 + 21) {
		for i, tr := range tracks {
			log.Printf("%v. %v", i, len(tr.tracks))
			for j, trs := range tr.tracks {
				log.Printf(" %v. %v", j, trs.Title)
			}
		}
		t.Fatalf("Wrong number of tracks: %v - should be 22", len(tracks))
	}

	if tracks[20+21-1].Disk != "2" {
		t.Errorf("Bad track: %+v", tracks[20+21-1])
	}
}

func TestRunExtractGruppo(t *testing.T) {
	data, err := ioutil.ReadFile("cdtests/782994.data")

	if err != nil {
		t.Fatalf("Error: %v", err)
	}

	record := &pbrc.Record{}
	proto.Unmarshal(data, record)

	tracks := TrackExtract(record.GetRelease(), false)

	if len(tracks) != (9 + 3) {
		for i, tr := range tracks {
			log.Printf("%v. %v (%v-%v)", i, len(tr.tracks), tr.Format, tr.Disk)
			for j, trs := range tr.tracks {
				log.Printf(" %v. %v", j, trs.Title)
			}
		}
		t.Fatalf("Wrong number of tracks: %v - should be 12", len(tracks))
	}

	if tracks[0].Disk != "1" {
		t.Errorf("Bad first track: %+v", tracks[0])
	}

	if tracks[3+9-1].Disk != "2" || tracks[3+9-1].Position != "9" {
		t.Errorf("Bad track (2, 9): %+v", tracks[3+9-1])
		for i, tr := range tracks {
			log.Printf("%v. %v (%v-%v-%v)", i, len(tr.tracks), tr.Format, tr.Disk, tr.Position)
			for j, trs := range tr.tracks {
				log.Printf(" %v. %v", j, trs.Title)
			}
		}
	}

	log.Printf("%+v", tracks[len(tracks)-1])
}

func TestRunExtractSleep(t *testing.T) {
	data, err := ioutil.ReadFile("cdtests/7845100.data")

	if err != nil {
		t.Fatalf("Error: %v", err)
	}

	record := &pbrc.Record{}
	proto.Unmarshal(data, record)

	tracks := TrackExtract(record.GetRelease(), false)

	if len(tracks) != (5 + 6 + 4 + 4 + 4 + 4 + 2 + 2) {
		for i, tr := range tracks {
			log.Printf("%v. %v (%v-%v)", i, len(tr.tracks), tr.Format, tr.Disk)
			for j, trs := range tr.tracks {
				log.Printf(" %v. %v", j, trs.Title)
			}
		}
		t.Fatalf("Wrong number of tracks: %v", len(tracks))
	}
}

func TestRunExtractFloyd(t *testing.T) {
	data, err := ioutil.ReadFile("cdtests/1060844.data")

	if err != nil {
		t.Fatalf("Error: %v", err)
	}

	record := &pbrc.Record{}
	proto.Unmarshal(data, record)

	tracks := TrackExtract(record.GetRelease(), false)

	if len(tracks) != (11 + 11 + 9) {
		for i, tr := range tracks {
			log.Printf("%v. %v (%v-%v)", i, len(tr.tracks), tr.Format, tr.Disk)
			for j, trs := range tr.tracks {
				log.Printf(" %v. %v", j, trs.Title)
			}
		}
		t.Fatalf("Wrong number of tracks: %v", len(tracks))
	}

	if tracks[0].Format != "CD" {
		t.Errorf("Bad track: %+v", tracks[0])
	}
}

func TestRunExtractBunker(t *testing.T) {
	data, err := ioutil.ReadFile("cdtests/10768822.data")

	if err != nil {
		t.Fatalf("Error: %v", err)
	}

	record := &pbrc.Record{}
	proto.Unmarshal(data, record)

	tracks := TrackExtract(record.GetRelease(), false)

	if len(tracks) != (10 + 6 + 7) {
		for i, tr := range tracks {
			log.Printf("%v. %v (%v-%v)", i, len(tr.tracks), tr.Format, tr.Disk)
			for j, trs := range tr.tracks {
				log.Printf(" %v. %v", j, trs.Title)
			}
		}
		t.Fatalf("Wrong number of tracks: %v", len(tracks))
	}

	if tracks[0].Format != "Vinyl" {
		t.Errorf("Bad track format: %+v", tracks[0])
	}
	if tracks[len(tracks)-1].Format != "CD" {
		t.Errorf("Bad track: %+v", tracks[len(tracks)-1])
	}

}

func TestRunExtractIsotach(t *testing.T) {
	data, err := ioutil.ReadFile("cdtests/10844701.data")

	if err != nil {
		t.Fatalf("Error: %v", err)
	}

	record := &pbrc.Record{}
	proto.Unmarshal(data, record)

	tracks := TrackExtract(record.GetRelease(), false)

	if len(tracks) != 20 {
		for i, tr := range tracks {
			log.Printf("%v. %v (%v-%v)", i, len(tr.tracks), tr.Format, tr.Disk)
			for j, trs := range tr.tracks {
				log.Printf(" %v. %v", j, trs.Title)
			}
		}
		t.Fatalf("Wrong number of tracks: %v", len(tracks))
	}

	if tracks[0].Format != "Vinyl" {
		t.Errorf("Bad track: %+v", tracks[0])
	}
	if tracks[len(tracks)-1].Format != "CD" || tracks[len(tracks)-1].Disk != "2" {
		t.Errorf("Bad track: %+v", tracks[len(tracks)-1])
	}

}

func TestRunExtractSndtrak(t *testing.T) {
	data, err := ioutil.ReadFile("cdtests/13723675.data")

	if err != nil {
		t.Fatalf("Error: %v", err)
	}

	record := &pbrc.Record{}
	proto.Unmarshal(data, record)

	tracks := TrackExtract(record.GetRelease(), false)

	if len(tracks) != 9 {
		for i, tr := range tracks {
			log.Printf("%v. %v (%v-%v)", i, len(tr.tracks), tr.Format, tr.Disk)
			for j, trs := range tr.tracks {
				log.Printf(" %v. %v", j, trs.Title)
			}
		}
		t.Fatalf("Wrong number of tracks: %v", len(tracks))
	}

	if tracks[0].Format != "File" {
		t.Errorf("Bad track: %+v", tracks[0])
	}
}

func TestRunExtractBrainticket(t *testing.T) {
	data, err := ioutil.ReadFile("cdtests/2262574.data")

	if err != nil {
		t.Fatalf("Error: %v", err)
	}

	record := &pbrc.Record{}
	proto.Unmarshal(data, record)

	tracks := TrackExtract(record.GetRelease(), false)

	if len(tracks) != 4 {
		for i, tr := range tracks {
			log.Printf("%v. %v (%v-%v)", i, len(tr.tracks), tr.Format, tr.Disk)
			for j, trs := range tr.tracks {
				log.Printf(" %v. %v", j, trs.Title)
			}
		}
		t.Fatalf("Wrong number of tracks: %v", len(tracks))
	}

	if tracks[3].Format != "CD" {
		t.Errorf("Bad track: %+v", tracks[3])
	}
}

func TestRunExtractGrosskopf(t *testing.T) {
	data, err := ioutil.ReadFile("cdtests/2672689.data")

	if err != nil {
		t.Fatalf("Error: %v", err)
	}

	record := &pbrc.Record{}
	proto.Unmarshal(data, record)

	tracks := TrackExtract(record.GetRelease(), false)

	if len(tracks) != 8+11 || tracks[8+11-1].Disk != "2" {
		for i, tr := range tracks {
			log.Printf("%v. %v (%v-%v)", i, len(tr.tracks), tr.Format, tr.Disk)
			for j, trs := range tr.tracks {
				log.Printf(" %v. %v", j, trs.Title)
			}
		}
		t.Fatalf("Wrong number of tracks: %v", len(tracks))
	}
}

func TestRunExtractKreepers(t *testing.T) {
	data, err := ioutil.ReadFile("cdtests/5675438.data")

	if err != nil {
		t.Fatalf("Error: %v", err)
	}

	record := &pbrc.Record{}
	proto.Unmarshal(data, record)

	tracks := TrackExtract(record.GetRelease(), false)

	if len(tracks) != 11 || tracks[len(tracks)-1].Format != "CD" || tracks[len(tracks)-1].Disk != "6" {
		for i, tr := range tracks {
			log.Printf("%v. %v (%v-%v)", i, len(tr.tracks), tr.Format, tr.Disk)
			for j, trs := range tr.tracks {
				log.Printf(" %v. %v", j, trs.Title)
			}
		}
		t.Errorf("Bad spec")
	}
}

func TestRunExtractVannier(t *testing.T) {
	data, err := ioutil.ReadFile("cdtests/3290375.data")

	if err != nil {
		t.Fatalf("Error: %v", err)
	}

	record := &pbrc.Record{}
	proto.Unmarshal(data, record)

	tracks := TrackExtract(record.GetRelease(), false)

	if len(tracks) != 36 || tracks[len(tracks)-1].Format != "CD" || tracks[len(tracks)-1].Disk != "4" {
		for i, tr := range tracks {
			log.Printf("%v. %v (%v-%v)", i, len(tr.tracks), tr.Format, tr.Disk)
			for j, trs := range tr.tracks {
				log.Printf(" %v. %v", j, trs.Title)
			}
		}
		t.Errorf("Bad spec")
	}
}

func TestRunExtractFoster(t *testing.T) {
	data, err := ioutil.ReadFile("cdtests/4422735.data")

	if err != nil {
		t.Fatalf("Error: %v", err)
	}

	record := &pbrc.Record{}
	proto.Unmarshal(data, record)

	tracks := TrackExtract(record.GetRelease(), false)

	if len(tracks) != 22 || tracks[0].Format == "CD" || tracks[0].Disk != "1" {
		for i, tr := range tracks {
			log.Printf("%v. %v (%v-%v)", i, len(tr.tracks), tr.Format, tr.Disk)
			for j, trs := range tr.tracks {
				log.Printf(" %v. %v", j, trs.Title)
			}
		}
		t.Errorf("Bad spec")
	}
}

func TestRunExtractHex(t *testing.T) {
	data, err := ioutil.ReadFile("cdtests/493073.data")

	if err != nil {
		t.Fatalf("Error: %v", err)
	}

	record := &pbrc.Record{}
	proto.Unmarshal(data, record)

	tracks := TrackExtract(record.GetRelease(), false)

	if len(tracks) != 20 || tracks[0].Format != "CD" || tracks[0].Disk != "1" {
		for i, tr := range tracks {
			log.Printf("%v. %v (%v-%v)", i, len(tr.tracks), tr.Format, tr.Disk)
			for j, trs := range tr.tracks {
				log.Printf(" %v. %v", j, trs.Title)
			}
		}
		t.Errorf("Bad spec")
	}
}

func TestRunExtractDarkscorch(t *testing.T) {
	data, err := ioutil.ReadFile("cdtests/5872963.data")

	if err != nil {
		t.Fatalf("Error: %v", err)
	}

	record := &pbrc.Record{}
	proto.Unmarshal(data, record)

	tracks := TrackExtract(record.GetRelease(), false)

	if len(tracks) != 35 || tracks[19].Format != "CD" || tracks[19].Disk != "4" || tracks[18].Format == "CD" {
		for i, tr := range tracks {
			log.Printf("%v. %v (%v-%v)", i, len(tr.tracks), tr.Format, tr.Disk)
			for j, trs := range tr.tracks {
				log.Printf(" %v. %v", j, trs.Title)
			}
		}
		t.Errorf("Bad spec")
	}
}

func TestRunExtractGate(t *testing.T) {
	data, err := ioutil.ReadFile("cdtests/6163105.data")

	if err != nil {
		t.Fatalf("Error: %v", err)
	}

	record := &pbrc.Record{}
	proto.Unmarshal(data, record)

	tracks := TrackExtract(record.GetRelease(), false)

	if len(tracks) != 12 || tracks[11].Format != "CD" || tracks[11].Disk != "2" {
		for i, tr := range tracks {
			log.Printf("%v. %v (%v-%v)", i, len(tr.tracks), tr.Format, tr.Disk)
			for j, trs := range tr.tracks {
				log.Printf(" %v. %v", j, trs.Title)
			}
		}
		t.Errorf("Bad spec")
	}
}

func TestRunExtractBedazzled(t *testing.T) {
	data, err := ioutil.ReadFile("cdtests/8450627.data")

	if err != nil {
		t.Fatalf("Error: %v", err)
	}

	record := &pbrc.Record{}
	proto.Unmarshal(data, record)

	tracks := TrackExtract(record.GetRelease(), false)

	if len(tracks) != 16 || tracks[14].Format != "Vinyl" || tracks[14].Disk != "2" {
		for i, tr := range tracks {
			log.Printf("%v. %v (%v-%v)", i, len(tr.tracks), tr.Format, tr.Disk)
			for j, trs := range tr.tracks {
				log.Printf(" %v. %v", j, trs.Title)
			}
		}
		t.Errorf("Bad spec")
	}
}

func TestRunExtractSkein(t *testing.T) {
	data, err := ioutil.ReadFile("cdtests/2946989.data")

	if err != nil {
		t.Fatalf("Error: %v", err)
	}

	record := &pbrc.Record{}
	proto.Unmarshal(data, record)

	tracks := TrackExtract(record.GetRelease(), false)

	if len(tracks) != 14 || tracks[13].Position != "14" {
		for i, tr := range tracks {
			log.Printf("%v-%v. %v (%v-%v)", i, tr.Position, len(tr.tracks), tr.Format, tr.Disk)
			for j, trs := range tr.tracks {
				log.Printf(" %v. %v", j, trs.Title)
			}
		}
		t.Errorf("Bad spec")
	}
}

func TestRunExtractKnot(t *testing.T) {
	data, err := ioutil.ReadFile("cdtests/1633352.data")

	if err != nil {
		t.Fatalf("Error: %v", err)
	}

	record := &pbrc.Record{}
	proto.Unmarshal(data, record)

	tracks := TrackExtract(record.GetRelease(), false)

	if len(tracks) != 12 || tracks[11].Position != "12" {
		for i, tr := range tracks {
			log.Printf("%v-%v. %v (%v-%v)", i, tr.Position, len(tr.tracks), tr.Format, tr.Disk)
			for j, trs := range tr.tracks {
				log.Printf(" %v. %v", j, trs.Title)
			}
		}
		t.Errorf("Bad spec")
	}

	log.Printf("WHAT %+v", tracks[len(tracks)-1])
}

func TestRunExtractBaird(t *testing.T) {
	data, err := ioutil.ReadFile("cdtests/4192928.data")

	if err != nil {
		t.Fatalf("Error: %v", err)
	}

	record := &pbrc.Record{}
	proto.Unmarshal(data, record)

	tracks := TrackExtract(record.GetRelease(), false)

	if len(tracks) != 18 || tracks[11].Position != "2" {
		for i, tr := range tracks {
			log.Printf("%v-%v. %v (%v-%v)", i, tr.Position, len(tr.tracks), tr.Format, tr.Disk)
			for j, trs := range tr.tracks {
				log.Printf(" %v. %v", j, trs.Title)
			}
		}
		t.Errorf("Bad spec: %v -> %v", len(tracks), tracks[11].Position)
	}

	log.Printf("WHAT %+v", tracks[len(tracks)-1])
}

func TestRunExtractHaint(t *testing.T) {
	data, err := ioutil.ReadFile("cdtests/12182265.data")

	if err != nil {
		t.Fatalf("Error: %v", err)
	}

	record := &pbrc.Record{}
	proto.Unmarshal(data, record)

	tracks := TrackExtract(record.GetRelease(), false)

	if len(tracks) != 4 {
		for i, tr := range tracks {
			log.Printf("%v-%v. %v (%v-%v)", i, tr.Position, len(tr.tracks), tr.Format, tr.Disk)
			for j, trs := range tr.tracks {
				log.Printf(" %v. %v", j, trs.Title)
			}
		}
		t.Errorf("Bad spec: %v -> %v", len(tracks), tracks[1].Position)
	}

	log.Printf("WHAT %+v", tracks[len(tracks)-1])
}

func TestRunExtractBestShow(t *testing.T) {
	data, err := ioutil.ReadFile("cdtests/6897665.data")

	if err != nil {
		t.Fatalf("Error: %v", err)
	}

	record := &pbrc.Record{}
	proto.Unmarshal(data, record)

	tracks := TrackExtract(record.GetRelease(), false)

	if len(tracks) != 153 {
		t.Errorf("Bad tracks: %v", len(tracks))
	}

	for _, t := range tracks {
		log.Printf("%+v", t)
	}
}

func TestRunExtractLantern(t *testing.T) {
	data, err := ioutil.ReadFile("cdtests//32780652.data")
	if err != nil {
		t.Fatalf("Error: %v", err)
	}

	record := &pbrc.Record{}
	proto.Unmarshal(data, record)

	tracks := TrackExtract(record.GetRelease(), false)

	if len(tracks) != 11 {
		for i, tr := range tracks {
			log.Printf("%v. %v", i, len(tr.tracks))
			for j, trs := range tr.tracks {
				log.Printf(" %v. %v", j, trs.Title)
			}
		}
		t.Fatalf("Wrong number of tracks: %v - should be 11", len(tracks))
	}

	for _, track := range tracks {
		if track.Disk != "1" {
			t.Errorf("Should be disk 1: %+v", track)
		}
	}

}
