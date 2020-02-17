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

func AnounanceTime(){
	for {
		fmt.Println(time.Now())
		time.Sleep(5*time.Second)
	}
}

func RepeatChangeMsg(){
	var ServerAddress string = "127.0.0.1:50052"
	for{
		timer := time.NewTimer(10*time.Second)
		select{
		case <- timer.C:
			fmt.Println("sending message")
			conn, _ := grpc.Dial(ServerAddress,grpc.WithInsecure())
			client := pb.NewTestRpcClient(conn)
			var SendMsg pb.TestMsg
			SendMsg.Testpoint = 2
			SendMsg.Testnumber = 0
			RetMsg, _ := client.Echofeature(context.Background(),&SendMsg)
			fmt.Println(RetMsg.Testpoint)
			fmt.Println(RetMsg.Testnumber)
			conn.Close()
		}
	}
}

func main(){
	lis, err := net.Listen("tcp",port)
	if err != nil {
        log.Fatalf("failed to listen: %v", err)
	}
	grpcserver := grpc.NewServer()
	pb.RegisterTestRpcServer(grpcserver,&server{})
	go grpcserver.Serve(lis)
	go AnounanceTime()
	RepeatChangeMsg()
}


