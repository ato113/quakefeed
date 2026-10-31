# Build the binary.
FROM golang:1.23-alpine AS build

WORKDIR /src
# No third-party dependencies, so there is nothing to download here.
COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal

# CGO is off so the binary runs on scratch; the flags drop the symbol table.
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /quakefeed ./cmd/quakefeed

# Ship only the binary.
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /quakefeed /quakefeed

EXPOSE 8080
USER nonroot:nonroot

# Timestamps are converted with a fixed +03 offset rather than tzdata, so this
# image needs no zoneinfo.
ENTRYPOINT ["/quakefeed"]
CMD ["-addr", ":8080"]
