FROM golang:1.25-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .

ARG VERSION=dev
ARG COMMIT=unknown
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT}" -o /octojoin . \
    && mkdir /data

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /octojoin /usr/local/bin/octojoin
# State and cache are written to $HOME/.config/octojoin
COPY --from=build --chown=nonroot:nonroot /data /data
ENV HOME=/data \
    OCTOJOIN_DAEMON=true \
    OCTOJOIN_WEB=true
VOLUME /data
EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/octojoin"]
