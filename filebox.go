// FileBox - Educational Toy Application
//
// This implements a file container approach: files as containers with
// FID-based coordination to prevent duplicate S3 uploads.
//
// WARNING: This is NOT production-ready software.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
)

// FileBox - File container approach
type FileBox struct {
	storageDir    string
	s3Client      *s3.S3
	bucket        string
	maxFileSize   int64
	files         map[string]*ContainerFile
	fileLock      sync.RWMutex
	replicas      []string
	replicaClient *http.Client
	hostID        string
	machineID     uint32
}

// ContainerFile - A file that contains multiple blobs
type ContainerFile struct {
	FID       *FID       `json:"fid"`
	FilePath  string     `json:"file_path"`
	Size      int64      `json:"size"`
	Created   time.Time  `json:"created"`
	Uploaded  bool       `json:"uploaded"`
	Uploading bool       `json:"uploading"`
	Blobs     []BlobInfo `json:"blobs"` // Track individual blobs within the file
}

// BlobInfo - Information about a blob within a container file
type BlobInfo struct {
	ID     string `json:"id"`
	Offset int64  `json:"offset"`
	Length int64  `json:"length"`
	Size   int64  `json:"size"`
}

// BlobResponse - Response for blob operations
type BlobResponse struct {
	ID      string `json:"id"`
	Size    int64  `json:"size"`
	Created string `json:"created"`
	FileID  string `json:"file_id"`
}

// NewFileBox creates a new FileBox instance
func NewFileBox(storageDir, bucket string, replicas []string) *FileBox {
	// Create storage directory
	os.MkdirAll(storageDir, 0755)

	// Initialize S3 client
	sess := session.Must(session.NewSessionWithOptions(session.Options{
		SharedConfigState: session.SharedConfigEnable,
		Profile:           getEnvOrDefault("AWS_PROFILE", "stg-sso-admin"),
	}))
	s3Client := s3.New(sess)

	// Generate unique host ID and machine ID
	hostID := generateHostID()
	machineID := generateMachineID()

	fb := &FileBox{
		storageDir:    storageDir,
		s3Client:      s3Client,
		bucket:        bucket,
		maxFileSize:   100 * 1024 * 1024, // 100MB
		files:         make(map[string]*ContainerFile),
		replicas:      replicas,
		replicaClient: &http.Client{Timeout: 30 * time.Second},
		hostID:        hostID,
		machineID:     machineID,
	}

	// Recover existing files
	fb.recoverFiles()

	log.Printf("FileBox initialized - Host ID: %s, Machine ID: %d", hostID, machineID)
	return fb
}

// generateHostID creates a unique host ID
func generateHostID() string {
	hostname, _ := os.Hostname()
	return fmt.Sprintf("%s-%d", hostname, time.Now().Unix())
}

// generateMachineID creates a unique machine ID for this host
func generateMachineID() uint32 {
	// Use hostname hash as machine ID
	hostname, _ := os.Hostname()
	hash := 0
	for _, c := range hostname {
		hash = hash*31 + int(c)
	}
	return uint32(hash & 0xFFFFFFFF)
}

// getOrCreateContainerFile finds an existing container file or creates a new one
func (fb *FileBox) getOrCreateContainerFile(requiredSpace int64) *ContainerFile {
	fb.fileLock.Lock()
	defer fb.fileLock.Unlock()

	// Find existing file that can accept this blob
	for _, file := range fb.files {
		if !file.Uploaded && !file.Uploading && (file.Size+requiredSpace) <= fb.maxFileSize {
			return file
		}
	}

	// Create new container file
	fid := NewFIDWithMachineID(fb.machineID)
	fidStr := fid.String()
	filePath := filepath.Join(fb.storageDir, fidStr)

	containerFile := &ContainerFile{
		FID:      fid,
		FilePath: filePath,
		Size:     0,
		Created:  time.Now(),
		Blobs:    make([]BlobInfo, 0),
	}

	fb.files[fidStr] = containerFile
	log.Printf("Created new container file: %s (required space: %d bytes)", fidStr, requiredSpace)
	return containerFile
}

