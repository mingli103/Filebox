// FileBox Main - Educational Toy Application
//
// This implements a file container approach: files as containers with
// FID-based coordination to prevent duplicate S3 uploads.
//
// WARNING: This is NOT production-ready software.
package main

import (
	"log"
	"net/http"
	"os"
	"strings"
)

func main() {
	// Configuration
	storageDir := os.Getenv("STORAGE_DIR")
	if storageDir == "" {
		storageDir = "./files"
	}

	bucket := os.Getenv("S3_BUCKET")
	if bucket == "" {
		log.Fatal("S3_BUCKET environment variable required")
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	// Parse replicas
	replicasStr := os.Getenv("REPLICAS")
	var replicas []string
	if replicasStr != "" {
		replicas = strings.Split(replicasStr, ",")
		for i, replica := range replicas {
			replicas[i] = strings.TrimSpace(replica)
		}
	}

	// Create FileBox instance
	filebox := NewFileBox(storageDir, bucket, replicas)

	// Register HTTP handlers
	http.HandleFunc("/upload", filebox.handleUpload)
	http.HandleFunc("/blob/", filebox.handleDownload)
	http.HandleFunc("/files", filebox.handleListFiles)
	http.HandleFunc("/replicate", filebox.handleReplicate)

	// Start server
	log.Printf("FileBox (Educational Toy) starting on port %s", port)
	log.Printf("Storage directory: %s", storageDir)
	log.Printf("S3 bucket: %s", bucket)
	log.Printf("Host ID: %s", filebox.hostID)
	if len(replicas) > 0 {
		log.Printf("Replicas: %v", replicas)
	} else {
		log.Printf("No replicas configured")
	}

	log.Fatal(http.ListenAndServe(":"+port, nil))
}
