package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestRotateGuideFileUsesHardLinkWhenAvailable(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "xmlguide.xmltv")
	destination := filepath.Join(dir, "xmlguide.20260828.xmltv")
	if err := os.WriteFile(source, []byte("guide"), 0644); err != nil {
		t.Fatal(err)
	}
	linked, err := rotateGuideFile(source, destination, os.Link)
	if err != nil || !linked {
		t.Fatalf("rotate = linked:%v err:%v", linked, err)
	}
	sourceInfo, _ := os.Stat(source)
	destinationInfo, _ := os.Stat(destination)
	if !os.SameFile(sourceInfo, destinationInfo) {
		t.Fatal("rotation did not share the source file data")
	}
}

func TestRotateGuideFileFallsBackToCopy(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "xmlguide.xmltv")
	destination := filepath.Join(dir, "xmlguide.20260828.xmltv")
	if err := os.WriteFile(source, []byte("guide"), 0644); err != nil {
		t.Fatal(err)
	}
	linked, err := rotateGuideFile(source, destination, func(string, string) error { return errors.New("links unavailable") })
	if err != nil || linked {
		t.Fatalf("rotate = linked:%v err:%v", linked, err)
	}
	data, err := os.ReadFile(destination)
	if err != nil || string(data) != "guide" {
		t.Fatalf("copied rotation = %q err:%v", data, err)
	}
}

func TestRotateGuideFilePreservesExistingRotationOnFailure(t *testing.T) {
	dir := t.TempDir()
	destination := filepath.Join(dir, "xmlguide.20260828.xmltv")
	if err := os.WriteFile(destination, []byte("existing guide"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := rotateGuideFile(dir, destination, func(string, string) error { return errors.New("links unavailable") }); err == nil {
		t.Fatal("rotation unexpectedly succeeded for a directory source")
	}
	data, err := os.ReadFile(destination)
	if err != nil || string(data) != "existing guide" {
		t.Fatalf("existing rotation = %q err:%v", data, err)
	}
}
