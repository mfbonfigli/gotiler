# syntax=docker/dockerfile:1.7

##################################
#         BASE BUILD IMAGE       #
##################################
FROM public.ecr.aws/lts/ubuntu:24.04 AS builder-base

ARG GO_VERSION="1.26.4"
ARG PROJ_VERSION="proj-9.8.1"
ARG PROJECT_FOLDER="/usr/src/gotiler"

RUN apt-get update && DEBIAN_FRONTEND=noninteractive apt-get install -y --fix-missing --no-install-recommends \
    apt-transport-https software-properties-common ca-certificates wget zip unzip curl tar pkg-config \
    git cmake make sqlite3 libsqlite3-dev build-essential \
    && apt-get clean && rm -rf /var/lib/apt/lists/*

RUN . /etc/os-release && wget -q "https://packages.microsoft.com/config/ubuntu/$VERSION_ID/packages-microsoft-prod.deb" \
    && dpkg -i packages-microsoft-prod.deb \
    && rm packages-microsoft-prod.deb \
    && apt-get update && DEBIAN_FRONTEND=noninteractive apt-get install -y powershell \
    && apt-get clean && rm -rf /var/lib/apt/lists/*

WORKDIR /vcpkg
RUN git clone https://github.com/Microsoft/vcpkg.git . \
    && ./bootstrap-vcpkg.sh -disableMetrics

WORKDIR ${PROJECT_FOLDER}
RUN wget -c https://download.osgeo.org/proj/$PROJ_VERSION.tar.gz \
    && tar -xvzf $PROJ_VERSION.tar.gz \
    && mkdir $PROJ_VERSION/build

WORKDIR /tmp
RUN wget https://go.dev/dl/go$GO_VERSION.linux-amd64.tar.gz \
    && rm -rf /usr/local/go \
    && tar -C /usr/local -xzf go$GO_VERSION.linux-amd64.tar.gz

ENV PATH="/usr/local/go/bin:${PATH}"


##################################
#       LINUX AMD64 BUILDER      #
##################################
FROM builder-base AS linux-amd64-builder

RUN /vcpkg/vcpkg install sqlite3[core,tool] tiff --triplet=x64-linux

WORKDIR ${PROJECT_FOLDER}/${PROJ_VERSION}/build

RUN cmake \
    -DCMAKE_TOOLCHAIN_FILE=/vcpkg/scripts/buildsystems/vcpkg.cmake \
    -DVCPKG_TARGET_TRIPLET=x64-linux \
    -DCMAKE_INSTALL_PREFIX=/usr/local/ \
    -DCMAKE_BUILD_TYPE=Release \
    -DBUILD_APPS=OFF \
    -DBUILD_SHARED_LIBS=OFF \
    -DENABLE_CURL=OFF \
    -DENABLE_TIFF=ON \
    -DBUILD_TESTING=OFF \
    -DEMBED_PROJ_DATA_PATH=OFF \
    ..

RUN cmake --build . --config Release -j $(nproc) \
    && cmake --build . --target install -j $(nproc)

WORKDIR ${PROJECT_FOLDER}/build
COPY go.mod go.sum ./
RUN go mod download
COPY . .

RUN mkdir -p \
    /artifacts/linux-amd64/share \
    /artifacts/tests/linux-amd64

# netgo,osusergo keep net and os/user in pure Go: with a -static CGO_LDFLAGS,
# binaries whose only cgo comes from the standard library (e.g. anything using
# net/http, like the s3upload tests) otherwise fail to link, and the static
# executable would depend on the host glibc for name resolution
ENV PKG_CONFIG_PATH="/vcpkg/installed/x64-linux/lib/pkgconfig" \
    CGO_ENABLED="1" \
    CGO_LDFLAGS="-L/vcpkg/installed/x64-linux/lib -g -O2 -static -lstdc++ -lsqlite3 -ltiff -lz -ljpeg -llzma -lm" \
    GOFLAGS="-tags=netgo,osusergo"

ARG VERSION="3.0.0-dev"
ARG GIT_COMMIT="unknown"
RUN go build -o /artifacts/linux-amd64/gotiler -ldflags "-X main.GitCommit=${GIT_COMMIT} -X main.Version=${VERSION}" ./cmd/main.go

# test binaries mirror the package layout: scripts/run-tests.sh runs each one
# from its package directory, where tests find their testdata
RUN for dir in $(go list -f '{{if or .TestGoFiles .XTestGoFiles}}{{.Dir}}{{end}}' ./...); do \
      rel="${dir#$PWD/}"; \
      go test -c -o "/artifacts/tests/linux-amd64/${rel}/pkg.test" "./${rel}" || exit 1; \
    done

RUN cp -r /usr/local/share/proj/. /artifacts/linux-amd64/share/

RUN bash scripts/3p-license-gen.sh --out /artifacts/linux-amd64/THIRD-PARTY-LICENSES.md \
      --notice "PROJ=${PROJECT_FOLDER}/${PROJ_VERSION}/COPYING" \
      --notice "SQLite=/vcpkg/installed/x64-linux/share/sqlite3/copyright" \
      --notice "libtiff=/vcpkg/installed/x64-linux/share/tiff/copyright" \
      --notice "libjpeg-turbo=/vcpkg/installed/x64-linux/share/libjpeg-turbo/copyright" \
      --notice "liblzma (XZ Utils)=/vcpkg/installed/x64-linux/share/liblzma/copyright" \
      --notice "zlib=/vcpkg/installed/x64-linux/share/zlib/copyright" \
      --notice "GNU C Library (glibc)=/usr/share/doc/libc6/copyright" \
      --notice "GNU Lesser General Public License v2.1 (glibc)=/usr/share/common-licenses/LGPL-2.1"


##################################
#       LINUX ARM64 BUILDER      #
##################################
FROM builder-base AS linux-arm64-builder

RUN apt-get update \
    && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends crossbuild-essential-arm64 \
    && apt-get clean \
    && rm -rf /var/lib/apt/lists/*

RUN /vcpkg/vcpkg install sqlite3[core,tool] tiff --triplet=arm64-linux

WORKDIR ${PROJECT_FOLDER}/${PROJ_VERSION}/build

RUN cmake \
    -DCMAKE_TOOLCHAIN_FILE=/vcpkg/scripts/buildsystems/vcpkg.cmake \
    -DVCPKG_TARGET_TRIPLET=arm64-linux \
    -DCMAKE_SYSTEM_NAME=Linux \
    -DCMAKE_SYSTEM_PROCESSOR=aarch64 \
    -DCMAKE_C_COMPILER=aarch64-linux-gnu-gcc \
    -DCMAKE_CXX_COMPILER=aarch64-linux-gnu-g++ \
    -DEXE_SQLITE3=/usr/bin/sqlite3 \
    -DCMAKE_INSTALL_PREFIX=/usr/local/ \
    -DCMAKE_BUILD_TYPE=Release \
    -DBUILD_APPS=OFF \
    -DBUILD_SHARED_LIBS=OFF \
    -DENABLE_CURL=OFF \
    -DENABLE_TIFF=ON \
    -DBUILD_TESTING=OFF \
    -DEMBED_PROJ_DATA_PATH=OFF \
    ..

RUN cmake --build . --config Release -j $(nproc) \
    && cmake --build . --target install -j $(nproc)

WORKDIR ${PROJECT_FOLDER}/build
COPY go.mod go.sum ./
RUN go mod download
COPY . .

RUN mkdir -p \
    /artifacts/linux-arm64/share \
    /artifacts/tests/linux-arm64

# netgo,osusergo: see the linux-amd64 builder
ENV PKG_CONFIG_PATH="/vcpkg/installed/arm64-linux/lib/pkgconfig" \
    CC="aarch64-linux-gnu-gcc" \
    CXX="aarch64-linux-gnu-g++" \
    GOOS="linux" \
    GOARCH="arm64" \
    CGO_ENABLED="1" \
    CGO_LDFLAGS="-L/vcpkg/installed/arm64-linux/lib -g -O2 -static -lstdc++ -lsqlite3 -ltiff -lz -ljpeg -llzma -lm" \
    GOFLAGS="-tags=netgo,osusergo"

ARG VERSION="3.0.0-dev"
ARG GIT_COMMIT="unknown"
RUN go build -o /artifacts/linux-arm64/gotiler -ldflags "-X main.GitCommit=${GIT_COMMIT} -X main.Version=${VERSION}" ./cmd/main.go

# test binaries mirror the package layout: scripts/run-tests.sh runs each one
# from its package directory, where tests find their testdata
RUN for dir in $(go list -f '{{if or .TestGoFiles .XTestGoFiles}}{{.Dir}}{{end}}' ./...); do \
      rel="${dir#$PWD/}"; \
      go test -c -o "/artifacts/tests/linux-arm64/${rel}/pkg.test" "./${rel}" || exit 1; \
    done

RUN cp -r /usr/local/share/proj/. /artifacts/linux-arm64/share/

RUN bash scripts/3p-license-gen.sh --out /artifacts/linux-arm64/THIRD-PARTY-LICENSES.md \
      --notice "PROJ=${PROJECT_FOLDER}/${PROJ_VERSION}/COPYING" \
      --notice "SQLite=/vcpkg/installed/arm64-linux/share/sqlite3/copyright" \
      --notice "libtiff=/vcpkg/installed/arm64-linux/share/tiff/copyright" \
      --notice "libjpeg-turbo=/vcpkg/installed/arm64-linux/share/libjpeg-turbo/copyright" \
      --notice "liblzma (XZ Utils)=/vcpkg/installed/arm64-linux/share/liblzma/copyright" \
      --notice "zlib=/vcpkg/installed/arm64-linux/share/zlib/copyright" \
      --notice "GNU C Library (glibc)=/usr/share/doc/libc6-arm64-cross/copyright" \
      --notice "GNU Lesser General Public License v2.1 (glibc)=/usr/share/common-licenses/LGPL-2.1"


##################################
#      WINDOWS AMD64 BUILDER     #
##################################
FROM builder-base AS windows-amd64-builder

RUN apt-get update \
    && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends mingw-w64 pkg-config \
    && apt-get clean \
    && rm -rf /var/lib/apt/lists/*

ENV VCPKG_DEFAULT_TRIPLET=x64-mingw-static
ENV VCPKG_DEFAULT_HOST_TRIPLET=x64-mingw-static
ENV CMAKE_C_COMPILER=x86_64-w64-mingw32-gcc
ENV CMAKE_CXX_COMPILER=x86_64-w64-mingw32-g++
ENV CMAKE_SYSTEM_NAME=Windows

RUN /vcpkg/vcpkg install sqlite3[core,tool] tiff zlib --triplet=x64-mingw-static

WORKDIR ${PROJECT_FOLDER}/${PROJ_VERSION}/build

RUN cmake \
    -DCMAKE_TOOLCHAIN_FILE=/vcpkg/scripts/buildsystems/vcpkg.cmake \
    -DCMAKE_SYSTEM_NAME=Windows \
    -DVCPKG_TARGET_TRIPLET=x64-mingw-static \
    -DCMAKE_C_COMPILER=x86_64-w64-mingw32-gcc \
    -DCMAKE_CXX_COMPILER=x86_64-w64-mingw32-g++ \
    -DEXE_SQLITE3=/usr/bin/sqlite3 \
    -DCMAKE_INSTALL_PREFIX=/usr/local/ \
    -DCMAKE_BUILD_TYPE=Release \
    -DBUILD_APPS=OFF \
    -DBUILD_SHARED_LIBS=OFF \
    -DENABLE_CURL=OFF \
    -DENABLE_TIFF=ON \
    -DBUILD_TESTING=OFF \
    -DEMBED_PROJ_DATA_PATH=OFF \
    ..

RUN cmake --build . --config Release -j $(nproc) \
    && cmake --build . --target install -j $(nproc)

WORKDIR ${PROJECT_FOLDER}/build
COPY go.mod go.sum ./
RUN go mod download
COPY . .

RUN mkdir -p \
    /artifacts/windows-amd64/share \
    /artifacts/tests/windows-amd64

RUN ln -s /usr/local/lib/libproj.a /usr/local/lib/libproj_9.a

ENV PKG_CONFIG_PATH="/vcpkg/installed/x64-mingw-static/lib/pkgconfig" \
    CC="x86_64-w64-mingw32-gcc" \
    CGO_ENABLED="1" \
    CGO_LDFLAGS="-L/vcpkg/installed/x64-mingw-static/lib -g -O2 -static -lstdc++ -lsqlite3 -ltiff -lzs -ljpeg -llzma -lm" \
    GOOS="windows" \
    GOARCH="amd64"

ARG VERSION="3.0.0-dev"
ARG GIT_COMMIT="unknown"
RUN go build -o /artifacts/windows-amd64/gotiler.exe -ldflags "-X main.GitCommit=${GIT_COMMIT} -X main.Version=${VERSION}" ./cmd/main.go

# test binaries mirror the package layout: scripts/run-tests.sh runs each one
# from its package directory, where tests find their testdata
RUN for dir in $(go list -f '{{if or .TestGoFiles .XTestGoFiles}}{{.Dir}}{{end}}' ./...); do \
      rel="${dir#$PWD/}"; \
      go test -c -o "/artifacts/tests/windows-amd64/${rel}/pkg.test.exe" "./${rel}" || exit 1; \
    done

RUN cp -r /usr/local/share/proj/. /artifacts/windows-amd64/share/

RUN bash scripts/3p-license-gen.sh --out /artifacts/windows-amd64/THIRD-PARTY-LICENSES.md \
      --notice "PROJ=${PROJECT_FOLDER}/${PROJ_VERSION}/COPYING" \
      --notice "SQLite=/vcpkg/installed/x64-mingw-static/share/sqlite3/copyright" \
      --notice "libtiff=/vcpkg/installed/x64-mingw-static/share/tiff/copyright" \
      --notice "libjpeg-turbo=/vcpkg/installed/x64-mingw-static/share/libjpeg-turbo/copyright" \
      --notice "liblzma (XZ Utils)=/vcpkg/installed/x64-mingw-static/share/liblzma/copyright" \
      --notice "zlib=/vcpkg/installed/x64-mingw-static/share/zlib/copyright" \
      --notice "mingw-w64 runtime=/usr/share/doc/mingw-w64-x86-64-dev/copyright"


##################################
#            PACKAGING           #
##################################
FROM scratch AS final

COPY --from=linux-amd64-builder   /artifacts/linux-amd64/   /linux-amd64/
COPY --from=linux-arm64-builder   /artifacts/linux-arm64/   /linux-arm64/
COPY --from=windows-amd64-builder /artifacts/windows-amd64/ /windows-amd64/

COPY --from=linux-amd64-builder   /artifacts/tests/linux-amd64/   /tests/linux-amd64/
COPY --from=linux-arm64-builder   /artifacts/tests/linux-arm64/   /tests/linux-arm64/
COPY --from=windows-amd64-builder /artifacts/tests/windows-amd64/ /tests/windows-amd64/
