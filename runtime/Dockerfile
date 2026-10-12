FROM golang:1.27.1 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /molr ./cmd/molr

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /molr /molr
VOLUME ["/data"]
EXPOSE 8080
ENTRYPOINT ["/molr"]
CMD ["serve", "-config", "/etc/molr/server.json"]
