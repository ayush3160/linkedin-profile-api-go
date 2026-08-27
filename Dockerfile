# --- build ---------------------------------------------------------------
FROM golang:1.23-alpine AS build

WORKDIR /src

# No third-party dependencies, so go.mod alone primes the module cache.
COPY go.mod ./
RUN go mod download

COPY . .
# Static binary: nothing to link against in the runtime image.
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

# --- run -----------------------------------------------------------------
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/server /server

ENV PORT=8000
EXPOSE 8000
USER nonroot:nonroot

ENTRYPOINT ["/server"]
