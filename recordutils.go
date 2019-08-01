package main

import (
	"fmt"
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
func TrackExtract(r *pbgd.Release) []*TrackSet {
	trackset := make([]*TrackSet, 0)

	multiFormat := false
	formatCounts := make(map[string]int)
	for _, form := range r.GetFormats() {
		if form.GetName() != "Box Set" {
			formatCounts[form.GetName()]++
		}
	}

	if len(formatCounts) > 1 {
		multiFormat = true
	}

	disk := 1

	currTrack := 1
	if multiFormat {
		currTrack = 1
	}

	currFormat := r.GetFormats()[0].Name
	if currFormat == "Box Set" {
		currFormat = r.GetFormats()[1].Name
	}
	currDisk := "1"[0]

	currStart := "A"[0]
	if r.Tracklist[0].TrackType == pbgd.Track_TRACK {
		currStart = r.Tracklist[0].Position[0]
	} else {
		if r.Tracklist[1].TrackType == pbgd.Track_TRACK {
			currStart = r.Tracklist[1].Position[0]
		} else {
			currStart = r.Tracklist[2].Position[0]
		}
	}

	for _, track := range flatten(r.Tracklist) {
		if track.TrackType == pbgd.Track_TRACK {
			if track.Position[0] != currStart && !multiFormat {
				currStart = track.Position[0]
			}
			if strings.Contains(track.Position, "-") {
				elems := strings.Split(track.Position, "-")

				if strings.HasPrefix(elems[0], "LP") {
					if len(elems[0]) != 2 && elems[0][2] != currDisk {
						if elems[0][2] != currDisk {
							disk++
							currDisk = elems[0][2]
						}
					}
				}
				if strings.HasPrefix(elems[0], "7\"") || strings.HasPrefix(elems[0], "4.72\"") {
					if currFormat != "7inch" {
						disk++
						currTrack = 1
						currFormat = "7inch"
					}
				}
				if strings.HasPrefix(elems[0], "CD") {
					if currFormat != "CD" {
						disk++
						currTrack = 1
						if len(elems[0]) > 2 {
							currDisk = elems[0][2]
						}
					}
					currFormat = "CD"

					if len(elems[0]) != 2 && elems[0][2] != currDisk {
						disk++
						currDisk = elems[0][2]
					}
				}
				_, err := strconv.Atoi(elems[0])
				if err == nil {
					if currDisk != elems[0][0] {
						disk++
						currTrack = 1
						currDisk = elems[0][0]
					}
				}
			}

			if track.Position != "Video" && !strings.HasPrefix(track.Position, "DVD") && !strings.HasPrefix(track.Position, "BD") {
				trackset = append(trackset, &TrackSet{Format: currFormat, Disk: fmt.Sprintf("%v", disk), tracks: []*pbgd.Track{track}, Position: fmt.Sprintf("%v", currTrack)})
				currTrack++
			}
		}
	}

	//Perform la merge
	found := true
	for found {
		found = false
		for i := range trackset[1:] {
			if val, _ := shouldMerge(trackset[i], trackset[i+1]); val {
				trackset[i].tracks = append(trackset[i].tracks, trackset[i+1].tracks...)
				trackset = append(trackset[:i+1], trackset[i+2:]...)
				found = true
				break
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
