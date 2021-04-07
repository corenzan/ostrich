FROM golang:1.16-alpine

RUN go get github.com/codegangsta/gin

WORKDIR /go/src/app

CMD ["gin", "run", "."]