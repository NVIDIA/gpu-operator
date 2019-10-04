FROM golang:1.13

WORKDIR /go/src/github.com/NVIDIA/gpu-operator

COPY . .

RUN go get -u golang.org/x/lint/golint
RUN go get -u github.com/gordonklaus/ineffassign

CMD ["bash"]
