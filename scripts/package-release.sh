#!/usr/bin/env bash

set -euo pipefail

APP_NAME="zenssh"
VERSION="${1:-dev}"
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DIST_DIR="${ROOT_DIR}/dist"

build_target() {
	local goos="$1"
	local goarch="$2"
	local out_dir="${DIST_DIR}/${APP_NAME}_${VERSION}_${goos}_${goarch}"
	local archive="${DIST_DIR}/${APP_NAME}_${VERSION}_${goos}_${goarch}.tar.gz"
	local commit
	local date

	commit="$(git -C "${ROOT_DIR}" rev-parse --short HEAD 2>/dev/null || echo none)"
	date="$(date -u +"%Y-%m-%dT%H:%M:%SZ")"

	rm -rf "${out_dir}"
	mkdir -p "${out_dir}"

	CGO_ENABLED=0 GOOS="${goos}" GOARCH="${goarch}" \
		go build \
		-ldflags="-s -w -X zenssh/internal/version.Version=${VERSION} -X zenssh/internal/version.Commit=${commit} -X zenssh/internal/version.Date=${date}" \
		-o "${out_dir}/${APP_NAME}" \
		./cmd/zenssh

	cp "${ROOT_DIR}/README.md" "${out_dir}/README.md"
	tar -C "${DIST_DIR}" -czf "${archive}" "$(basename "${out_dir}")"
}

mkdir -p "${DIST_DIR}"

build_target linux amd64
build_target linux arm64

printf 'Pacotes gerados em %s\n' "${DIST_DIR}"
