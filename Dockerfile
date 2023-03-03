FROM golang:latest

WORKDIR /go/src/app

COPY . .

RUN go mod tidy
RUN go mod download

EXPOSE 9092

CMD ["go", "run", "main.go"]

VOLUME ./playlist.txt:/go/src/app/playlist.txt