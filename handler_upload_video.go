package main

import (
	"bytes"
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
	"path/filepath"
	"strings"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/google/uuid"
)

func (cfg *apiConfig) handlerUploadVideo(w http.ResponseWriter, r *http.Request) {
	videoLimit := 1 << 30
	r.Body = http.MaxBytesReader(w, r.Body, int64(videoLimit))
	videoIdStg := r.PathValue("videoID")
	videoID, err := uuid.Parse(videoIdStg)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid Id", err)
	}

	token, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Can't parse JWT", err)
		return
	}

	userID, err := auth.ValidateJWT(token, cfg.jwtSecret)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Can't validate JWT", err)
		return
	}
	dbVideo, err := cfg.db.GetVideo(videoID)
	if userID != dbVideo.UserID {
		respondWithError(w, http.StatusUnauthorized, "Don't have permission", err)
		return
	}

	file, handler, err := r.FormFile("video")
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Can't parse video", err)
		return
	}
	defer file.Close()
	contentType := handler.Header.Get("Content-Type")
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil || mediaType != "video/mp4" {
		respondWithError(w, http.StatusBadRequest, "Invalid media type", err)
		return
	}

	tempFile, err := os.CreateTemp("", "tubely-upload.mp4")
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Can't create temp file", err)
		return
	}
	defer tempFile.Close()
	defer os.Remove(tempFile.Name())

	_, copyError := io.Copy(tempFile, file)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error during file write", copyError)
		return
	}

	tempFilePath, err := filepath.Abs(tempFile.Name())
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "can't get path", err)
	}
	aspectRation, err := getVideoAspectRatio(tempFilePath)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "can't get video aspectRation", err)
		return
	}
	_, err = tempFile.Seek(0, io.SeekStart)

	var random [32]byte
	_, error := rand.Read(random[:])
	if error != nil {
		respondWithError(w, http.StatusInternalServerError, "Can't generate random name", error)
		return
	}
	fileExtension := strings.Split(mediaType, "/")
	randomFileName := base64.RawURLEncoding.EncodeToString(random[:])
	fileName := fmt.Sprintf("%s/%s.%s", aspectRation, randomFileName, fileExtension[1])

	_, s3Error := cfg.s3Client.PutObject(context.TODO(), &s3.PutObjectInput{
		Bucket:      &cfg.s3Bucket,
		Key:         &fileName,
		Body:        tempFile,
		ContentType: &mediaType,
	})
	if s3Error != nil {
		respondWithError(w, http.StatusInternalServerError, "Can't upload video to s3", s3Error)
		return
	}
	videoUrl := fmt.Sprintf("https://%s.s3.%s.amazonaws.com/%s", cfg.s3Bucket, cfg.s3Region, fileName)
	dbVideo.VideoURL = &videoUrl
	dbError := cfg.db.UpdateVideo(dbVideo)
	if dbError != nil {
		respondWithError(w, http.StatusInternalServerError, "Can't update videoUrl", err)
		return
	}
}

func getVideoAspectRatio(filePath string) (string, error) {
	type FFProbeOutput struct {
		Streams []struct {
			Width  int `json:"width"`
			Height int `json:"height"`
		}
	}

	command := exec.Command("ffprobe", "-v", "error", "-print_format", "json", "-show_streams", filePath)
	var buffer bytes.Buffer
	command.Stdout = &buffer
	err := command.Run()
	if err != nil {
		return "", errors.New("can't run ffprobe ")
	}
	var result FFProbeOutput

	umarshalError := json.Unmarshal(buffer.Bytes(), &result)
	if umarshalError != nil {
		return "Can't parse the result", umarshalError
	}
	info := result.Streams[0]

	ratio := float64(info.Width) / float64(info.Height)

	if math.Abs(ratio-16.0/9.0) < 0.1 {
		return "landscape", nil
	}

	if math.Abs(ratio-9.0/16.0) < 0.1 {
		return "portrait", nil
	}

	return "other", nil
}

func processVideoForFastStart(filepath string) (string, error) {
	outputFilePath := fmt.Sprintf("/.processing/%s", filepath)
	command := exec.Command("ffprobe", "-i", "-c", "copy", "-movflags", "faststart", "-f", "mp4", outputFilePath)
	err := command.Run()
	if err != nil {
		return "can't process fast start video", err
	}
	return outputFilePath, nil
}
