package main

import (
	"testing"

	"golang.org/x/net/context"
)

func TestLogMissing(t *testing.T) {
	s := Init("testdata")
	s.io = &testIo{dir: "testdata"}
	s.rc = &testRc{}
	gh := &testGh{}
	s.gh = gh
	s.SkipLog = true

	s.logMissing(context.Background())

	if gh.count != 1 {
		t.Errorf("Missing has not been logged")
	}
}

func TestLogMissingFailOnMissing(t *testing.T) {
	s := Init("testdata")
	s.io = &testIo{dir: "testdata", failRead: true}
	s.rc = &testRc{}
	gh := &testGh{}
	s.gh = gh
	s.SkipLog = true

	s.logMissing(context.Background())

	if gh.count > 0 {
		t.Errorf("Failing missing has not failed log")
	}

}

func TestLogMissingFailOnBadLog(t *testing.T) {
	s := Init("testdata")
	s.io = &testIo{dir: "testdata"}
	s.rc = &testRc{}
	gh := &testGh{fail: true}
	s.gh = gh
	s.SkipLog = true

	s.logMissing(context.Background())

	if gh.count > 0 {
		t.Errorf("Failing missing has not failed log:")
	}

}
