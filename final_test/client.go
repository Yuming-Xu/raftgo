package main
import(
	"fmt"
	//"sync"
	"golang.org/x/net/context"

	"google.golang.org/grpc"

	pb "google.golang.org/grpc/golangwork/service_raft"
)


func main(){
	var option int
	var Req pb.Request
	var Address string = "127.0.0.1:"
	var Port string
	for{
		fmt.Println("enter 0 to quit, 1 to request")
		fmt.Scanf("%v",&option)
		if option == 0{
			break
		}
		fmt.Println("please enter your request key")
		fmt.Scanf("%s",&Req.Name)
		fmt.Println("please enter your request Value")
		fmt.Scanf("%s",&Req.Value)
		fmt.Println("please enter the port")
		fmt.Scanf("%s",&Port)
		Address = Address + Port
		conn, _ := grpc.Dial(Address,grpc.WithInsecure())
		client := pb.NewMsgRpcClient(conn)
		client.RecvCmd(context.Background(),&Req)
		conn.Close()
	}
}