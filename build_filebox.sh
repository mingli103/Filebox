#!/bin/bash

# FileBox Educational Toy - Build Script
echo "Building FileBox (Educational Toy)..."

# Build the binary
go build -o filebox main.go filebox.go fid.go

if [ $? -eq 0 ]; then
    echo "✅ FileBox (Educational Toy) built successfully!"
    echo "Run with: ./filebox"
    echo ""
    echo "⚠️  WARNING: This is a toy application for learning purposes only!"
    echo "   Do not use in production environments."
else
    echo "❌ Build failed!"
    exit 1
fi
