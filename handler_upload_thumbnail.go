package main

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/google/uuid"
)

func (cfg *apiConfig) handlerUploadThumbnail(w http.ResponseWriter, r *http.Request) {
	videoIDString := r.PathValue("videoID")
	maxMemory := 10 << 20
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

	parseErr := r.ParseMultipartForm(int64(maxMemory))

	if parseErr != nil {
		respondWithError(w, http.StatusInternalServerError, "Can't pase video", parseErr)
		return
	}

	file, headers, err := r.FormFile("thumbnail")
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Can't parse headers", err)
		return
	}

	mediaType, _, err := mime.ParseMediaType(headers.Header.Get("Content-Type"))
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid Content-Type", err)
		return
	}

	if mediaType != "image/jpeg" && mediaType != "image/png" {
		respondWithError(w, http.StatusBadRequest, "Invalid file type", err)
		return

	}
	defer file.Close()

	video, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Can't get video", err)
		return
	}

	fileExtension := strings.Split(mediaType, "/")

	var random [32]byte
	_, error := rand.Read(random[:])
	if error != nil {
		respondWithError(w, http.StatusInternalServerError, "Can't update thumbnail", err)
		return
	}
	randomString := base64.RawURLEncoding.EncodeToString(random[:])
	path := filepath.Join(cfg.assetsRoot, fmt.Sprintf("%s.%s", randomString, fileExtension[1]))
	newFile, fileErr := os.Create(path)
	if fileErr != nil {
		respondWithError(w, http.StatusInternalServerError, "Can't create file", fileErr)
		return
	}

	defer newFile.Close()
	_, copyErr := io.Copy(newFile, file)
	if copyErr != nil {
		respondWithError(w, http.StatusInternalServerError, "Can't copy file", copyErr)
		return
	}
	url := fmt.Sprintf("http://localhost:%s/%s", cfg.port, path)

	video.ThumbnailURL = &url
	if video.UserID != userID {
		respondWithError(w, http.StatusUnauthorized, "You don't have rights", err)
		return
	}

	updateErr := cfg.db.UpdateVideo(video)
	if updateErr != nil {
		respondWithError(w, http.StatusInternalServerError, "Can't save video", err)
		return
	}

	updatedVideo, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Can't get video", err)
		return
	}

	respondWithJSON(w, http.StatusOK, updatedVideo)
}
