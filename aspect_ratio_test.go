package main

import (
	"path/filepath"
	"testing"
)

func TestAspectRatio(t *testing.T) {
	filePath := filepath.Join("samples", "boots-video-vertical.mp4")

	aspectRatio, err := getVideoAspectRatio(filePath)
	if err != nil {
		t.Fatal(err)
	}
	if aspectRatio != "9:16" {
		t.Errorf("Expected 9:16, got %s", aspectRatio)
	}
}
