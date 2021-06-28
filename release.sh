#!/bin/bash

ARCH='amd64'

NAME=redis-key-stats

GOOS="linux" GOARCH=$ARCH go build -o $NAME-linux-$ARCH
GOOS="darwin" GOARCH=$ARCH go build -o $NAME-darwin-$ARCH
GOOS="windows" GOARCH=$ARCH go build -o $NAME-windows-$ARCH.exe
