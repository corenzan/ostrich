FROM golang:1.16-alpine

RUN go get github.com/codegangsta/gin

WORKDIR /app

CMD ["gin", "run", "."]