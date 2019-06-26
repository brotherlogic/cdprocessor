package main

import (
	"io/ioutil"
	"log"
	"testing"

	pbgd "github.com/brotherlogic/godiscogs"
	pbrc "github.com/brotherlogic/recordcollection/proto"
	"github.com/golang/protobuf/proto"
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

	tracks := TrackExtract(r)

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

	tracks := TrackExtract(r)

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

	tracks := TrackExtract(release)

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

	tracks := TrackExtract(release)

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
		t.Errorf("Track 13 was not found")
	}

}

func TestRunExtractLiveVariousYears(t *testing.T) {
	data, err := ioutil.ReadFile("cdtests/1997688.file")
	if err != nil {
		t.Fatalf("Error: %v", err)
	}

	record := &pbrc.Record{}
	proto.Unmarshal(data, record)

	tracks := TrackExtract(record.GetRelease())

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

	tracks := TrackExtract(record.GetRelease())

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

	tracks := TrackExtract(record.GetRelease())

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

	tracks := TrackExtract(record.GetRelease())

	if len(tracks) != 12 {
		t.Errorf("Wrong number of tracks: %v, from %v", len(tracks), len(record.GetRelease().Tracklist))
		for i, t := range tracks {
			log.Printf("%v. %v", i, len(t.tracks))
			for j, tr := range t.tracks {
				log.Printf(" %v. %v", j, tr.Title)
			}
		}
	}

	if tracks[6].Position != "1" || tracks[6].Disk != "2" {
		t.Errorf("Format was not extracted %+v", tracks[6])
	}
}

func TestRunExtractBehindCounter(t *testing.T) {
	data, err := ioutil.ReadFile("cdtests/10404409.data")
	if err != nil {
		t.Fatalf("Error: %v", err)
	}

	record := &pbrc.Record{}
	proto.Unmarshal(data, record)

	tracks := TrackExtract(record.GetRelease())

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
		t.Errorf("First track poor extract: %+v", tracks[0])
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

	tracks := TrackExtract(record.GetRelease())

	if len(tracks) != 100 {
		t.Errorf("Wrong number of tracks: %v, from %v", len(tracks), len(record.GetRelease().Tracklist))
		for i, t := range tracks {
			log.Printf("%v. %v", i, len(t.tracks))
			for j, tr := range t.tracks {
				log.Printf(" %v. %v", j, tr.Title)
			}
		}
	}

	if tracks[99].Disk != "10" {
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

	tracks := TrackExtract(record.GetRelease())

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

	tracks := TrackExtract(record.GetRelease())

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

	tracks := TrackExtract(record.GetRelease())

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

	tracks := TrackExtract(record.GetRelease())

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
		t.Errorf("Bad track: %+v", tracks[15+2-1])
	}

}

func TestRunExtractInAHole(t *testing.T) {
	data, err := ioutil.ReadFile("cdtests/4605230.data")
	if err != nil {
		t.Fatalf("Error: %v", err)
	}

	record := &pbrc.Record{}
	proto.Unmarshal(data, record)

	tracks := TrackExtract(record.GetRelease())

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
