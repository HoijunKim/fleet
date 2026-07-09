# Multi-stage build of the fleet backend (cmd/fleetd) as a static binary.
FROM golang:1.22-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -o /out/fleetd ./cmd/fleetd

FROM gcr.io/distroless/static-debian12
COPY --from=build /out/fleetd /fleetd
EXPOSE 8080
ENV PORT=8080
ENTRYPOINT ["/fleetd"]
