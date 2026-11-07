FROM golang:1.26-alpine AS builder
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

# CGO_ENABLED=0  → static binary, no libc (required below)
# -trimpath      → no local machine paths: reproducible build
# -s -w          → strips symbol tables: a few MBs smaller

RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/api ./cmd/api


FROM gcr.io/distroless/static:nonroot
COPY --from=builder /out/api /api
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/api"] 