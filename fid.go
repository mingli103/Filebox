// FID (File ID) system for FileBox
//
// This is part of an educational toy application for learning blob storage concepts.
// WARNING: This is NOT production-ready software.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync/atomic"
	"time"
)

// FID represents a File ID with embedded metadata
type FID struct {
	MachineID uint32  // Machine that created this file
	Timestamp int64   // Unix timestamp when created
	Sequence  uint32  // Sequence number
	Hash      [8]byte // Hash for integrity
}

// Global sequence counter for FID generation
var sequenceCounter uint32

// NewFID creates a new FID with current timestamp
func NewFID() *FID {
	// Generate random machine ID (in real system, this would be configured)
	machineID := uint32(time.Now().UnixNano() % 1000000)
	return NewFIDWithMachineID(machineID)
}

// NewFIDWithMachineID creates a new FID with specific machine ID
func NewFIDWithMachineID(machineID uint32) *FID {
	fid := &FID{}
	fid.GenerateWithMachineID(machineID)
	return fid
}

// GenerateWithMachineID generates FID with specific machine ID
func (f *FID) GenerateWithMachineID(machineID uint32) {
	now := time.Now().Unix()
	seq := atomic.AddUint32(&sequenceCounter, 1)

	f.Timestamp = now
	f.Sequence = seq
	f.MachineID = machineID

	// Generate hash
	hash := fmt.Sprintf("%d-%d-%d", now, seq, machineID)
	h := sha256.Sum256([]byte(hash))
	copy(f.Hash[:], h[:8])
}

// String returns the FID as a hex string
func (f *FID) String() string {
	return fmt.Sprintf("%08x%08x%08x%08x",
		f.MachineID, f.Timestamp, f.Sequence,
		uint32(f.Hash[0])<<24|uint32(f.Hash[1])<<16|uint32(f.Hash[2])<<8|uint32(f.Hash[3]))
}

// ParseFID parses a hex string back into a FID
func ParseFID(fidStr string) (*FID, error) {
	if len(fidStr) != 32 {
		return nil, fmt.Errorf("invalid FID length: expected 32, got %d", len(fidStr))
	}

	// Parse hex string
	bytes, err := hex.DecodeString(fidStr)
	if err != nil {
		return nil, fmt.Errorf("invalid hex in FID: %v", err)
	}

	if len(bytes) != 16 {
		return nil, fmt.Errorf("invalid FID byte length: expected 16, got %d", len(bytes))
	}

	fid := &FID{
		MachineID: uint32(bytes[0])<<24 | uint32(bytes[1])<<16 | uint32(bytes[2])<<8 | uint32(bytes[3]),
		Timestamp: int64(uint32(bytes[4])<<24 | uint32(bytes[5])<<16 | uint32(bytes[6])<<8 | uint32(bytes[7])),
		Sequence:  uint32(bytes[8])<<24 | uint32(bytes[9])<<16 | uint32(bytes[10])<<8 | uint32(bytes[11]),
	}

	copy(fid.Hash[:], bytes[12:16])

	return fid, nil
}

// Machine returns the machine ID
func (f *FID) Machine() uint32 {
	return f.MachineID
}

// Created returns when this FID was created
func (f *FID) Created() time.Time {
	return time.Unix(f.Timestamp, 0)
}

// IsValid checks if the FID is valid
func (f *FID) IsValid() bool {
	// Check if timestamp is reasonable (not too old, not in future)
	now := time.Now().Unix()
	return f.Timestamp > now-86400*365 && f.Timestamp <= now+3600 // Within 1 year, not more than 1 hour in future
}

// S3Key returns the S3 key for this FID
func (f *FID) S3Key() string {
	// Use a hierarchical structure: machine/timestamp/sequence
	return fmt.Sprintf("files/%d/%d/%s", f.MachineID, f.Timestamp, f.String())
}
