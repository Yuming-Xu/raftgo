package main

import(
	"fmt"
	"time"
	"log"
	"net"

	"google.golang.org/grpc"
	"golang.org/x/net/context"

	pb "google.golang.org/grpc/golangwork/test1"
)

const (
    port = ":50051"
)

type server struct {}

func (s *server) Echofeature(ctx context.Context, sendMsg *pb.TestMsg)(*pb.TestMsg, error){
	fmt.Println("message recevied")
	var RetMsg pb.TestMsg
	switch sendMsg.Testpoint {
		case 1:
			RetMsg.Testpoint = 11
			RetMsg.Testnumber = sendMsg.Testnumber+10
		case 2:
			RetMsg.Testpoint = 12
			RetMsg.Testnumber = sendMsg.Testnumber+20
	}
	return &RetMsg,nil
}

func main(){
	lis, err := net.Listen("tcp",port)
	if err != nil {
        log.Fatalf("failed to listen: %v", err)
	}
	grpcserver := grpc.NewServer()
	pb.RegisterTestRpcServer(grpcserver,&server{})
	go grpcserver.Serve(lis)
	for {
		fmt.Println(time.Now())
		time.Sleep(5*time.Second)
	}
}


