package main
import(
	//"fmt"
	"math/rand"
	"time"
	"golang.org/x/net/context"

	"google.golang.org/grpc"

	pb "google.golang.org/grpc/golangwork/service_raft"
)

var error_num int
var Port []string
func testClient(id int){
	var num int = 0
	var	peers []string
	var clients []pb.MsgRpcClient
	peers = []string{	"127.0.0.1:50051",
						"127.0.0.1:50052",
						"127.0.0.1:50053",
						"127.0.0.1:50054",
						"127.0.0.1:50055"}
	for _,serverAddress := range peers{
		conn, _ := grpc.Dial(serverAddress,grpc.WithInsecure())
		client := pb.NewMsgRpcClient(conn)
		clients = append(clients,client)
	}
	for{
		var Req pb.Request
		Req.Name = "168" + string(id) + string(num)
		Req.Value = "bot"
		ctx, _ := context.WithTimeout(context.TODO(),3*time.Second)
		_ ,err := clients[rand.Intn(5)].RecvCmd(ctx,&Req)
		if err!=nil{
			error_num++
		}
		num++
	}
}

func main(){
	error_num = 0
	i:=0
	for i<100{
		go testClient(i)
		i++
	}
	for{
		time.Sleep(1*time.Second)
	}
}
