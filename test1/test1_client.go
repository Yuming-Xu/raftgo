package main

import(
	"fmt"
	"log"
	"google.golang.org/grpc"
	"golang.org/x/net/context"

	pb "google.golang.org/grpc/golangwork/test1"
)


func main(){
	var ServerAddress string = "127.0.0.1:50051"
	conn, err := grpc.Dial(ServerAddress,grpc.WithInsecure())
	if err != nil {
		log.Fatalf("fail to dial: %v", err)
	}
	defer conn.Close()
	client := pb.NewTestRpcClient(conn)
	var SendMsg pb.TestMsg
	SendMsg.Testpoint = 2
	SendMsg.Testnumber = 0
	RetMsg, err := client.Echofeature(context.Background(),&SendMsg)
	fmt.Println(RetMsg.Testpoint)
	fmt.Println(RetMsg.Testnumber)
}
