FROM golang:1.26.10-alpine@sha256:87c65a688975c0b1d4585d204874c45ca78354670e3a5f11368c5a492303d696 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/gateforge ./cmd/gateforge && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/demo-backend ./cmd/demo-backend

FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab AS gateforge
COPY --from=build /out/gateforge /gateforge
COPY configs/gateforge.json /configs/gateforge.json
EXPOSE 8080 9090
ENTRYPOINT ["/gateforge","run","-config","/configs/gateforge.json"]

FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab AS demo-backend
COPY --from=build /out/demo-backend /demo-backend
EXPOSE 8080
ENTRYPOINT ["/demo-backend"]

FROM build AS test-build
RUN CGO_ENABLED=0 go build -trimpath -o /out/test-backend ./cmd/test-backend

FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab AS test-backend
COPY --from=test-build /out/test-backend /test-backend
ENTRYPOINT ["/test-backend"]
