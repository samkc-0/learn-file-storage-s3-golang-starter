package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"mime"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/database"
	"github.com/google/uuid"
)

func (cfg *apiConfig) handlerUploadVideo(w http.ResponseWriter, r *http.Request) {
	videoIDString := r.PathValue("videoID")
	videoID, err := uuid.Parse(videoIDString)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid ID", err)
		return
	}

	token, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Coudln't find JWT", err)
		return
	}

	userID, err := auth.ValidateJWT(token, cfg.jwtSecret)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't validate JWT", err)
		return
	}

	fmt.Println("uploading video to S3 bucket", videoID, "by user", userID)

	// set upload limit to 1GB
	oneGigabyte := int64(1 << 30)
	http.MaxBytesReader(w, r.Body, oneGigabyte)

	metadata, err := cfg.db.GetVideo(videoID)
	if metadata.UserID != userID {
		err := errors.New("unauthorized")
		respondWithError(w, http.StatusUnauthorized, "bad credentials", err)
		return
	}

	src, header, err := r.FormFile("video")
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "unable to parse form file", err)
		return
	}

	mimeType, _, err := mime.ParseMediaType(header.Header.Get("Content-Type"))
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "something went wrong parsing media type", err)
		return
	}

	if mimeType != "video/mp4" {
		err := errors.New("media type must be video/mp4")
		respondWithError(w, http.StatusBadRequest, "media type must be video/mp4", err)
	}

	tmpFile, err := os.CreateTemp("", "user_upload.mp4")
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "something went wrong creating temp file", err)
		return
	}

	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	if _, err = io.Copy(tmpFile, src); err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't copy file", err)
		return
	}

	fastStartFilePath, err := processVideoForFastStart(tmpFile.Name())
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "internal server error", err)
	}
	fastStart, err := os.OpenFile(fastStartFilePath, os.O_RDONLY, 0)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "internal server error", err)
		return
	}
	defer fastStart.Close()

	if _, err := fastStart.Seek(0, io.SeekStart); err != nil {
		respondWithError(w, http.StatusInternalServerError, "internal server error", err)
		return
	}

	aspectRatio, err := getVideoAspectRatio(fastStartFilePath)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "internal server error", err)
		return
	}

	awsBucketFolder := "other"
	if aspectRatio == "16:9" {
		fmt.Println(fastStartFilePath + " is a landscape video")
		awsBucketFolder = "landscape"
	} else if aspectRatio == "9:16" {
		fmt.Println(fastStartFilePath + " is a portraint video (mobile)")
		awsBucketFolder = "portrait"
	} else {
		fmt.Println(fastStartFilePath + " is misc aspect ratio")
	}

	if _, err := tmpFile.Seek(0, io.SeekStart); err != nil {
		respondWithError(w, http.StatusInternalServerError, "internal server error", err)
		return
	}

	fileName := awsBucketFolder + "/" + randomFileID() + ".mp4"
	objectParams := s3.PutObjectInput{
		Bucket:      &cfg.s3Bucket,
		Key:         &fileName,
		Body:        fastStart,
		ContentType: &mimeType,
	}

	if _, err = cfg.s3Client.PutObject(r.Context(), &objectParams); err != nil {
		respondWithError(w, http.StatusInternalServerError, "something went wrong uploading file", err)
		return
	}
	videoURL := fmt.Sprintf("https://%s.s3.%s.amazonaws.com/%s", cfg.s3Bucket, cfg.s3Region, fileName)
	videoURL = fmt.Sprintf("https://d1tf4nlelp1n8q.cloudfront.net/%s", cfg.s3Bucket, fileName)

	metadata.VideoURL = &videoURL

	if err = cfg.db.UpdateVideo(metadata); err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't update video", err)
		return
	}
	respondWithJSON(w, http.StatusOK, metadata)
}

func randomFileID() string {
	bytesToGenerateNewFileID := make([]byte, 32)
	rand.Read(bytesToGenerateNewFileID)
	fileNameAsBase64 := base64.RawURLEncoding.EncodeToString(bytesToGenerateNewFileID)
	return fileNameAsBase64
}

func processVideoForFastStart(filepath string) (string, error) {
	outputPath := filepath + ".faststart"
	cmd := exec.Command("ffmpeg", "-i", filepath, "-c", "copy", "-movflags", "faststart", "-f", "mp4", outputPath)
	_, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return outputPath, nil
}

func getVideoAspectRatio(filePath string) (string, error) {
	type result struct {
		Streams []struct {
			Width  int `json:"width"`
			Height int `json:"height"`
		} `json:"streams"`
	}
	cmd := exec.Command("ffprobe", "-v", "error", "-print_format", "json", "-show_streams", filePath)
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}

	var dimensions result
	if err := json.Unmarshal(output, &dimensions); err != nil || len(dimensions.Streams) == 0 {
		return "", err
	}

	eps := 0.01
	w := dimensions.Streams[0].Width
	h := dimensions.Streams[0].Height
	ratio := float64(w) / float64(h)

	if math.Abs(ratio-(16./9.)) <= eps {
		return "16:9", nil
	}

	if math.Abs(ratio-(9./16.)) <= eps {
		return "9:16", nil
	}
	return fmt.Sprintf("%d:%d", w, h), nil
}
