FROM golang:1.23.2 AS build

WORKDIR /go/src/app

COPY go.mod go.sum main.go .
COPY internal internal

RUN go mod download
RUN CGO_ENABLED=0 go build -ldflags '-s -w' -o /go/bin/surreallog

FROM gcr.io/distroless/static

COPY --from=build /go/bin/surreallog /

ENTRYPOINT ["/surreallog"]
