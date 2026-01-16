ARG GO_VERSION
ARG IMG_TAG=latest

# Compile the atomoned binary
FROM golang:$GO_VERSION-alpine AS testapp-builder
RUN apk add --no-cache git openssh ca-certificates
WORKDIR /src/app/
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
ENV PACKAGES curl make git libc-dev bash gcc linux-headers eudev-dev python3
RUN apk add --no-cache $PACKAGES
RUN CGO_ENABLED=0 make build

# Add to a distroless container
FROM alpine:$IMG_TAG
RUN adduser -D nonroot
ARG IMG_TAG
COPY --from=testapp-builder /src/app/build/consumer /usr/local/bin/
COPY --from=testapp-builder /src/app/build/provider /usr/local/bin/
EXPOSE 26656 26657 26658 1317 9090
USER nonroot

#ENTRYPOINT ["atomoned", "start"]
