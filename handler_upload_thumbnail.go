package main

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"

	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/google/uuid"
)

func (cfg *apiConfig) handlerUploadThumbnail(w http.ResponseWriter, r *http.Request) {
	videoIDString := r.PathValue("videoID")
	videoID, err := uuid.Parse(videoIDString)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid ID", err)
		return
	}

	token, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't find JWT", err)
		return
	}

	userID, err := auth.ValidateJWT(token, cfg.jwtSecret)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't validate JWT", err)
		return
	}

	fmt.Println("uploading thumbnail for video", videoID, "by user", userID)

	tenMegabytes := int64(10 << 20)
	r.ParseMultipartForm(tenMegabytes)

	src, header, err := r.FormFile("thumbnail")
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "unable to parse form file", err)
		return
	}
	defer src.Close()

	metadata, err := cfg.db.GetVideo(videoID)
	if metadata.UserID != userID {
		err := errors.New("unauthorized")
		respondWithError(w, http.StatusUnauthorized, "bad credentials", err)
		return
	}

	mimeType, _, err := mime.ParseMediaType(header.Header.Get("Content-Type"))
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "something went wrong parsing media type", err)
		return
	}

	if !(mimeType == "image/jpeg" || mimeType == "image/png") {
		respondWithError(w, http.StatusBadRequest, "media type must be image/png or image/jpeg, got: "+mimeType, nil)
		return
	}

	bytesToGenerateNewFileID := make([]byte, 32)
	rand.Read(bytesToGenerateNewFileID)
	fileNameAsBase64 := base64.RawURLEncoding.EncodeToString(bytesToGenerateNewFileID)
	filename := fileNameAsBase64 + "." + mimeTypeToExt(mimeType)
	path := filepath.Join(cfg.assetsRoot, filename)
	fmt.Printf("writing to %s\n", path)
	dst, err := os.Create(path)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't create file", err)
		return
	}
	defer dst.Close()

	if _, err = io.Copy(dst, src); err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't copy file", err)
		return
	}
	dataUrl := "http://localhost:" + cfg.port + "/assets/" + filename
	metadata.ThumbnailURL = &dataUrl
	err = cfg.db.UpdateVideo(metadata)

	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't update video", err)
		return
	}
	respondWithJSON(w, http.StatusOK, metadata)

}

func mimeTypeToExt(mimeType string) string {
	switch mimeType {
	case "image/jpeg":
		return "jpg"
	case "image/png":
		return "png"
	default:
		return "bin"
	}
}