// AddBlob adds a blob to a container file
func (fb *FileBox) AddBlob(blobData []byte) (*BlobResponse, error) {
	// Check if blob is too large for any container file
	requiredSpace := int64(len(blobData))
	if requiredSpace > fb.maxFileSize {
		return nil, fmt.Errorf("blob size %d exceeds maximum file size %d", requiredSpace, fb.maxFileSize)
	}

	// Get or create container file with required space
	containerFile := fb.getOrCreateContainerFile(requiredSpace)

	// Double-check that the file can still accept this blob (race condition protection)
	fb.fileLock.RLock()
	currentSize := containerFile.Size
	canFit := (currentSize + requiredSpace) <= fb.maxFileSize
	fb.fileLock.RUnlock()

	if !canFit {
		// File became full between selection and writing, get a new one
		containerFile = fb.getOrCreateContainerFile(requiredSpace)
	}

	// Open file for appending
	file, err := os.OpenFile(containerFile.FilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("error opening container file: %v", err)
	}
	defer file.Close()

	// Write blob data
	offset := containerFile.Size
	length, err := file.Write(blobData)
	if err != nil {
		return nil, fmt.Errorf("error writing blob data: %v", err)
	}

	// Create blob info
	blobID := fmt.Sprintf("%s-%d", containerFile.FID.String(), len(containerFile.Blobs))
	blobInfo := BlobInfo{
		ID:     blobID,
		Offset: offset,
		Length: int64(length),
		Size:   int64(length),
	}

	// Update container file
	fb.fileLock.Lock()
	containerFile.Blobs = append(containerFile.Blobs, blobInfo)
	containerFile.Size += int64(length)
	fb.fileLock.Unlock()

	// Check if file should be uploaded
	if containerFile.Size >= fb.maxFileSize {
		go fb.uploadContainerFile(containerFile.FID.String())
	}

	// Replicate to peers
	go fb.replicateBlob(containerFile.FID.String(), blobData, offset, int64(length))

	return &BlobResponse{
		ID:      blobID,
		Size:    int64(length),
		Created: time.Now().Format(time.RFC3339),
		FileID:  containerFile.FID.String(),
	}, nil
}

// GetBlob retrieves a blob from a container file
func (fb *FileBox) GetBlob(blobID string) ([]byte, error) {
	// Parse blob ID to get file ID and blob index
	parts := strings.Split(blobID, "-")
	if len(parts) < 2 {
		return nil, fmt.Errorf("invalid blob ID format")
	}

	fileID := strings.Join(parts[:len(parts)-1], "-")
	blobIndex := len(parts) - 1

	fb.fileLock.RLock()
	containerFile, exists := fb.files[fileID]
	fb.fileLock.RUnlock()

	if !exists {
		return nil, fmt.Errorf("container file not found: %s", fileID)
	}

	if blobIndex >= len(containerFile.Blobs) {
		return nil, fmt.Errorf("blob index out of range")
	}

	blobInfo := containerFile.Blobs[blobIndex]

	// Read blob data from file
	file, err := os.Open(containerFile.FilePath)
	if err != nil {
		return nil, fmt.Errorf("error opening container file: %v", err)
	}
	defer file.Close()

	// Seek to blob offset
	_, err = file.Seek(blobInfo.Offset, 0)
	if err != nil {
		return nil, fmt.Errorf("error seeking to blob offset: %v", err)
	}

	// Read blob data
	blobData := make([]byte, blobInfo.Length)
	_, err = io.ReadFull(file, blobData)
	if err != nil {
		return nil, fmt.Errorf("error reading blob data: %v", err)
	}

	return blobData, nil
}

// replicateBlob replicates a blob to peer hosts
func (fb *FileBox) replicateBlob(fileID string, blobData []byte, offset, length int64) {
	if len(fb.replicas) == 0 {
		return
	}

	for _, replica := range fb.replicas {
		go func(host string) {
			if err := fb.sendBlobToReplica(host, fileID, blobData, offset, length); err != nil {
				log.Printf("Failed to replicate blob to %s: %v", host, err)
			} else {
				log.Printf("Successfully replicated blob to %s", host)
			}
		}(replica)
	}
}

// sendBlobToReplica sends a blob to a specific replica
func (fb *FileBox) sendBlobToReplica(host, fileID string, blobData []byte, offset, length int64) error {
	url := fmt.Sprintf("http://%s/replicate", host)

	// Create multipart form
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	// Add blob data
	part, err := writer.CreateFormFile("blob", "data")
	if err != nil {
		return err
	}
	part.Write(blobData)

	// Add metadata
	writer.WriteField("file_id", fileID)
	writer.WriteField("offset", fmt.Sprintf("%d", offset))
	writer.WriteField("length", fmt.Sprintf("%d", length))
	writer.WriteField("host_id", fb.hostID)
	writer.WriteField("machine_id", fmt.Sprintf("%d", fb.machineID))

	writer.Close()

	// Send request
	req, err := http.NewRequest("POST", url, &buf)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req = req.WithContext(ctx)

	resp, err := fb.replicaClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("replication failed: %s", string(body))
	}

	return nil
}

// uploadContainerFile uploads a container file to S3
func (fb *FileBox) uploadContainerFile(fileID string) {
	fb.fileLock.RLock()
	containerFile, exists := fb.files[fileID]
	fb.fileLock.RUnlock()

	if !exists || containerFile.Uploaded || containerFile.Uploading {
		return
	}

	// Mark as uploading
	fb.fileLock.Lock()
	containerFile.Uploading = true
	fb.fileLock.Unlock()

	// Generate S3 key (includes machine ID to prevent duplicates)
	s3Key := fmt.Sprintf("files/%d/%s", containerFile.FID.MachineID, fileID)

	// Upload to S3
	file, err := os.Open(containerFile.FilePath)
	if err != nil {
		log.Printf("Error opening file for upload: %v", err)
		return
	}
	defer file.Close()

	_, err = fb.s3Client.PutObject(&s3.PutObjectInput{
		Bucket: aws.String(fb.bucket),
		Key:    aws.String(s3Key),
		Body:   file,
	})

	if err != nil {
		log.Printf("Error uploading file %s to S3: %v", fileID, err)
		// Reset uploading flag on failure
		fb.fileLock.Lock()
		containerFile.Uploading = false
		fb.fileLock.Unlock()
		return
	}

	// Mark as uploaded
	fb.fileLock.Lock()
	containerFile.Uploaded = true
	containerFile.Uploading = false
	fb.fileLock.Unlock()

	log.Printf("Successfully uploaded file %s to S3", fileID)
}

