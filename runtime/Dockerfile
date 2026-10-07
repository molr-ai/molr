FROM golang:1.27.1 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /molr-server ./cmd/molr-server

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /molr-server /molr-server
VOLUME ["/data"]
EXPOSE 8080
ENTRYPOINT ["/molr-server"]
CMD ["serve", "-config", "/etc/molr/server.json"]
