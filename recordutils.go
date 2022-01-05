package main

import (
	"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"

	pbgd "github.com/brotherlogic/godiscogs"
)

// TrackSet is a set of tracks that map to a CD track
type TrackSet struct {
	tracks   []*pbgd.Track
	Position string
	Disk     string
	Format   string
}

func getDisk(pos string) int {
	disk := 1
	if pos[0] == 'C' || pos[0] == 'D' {
		disk = 2
	} else if pos[0] == 'E' || pos[0] == 'F' {
		disk = 3
	} else if pos[0] == 'G' || pos[0] == 'H' {
		disk = 4
	} else if pos[0] == 'I' || pos[0] == 'J' {
		disk = 5
	}
	return disk

}

func getFormatAndDisk(t *pbgd.Track) (string, int) {
	matcher := regexp.MustCompile("^[A-Z]\\d*$")
	if matcher.MatchString(t.Position) {
		return "Vinyl", getDisk(t.Position)
	}

	if strings.HasPrefix(t.Position, "CD") {
		if len(t.Position) > 2 {
			parts := strings.Split(t.Position, "-")
			disk, _ := strconv.Atoi(parts[0][2:])
			return "CD", disk
		}
	}

	if strings.HasPrefix(t.Position, "DVD") {
		if len(t.Position) > 2 {
			parts := strings.Split(t.Position, "-")
			disk, _ := strconv.Atoi(parts[0][3:])
			log.Printf("DISK %v from %v", disk, parts[0][3:])
			return "DVD", disk
		}
	}

	if strings.HasPrefix(t.Position, "BR") {
		if len(t.Position) > 2 {
			parts := strings.Split(t.Position, "-")
			disk, _ := strconv.Atoi(parts[0][2:])
			log.Printf("DISK %v from %v", disk, parts[0][2:])
			return "BR", disk
		}
	}

	if strings.Contains(t.Position, "-") {
		baselines := strings.Split(t.Position, "-")
		matcher = regexp.MustCompile("^\\d+")
		if matcher.MatchString(baselines[0]) && !strings.Contains(baselines[0], "\"") {
			disk, _ := strconv.Atoi(baselines[0])
			return "CD", disk
		}

		if strings.HasPrefix(baselines[0], "7\"") || strings.HasPrefix(baselines[0], "5\"") {
			if len(baselines[0]) > 2 {
				disk, _ := strconv.Atoi(baselines[0][2:])
				return "Vinyl", disk
			}
			return "Vinyl", -1
		}

		if baselines[0] == "Vinyl" || strings.HasPrefix(baselines[0], "LP") ||
			strings.HasPrefix(baselines[0], "4.72") {
			return "Vinyl", getDisk(baselines[1])
		}
	}

	return "Unknown", -1
}

func shouldMerge(t1, t2 *TrackSet) (bool, string) {
	matcher := regexp.MustCompile("^[a-z]")
	if matcher.MatchString(t1.tracks[0].Position) && matcher.MatchString(t2.tracks[0].Position) {
		return true, "^[a-z]"
	}

	cdJoin := regexp.MustCompile("^\\d[A-Z]")
	if cdJoin.MatchString(t1.tracks[0].Position) && cdJoin.MatchString(t2.tracks[0].Position) {
		if t1.tracks[0].Position[0] == t2.tracks[0].Position[0] {
			return true, "^\\d[A-Z]"
		}
	}

	if len(t1.tracks[0].Position) > 1 && len(t2.tracks[0].Position) > 1 {
		combiner := regexp.MustCompile("[a-z]$")
		if combiner.MatchString(t1.tracks[0].Position) && combiner.MatchString(t2.tracks[0].Position) && t1.tracks[0].Position[0] == t2.tracks[0].Position[0] {
			if t1.tracks[len(t1.tracks)-1].Position[len(t1.tracks[len(t1.tracks)-1].Position)-1] == t2.tracks[len(t2.tracks)-1].Position[len(t2.tracks[len(t2.tracks)-1].Position)-1]-1 {
				return true, "[a-z]$"
			}
		}
	}

	// Blah.1 and Blah.2 should be merged
	elems1 := strings.Split(t1.tracks[0].Position, ".")
	check1 := strings.Split(t1.tracks[0].Position, "-")
	elems2 := strings.Split(t2.tracks[0].Position, ".")
	if elems1[0] == elems2[0] && (len(check1) == 1 || len(check1[0]) < len(elems1[0])) {
		return true, "Both contain periods"
	}

	return false, "No Merge"
}

func flatten(tracklist []*pbgd.Track) []*pbgd.Track {
	tracks := make([]*pbgd.Track, 0)
	for _, track := range tracklist {
		tracks = append(tracks, track)
		tracks = append(tracks, flatten(track.SubTracks)...)
	}
	return tracks
}

//TrackExtract extracts a trackset from a release
func TrackExtract(r *pbgd.Release, tape bool) []*TrackSet {
	trackset := make([]*TrackSet, 0)

	if tape {
		return []*TrackSet{
			&TrackSet{Disk: "1",
				Format: "Tape",
				tracks: []*pbgd.Track{
					&pbgd.Track{Title: "Side A"},
				},
				Position: "1",
			},
			&TrackSet{Disk: "1",
				Format: "Tape",
				tracks: []*pbgd.Track{
					&pbgd.Track{Title: "Side B"},
				},
				Position: "2",
			},
		}
	}

	baseFormat := ""
	for _, form := range r.GetFormats() {
		if form.GetName() != "Box Set" && form.GetName() != "All Media" {
			baseFormat = form.GetName()
			if baseFormat == "CDr" {
				baseFormat = "CD"
			}
		}
	}

	currDisk := 0
	readDisk := 0
	currFormat := ""
	currTrack := 1
	for _, track := range flatten(r.Tracklist) {
		if track.TrackType == pbgd.Track_TRACK {
			format, disk := getFormatAndDisk(track)
			if format == "Unknown" {
				format = baseFormat
			}
			if format != currFormat {
				currFormat = format
				readDisk = disk
				currDisk++
			} else if readDisk != disk {
				currDisk++
				readDisk = disk
			}

			if !strings.HasPrefix(track.Position, "Video") && !strings.HasPrefix(track.Position, "DVD") && !strings.HasPrefix(track.Position, "BD") && !strings.HasPrefix(track.Position, "BR") {
				trackset = append(trackset, &TrackSet{Format: currFormat, Disk: fmt.Sprintf("%v", currDisk), tracks: []*pbgd.Track{track}, Position: fmt.Sprintf("%v", currTrack)})
				currTrack++
			}
		}
	}

	//Perform la merge
	found := true
	for found {
		found = false
		if len(trackset) > 1 {
			for i := range trackset[1:] {
				if val, _ := shouldMerge(trackset[i], trackset[i+1]); val {
					trackset[i].tracks = append(trackset[i].tracks, trackset[i+1].tracks...)
					trackset = append(trackset[:i+1], trackset[i+2:]...)
					found = true
					break
				}
			}
		}
	}

	// Rebalance track numbers
	currTrackRe := 1
	currDiskRe := trackset[0].Disk
	for i := range trackset {
		if trackset[i].Disk != currDiskRe {
			currTrackRe = 1
			currDiskRe = trackset[i].Disk
		}
		trackset[i].Position = fmt.Sprintf("%v", currTrackRe)
		currTrackRe++
	}
	return trackset
}

//GetTitle of trackset
func GetTitle(t *TrackSet) string {
	result := t.tracks[0].Title
	for _, tr := range t.tracks[1:] {
		result += " / " + tr.Title
	}
	return result
}
