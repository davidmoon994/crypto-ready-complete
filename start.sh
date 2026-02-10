#!/bin/bash

echo "========================================="
echo "  财务管理机器人 - 启动"
echo "========================================="
echo ""

cd "$(dirname "$0")"

echo "📦 下载依赖..."
go mod download

echo "🔨 编译程序..."
go build -o crypto-final cmd/main.go

echo "🚀 启动服务..."
./crypto-final
