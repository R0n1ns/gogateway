FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /gateforge ./cmd/gateforge
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /gateforge /gateforge
COPY configs/gateforge.json /configs/gateforge.json
EXPOSE 8080 9090
ENTRYPOINT ["/gateforge","-config","/configs/gateforge.json"]