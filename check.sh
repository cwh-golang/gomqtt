#!/usr/bin/env bash

echo 'formatting...'
go fmt ./...

echo 'vetting...'
go vet ./...

echo 'linting...'
golint ./..

echo 'testing...'
go test ./...
