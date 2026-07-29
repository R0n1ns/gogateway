FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/gateforge ./cmd/gateforge && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/demo-backend ./cmd/demo-backend

FROM gcr.io/distroless/static-debian12:nonroot AS gateforge
COPY --from=build /out/gateforge /gateforge
COPY configs/gateforge.json /configs/gateforge.json
EXPOSE 8080 9090
ENTRYPOINT ["/gateforge","run","-config","/configs/gateforge.json"]

FROM gcr.io/distroless/static-debian12:nonroot AS demo-backend
COPY --from=build /out/demo-backend /demo-backend
EXPOSE 8080
ENTRYPOINT ["/demo-backend"]