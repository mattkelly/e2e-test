FROM golang:1.12.7-alpine3.10

# Add build tools
RUN apk update && \
    apk add --no-cache git

RUN go get -u github.com/golang/dep/cmd/dep
RUN go get -u github.com/onsi/ginkgo
RUN go install github.com/onsi/ginkgo/ginkgo

ENV SRC_DIR=/go/src/github.com/containership/e2e-test/
ENV CGO_ENABLED=0
ENV GOOS=linux
ENV KUBECONFIG=kube.conf

WORKDIR $SRC_DIR

# Install deps before adding rest of source so we can cache the resulting vendor dir
COPY Gopkg.toml Gopkg.lock ./
RUN dep ensure -vendor-only

# Add the source code:
COPY . ./

# Compile test binaries to avoid compiling for each run
RUN ginkgo build -r .

ENTRYPOINT ["./scripts/provision-and-test.sh"]
