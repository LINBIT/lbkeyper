FROM golang:alpine as builder

WORKDIR /usr/src/app

# pre-copy/cache go.mod for pre-downloading dependencies and only redownloading them in subsequent builds if they change
COPY go.mod go.sum ./
RUN go mod download && go mod verify

COPY . .
RUN apk add --no-cache make git
RUN make

FROM alpine:latest
MAINTAINER Roland Kammerer <roland.kammerer@linbit.com>
COPY --from=builder /usr/src/app/lbkeyper /usr/local/bin/lbkeyper
ENTRYPOINT [ "/usr/local/bin/lbkeyper" ]
