# FileBox

> **⚠️ Educational Project**: This is a toy application created for learning distributed blob storage concepts. It is not production-ready and should not be used in production environments.

A minimal implementation demonstrating file containers with FID-based coordination for efficient blob storage.

## 🎯 How FileBox Works

FileBox implements a file container architecture for efficient blob storage:

### **📁 File Container Approach**

- **Container Files**: Each file contains multiple blobs
- **FID with Machine ID**: Each container file has a unique FID containing the creator's machine ID
- **No Coordination Needed**: Each host only uploads files it created
- **Natural Bundling**: Multiple blobs per S3 object = cost savings
- **Smart Space Management**: Automatically creates new files when existing ones can't fit new blobs

### **🔄 How It Works**

1. **Blob Upload**:
   - Host 1 receives blob → writes to container file with Host 1's machine ID
   - Host 1 replicates blob to Host 2 → Host 2 writes to container file with Host 1's machine ID

2. **S3 Upload**:
   - Host 1 uploads its container file to S3 (key includes Host 1's machine ID)
   - Host 2 uploads its container file to S3 (key includes Host 1's machine ID - same!)
   - **S3 deduplicates** identical content automatically

3. **No Duplicates**:
   - Same FID across all hosts → Same S3 key
   - Identical content → S3 deduplication
   - No coordination needed → Each host uploads independently

## 🚀 Quick Start

### **Build FileBox**

```bash
./build_filebox.sh
```

### **Run Single Host**

```bash
export S3_BUCKET="your-bucket"
export AWS_PROFILE="example-profile"
./filebox
```

### **Run Multiple Hosts (Replication)**

**Host 1:**

```bash
export S3_BUCKET="your-bucket"
export REPLICAS="host2:8080,host3:8080"
./filebox
```

**Host 2:**

```bash
export S3_BUCKET="your-bucket"
export REPLICAS="host1:8080,host3:8080"
./filebox
```

**Host 3:**

```bash
export S3_BUCKET="your-bucket"
export REPLICAS="host1:8080,host2:8080"
./filebox
```

## 📡 API Endpoints

- **POST /upload** - Upload blob to container file
- **GET /blob/{id}** - Download blob from container file
- **GET /files** - List all container files
- **POST /replicate** - Internal endpoint for replication

## 🏗️ Architecture

```
Upload Request → Container File → Replicate to Peers → Upload to S3
     ↓              ↓              ↓                ↓
  Generate FID   Multiple Blobs   Same FID        Host-specific Key
  (Machine ID)   in One File      Across Hosts    (Prevents Duplicates)
```

## 💡 Key Benefits

### **✅ Efficient Storage**

- **No coordination needed** - Each host uploads its own files
- **No race conditions** - Each host owns its container files
- **No duplicate uploads** - S3 deduplicates identical content

### **✅ Cost Savings**

- **Natural bundling** - Multiple blobs per S3 object
- **S3 deduplication** - Identical content stored once
- **Fewer PUT operations** - 1000 blobs → ~10 container files → 10 S3 PUTs

### **✅ Simple & Reliable**

- **Proven approach** - Based on file container architecture
- **Self-healing** - Automatic recovery on startup
- **No complex coordination** - Each host operates independently

### **✅ Smart Space Management**

- **Automatic file creation** - Creates new files when existing ones are full
- **Blob size validation** - Rejects blobs larger than max file size
- **Race condition protection** - Double-checks space availability before writing
- **Efficient space usage** - Maximizes container file utilization

## 📊 Example Flow

1. **Blob A uploaded to Host 1**:

   ```
   Host 1: container-001 (FID: 12345-001-0001, Machine ID: 1)
   ├── Blob A (offset 0-1000)
   ```

2. **Replication to Host 2 & 3**:

   ```
   Host 2: container-001 (FID: 12345-001-0001, Machine ID: 1) - Same FID!
   ├── Blob A (offset 0-1000)
   
   Host 3: container-001 (FID: 12345-001-0001, Machine ID: 1) - Same FID!
   ├── Blob A (offset 0-1000)
   ```

3. **S3 Upload**:

   ```
   All hosts upload to: s3://bucket/files/1/12345-001-0001
   S3 deduplicates identical content automatically
   ```

## 🎯 The Magic

The key insight is that **each host maintains its own copy of the same container files**. When it's time to upload, each host uploads its own copy to S3. The FID contains the original creator's machine ID, so all hosts upload to the same S3 key, and S3 handles deduplication.

This provides efficient storage with natural bundling benefits! 🎯