// recoverFiles scans existing files on startup
func (fb *FileBox) recoverFiles() {
	entries, err := os.ReadDir(fb.storageDir)
	if err != nil {
		log.Printf("Error reading storage directory: %v", err)
		return
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		fidStr := entry.Name()
		fid, err := ParseFID(fidStr)
		if err != nil {
			log.Printf("Invalid FID in storage directory: %s", fidStr)
			continue
		}

		// Check if this file was created by this host
		if fid.MachineID != fb.machineID {
			log.Printf("File %s was created by machine %d, not %d", fidStr, fid.MachineID, fb.machineID)
			continue
		}

		filePath := filepath.Join(fb.storageDir, fidStr)
		stat, err := os.Stat(filePath)
		if err != nil {
			continue
		}

		containerFile := &ContainerFile{
			FID:      fid,
			FilePath: filePath,
			Size:     stat.Size(),
			Created:  stat.ModTime(),
			Uploaded: false,
			Blobs:    make([]BlobInfo, 0), // Will be reconstructed on demand
		}

		fb.files[fidStr] = containerFile

		// Queue for upload if not already uploaded
		if !containerFile.Uploaded {
			go fb.uploadContainerFile(fidStr)
		}
	}

	log.Printf("Recovered %d container files", len(fb.files))
}

// HTTP handlers
func (fb *FileBox) handleUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Read blob data
	blobData, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Error reading blob data", http.StatusBadRequest)
		return
	}

	// Add blob to container file
	response, err := fb.AddBlob(blobData)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (fb *FileBox) handleDownload(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	blobID := r.URL.Path[len("/blob/"):]
	if blobID == "" {
		http.Error(w, "Blob ID required", http.StatusBadRequest)
		return
	}

	blobData, err := fb.GetBlob(blobID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Write(blobData)
}

func (fb *FileBox) handleReplicate(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse multipart form
	err := r.ParseMultipartForm(32 << 20)
	if err != nil {
		http.Error(w, "Error parsing form", http.StatusBadRequest)
		return
	}

	// Get blob data
	file, _, err := r.FormFile("blob")
	if err != nil {
		http.Error(w, "Error getting blob", http.StatusBadRequest)
		return
	}
	defer file.Close()

	blobData, err := io.ReadAll(file)
	if err != nil {
		http.Error(w, "Error reading blob data", http.StatusBadRequest)
		return
	}

	// Get metadata
	fileID := r.FormValue("file_id")
	offsetStr := r.FormValue("offset")
	lengthStr := r.FormValue("length")
	hostID := r.FormValue("host_id")

	if fileID == "" || offsetStr == "" || lengthStr == "" {
		http.Error(w, "Missing required fields", http.StatusBadRequest)
		return
	}

	var offset, length int64
	fmt.Sscanf(offsetStr, "%d", &offset)
	fmt.Sscanf(lengthStr, "%d", &length)

	// Create or get container file
	fb.fileLock.Lock()
	containerFile, exists := fb.files[fileID]
	if !exists {
		// Create new container file for replication
		fid, err := ParseFID(fileID)
		if err != nil {
			fb.fileLock.Unlock()
			http.Error(w, "Invalid file ID", http.StatusBadRequest)
			return
		}

		filePath := filepath.Join(fb.storageDir, fileID)
		containerFile = &ContainerFile{
			FID:      fid,
			FilePath: filePath,
			Size:     0,
			Created:  time.Now(),
			Blobs:    make([]BlobInfo, 0),
		}
		fb.files[fileID] = containerFile
	}
	fb.fileLock.Unlock()

	// Write blob data to file at specified offset
	fileHandle, err := os.OpenFile(containerFile.FilePath, os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		http.Error(w, "Error opening file", http.StatusInternalServerError)
		return
	}
	defer fileHandle.Close()

	_, err = fileHandle.Seek(offset, 0)
	if err != nil {
		http.Error(w, "Error seeking to offset", http.StatusInternalServerError)
		return
	}

	_, err = fileHandle.Write(blobData)
	if err != nil {
		http.Error(w, "Error writing blob data", http.StatusInternalServerError)
		return
	}

	// Update container file size
	fb.fileLock.Lock()
	if offset+length > containerFile.Size {
		containerFile.Size = offset + length
	}
	fb.fileLock.Unlock()

	log.Printf("Replicated blob from %s to file %s at offset %d", hostID, fileID, offset)
	w.WriteHeader(http.StatusOK)
}

func (fb *FileBox) handleListFiles(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	fb.fileLock.RLock()
	files := make([]*ContainerFile, 0, len(fb.files))
	for _, file := range fb.files {
		files = append(files, file)
	}
	fb.fileLock.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(files)
}

// Helper function
func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
