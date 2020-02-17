package main
import(
	"fmt"
	"time"
	"sync"
	"log"
	"math"
	"rand"

	"github.com/golang/protobuf/proto"

	"google.golang.org/grpc"

	pb "google.golang.org/grpc/golangwork/service_raft"
)

const (
    port = ":50051"
    MAXLOGLENGTH int = 1000
)

var timer Time.Timer
var timerLock sync.Mutex
var serverLock sync.RWMutex
var logLock sync.RWMutex
var selfAddr string = "127.0.0.1:50051"
var peers []string = {"127.0.0.1:50051","127.0.0.1:50052","127.0.0.1:50053","127.0.0.1:50054","127.0.0.1:50055"}

type server struct{
	var state uint64
	var nodeId uint64
	var leaderId uint64
	var votes uint64
	var currentTerm uint64
	var votedFor uint64
	var lastApplied uint64
	var CommitIndex uint64
	var simple_kvstore map[string]string//need to be initialize
	var simple_raftlog_key [MAXLOGLENGTH] string
	var simple_raftlog_Value [MAXLOGLENGTH] string
	var simple_raftlog_Term [MAXLOGLENGTH] uint64
	var simple_raftlog_length uint64
	var nextIndex []uint64
	var matchIndex []uint64
}

//access to much critcal area
func (s *server)TransMsg(ctx context.Context, msgSend *pb.MessageSend)(*pb.MessageRet, error){

	var msgRet pb.MessageRet

	msgRet.Type = msgSend.Type

	if msgSend.Term > s.currentTerm {
		//serverLock.Lock()
		s.currentTerm = msgSend.Term
		//serverLock.Unlock()
		BecomeFollower()
	}
	//serverLock.RLock()
	msgRet.Term = s.currentTerm
	//serverLock.RULock()

	switch msgSend.Type {
		case 1://voteRPC
			if msgSend.Term < s.currentTerm {
				msgRet.Success = 0
			} else {
				if s.votedFor == 0 || s.votedFor == msgSend.NodeId{
					if s.isUpToDate(msgSend.LogTerm,msgSend.LogIndex){
						s.votedFor = msgSend.NodeId
						msgRet.Success = 1
					}else{
						msgRet.Success = 0
					}
				}else {
					msgRet.Success = 0
				}
			}
		default://heartbeat or appendEntry RPC

			if msgSend.Term < s.currentTerm {
				msgRet.Success = 0
			} else {
				ResetClock()
				s.leaderId = msgSend.NodeId
				if s.simple_raftlog_length < msgSend.LogIndex {//didn't exist a Entry in the previous one
					msgRet.Success = 0
				}else if s.simple_raftlog_length >= msgSend.LogIndex && msgSend.LogTerm != s.simple_raftlog_Term[msgSend.LogIndex]{
					//there is a conflict,delete all entries in Index and after it
					//logLock.Lock()
					s.simple_raftlog_length = msgSend.LogIndex - 1
					//logLock.Unlock()
					msgRet.Success = 0
				}else {/
					msgRet.Success = 1
					//may be unworkable
					if s.simple_raftlog_length > msgSend.LogIndex && msgSend.Entry.Term != s.simple_raftlog_Term[msgSend.Entry.Index]{
						//unmatch the new one,conflict delete all
						//logLock.Lock()
						s.simple_raftlog_length = msgSend.LogIndex+1
						//logLock.Unlock()
					}
					if msgSend.Type != 2{
						s.AddEntry(msgSend.Entry.key,msgSend.Entry.Value,msgSend.Entry.Term)
					}
				}
				if msgSend.CommitIndex > s.CommitIndex{
				//serverLock.Lock()
				s.CommitIndex = math.min(msgSend.CommitIndex,s.CommitIndex)
				//serverLock.Unlock()
				}
			}
	}
	return &msgRet,nil
}
func (s *server)IsUpToDate(Term,Index uint64)bool{
	return !simple_raftlog_length ||Term > s.simple_raftlog_Term[s.simple_raftlog_length] || (Term == s.simple_raftlog_Term[s.simple_raftlog_length] && Index >= s.simple_raftlog_length)
}
func (s *server)AddEntry(key,Value string,Term uint64){
	//logLock.Lock()
	s.simple_raftlog_Term = Term
	s.simple_raftlog_Value = Value
	s.simple_raftlog_key = key
	//logLock.Unlock()
}
func (s *server)ProduceEntry(index uint64)pb.Entry{
	var entry pb.Entry
	entry.Type = 0//todo: produce configtype
	entry.Term = s.simple_raftlog_Term[index]
	entry.Index = index
	entry.key = s.simple_raftlog_key[index]
	entry.value = s.simple_raftlog_Value[index]
	return entry
}
func (s *server)ProduceMsg(type uint64)pb.MessageSend{
	var msgSend pb.MessageSend
	switch type{
		case 0://entry
			msgSend.Type = 0
			msgSend.Term = s.currentTerm
			msgSend.NodeId = s.nodeId
			msgSend.CommitIndex = s.CommitIndex
			msgSend.LogIndex = s.nextIndex[nodeId] - 1
			msgSend.LogTerm = s.simple_raftlog_Term[s.nextIndex[nodeId] -1]
			msgSend.Entry = s.ProduceEntry(s.nextIndex[nodeId])
		case 1://vote
			msgSend.Type = 1
			msgSend.NodeId = s.nodeId
			msgSend.LogIndex = s.simple_raftlog_length
			msgSend.LogTerm = s.simple_raftlog_Term[s.simple_raftlog_length]
		case 2://heartbeat
			msgSend.Type = 2
			msgSend.Term = s.currentTerm
			msgSend.NodeId = s.nodeId
			msgSend.LogIndex = s.nextIndex[nodeId] - 1
			msgSend.LogTerm = s.simple_raftlog_Term[s.nextIndex[nodeId] -1]
			msgSend.CommitIndex = s.CommitIndex
	}
	return msgSend
}
func (s *server)handleReturn(Ret *pb.msgRet,nodeId uint64)bool{
	switch Ret{
		case 0:
			if Ret.Success == 0{
				s.nextIndex[nodeId] = s.nextIndex[nodeId]-1
				return false
			}else{
				s.matchIndex[nodeId] = s.nextIndex[nodeId]
				s.nextIndex[nodeId] = s.nextIndex[nodeId]+1
				return true
			}
		case 1:
		case 2://heartbeat
	}
	return false
}
func ResetClock()
{
	timerLock.Lock()
	timer.Reset((rand.Int(400)+100)*Millisecond)
	timerLock.Unlock()
}
func StopClock()
{
	timerLock.Lock()
	timer.Stop()
	timerLock.Unlock()
}
func (s *server)SendEntryRpc(serverAddress string,nodeId uint64)bool{
	//first send a heartbeat, and set a timer, every 10ms send a new Rpc to the peers until it is not leader
	s.SendHeartBeat(serverAddress,nodeId)
	RpcTimer := time.NewTimer(10 * time.Millisecond)
	select{
		case <-(s.state!=3):
			return //stop sending
		case RpcTimer.C:
			fmt.Println("sending message")
			conn, _ := grpc.Dial(serverAddress,grpc.WithInsecure())
			client := pb.NewMsgRpcClient(conn)
			msgSend := s.ProduceMsg(0)
			
			ctx, _ := context.WithTimeout(context.TODO(),time.Millisecond*100)
			msgRet, _ := client.TransMsg(ctx,&msgSend)
	}

}
func (s *server)BroadcastEntryRpc(){
	for i,serverAddress := range peers{
		if i == s.nodeId{
			continue
		}
		go s.SendEntryRpc(serverAddress,i)
	}

}
func (s *server)SendHeartBeat(serverAddress string,nodeId uint64){
	fmt.Println("sending message")
	conn, _ := grpc.Dial(serverAddress,grpc.WithInsecure())
	client := pb.NewMsgRpcClient(conn)
	msgSend := s.ProduceMsg(2)
	ctx, _ := context.WithTimeout(context.TODO(),time.Millisecond*100)
	msgRet, _ := client.TransMsg(ctx,&msgSend)
	conn.Close()

}
func (s *server)BroadcastHeartBeat(){
	for i,serverAddress := range peers{
		if i == s.nodeId{
			continue
		}
		go s.SendHeartBeat(serverAddress)
	}
}
func (s *server)SendVoteRpc(){

}
func (s *server)BroadcastVoteRpc(){

}
func (s *server)BecomeFollower(){
	s.state = 1
	s.votedFor = 0
}
func (s *server)BecomeLeader(){
	s.state = 3
	s.leaderId = s.nodeId
	//nextIndex[1] = s.simple_raftlog_length+1
	nextIndex[2] = s.simple_raftlog_length+1
	nextIndex[3] = s.simple_raftlog_length+1
	nextIndex[4] = s.simple_raftlog_length+1
	nextIndex[5] = s.simple_raftlog_length+1
	//matchIndex[1] = 0
	matchIndex[2] = 0
	matchIndex[3] = 0
	matchIndex[4] = 0
	matchIndex[5] = 0
	go grpcserver.BroadcastEntryRpc()
}
func (s *server)BecomeCandidate(){
	s.state = 2
	//serverLock.Lock()
	s.currentTerm = s.currentTerm + 1
	s.votedFor = s.nodeId
	//serverLock.Unlock()
	//reset timer
	ResetClock()
	s.votes = 1;//vote for himself
	s.BroadcastVoteRpc()
}
func (s *server)ResetServer(){
	s.nodeId = 1
	s.leaderId = 0 //doesn't have a leader
	s.currentTerm = 1
	s.votedFor = 0 //doesn't vote for any one
	s.lastApplied = 0//doesn't apple anything
	s.CommitIndex = 0
	s.simple_kvstore = make(map[string]string)
	s.votes = 0
	s.state = 1//follower
	timer.NewTimer((rand.Int(400)+100)*time.Millisecond)
	go s.timerFunc()
}
func (s *server)ForwardCommand(){

}
func (s *server)CommitLog(){

}
func (s *server)TimerFunc(){
	for{
		select{
			case <- timer.C:
				s.BecomeCandidate()
		}
	}
}

func main(){
	lis, err := net.Listen("tcp",port)
	if err != nil {
        log.Fatalf("failed to listen: %v", err)
	}
	grpcserver := grpc.NewServer()
	pb.RegisterMsgRpcServer(grpcserver,&server)
	grpcserver.ResetServer()
	go TimerFunc()
	go grpcserver.Serve(lis)//begin to serve
	grpcserver.BecomeFollower()//at first, the server is a follower
	for{
		switch grpcserver.state{
			case 1://follower state
			case 2://candidate state
				if grpcserver.votes == 3{
					grpcserver.BecomeLeader()
				}
			case 3://leader state
		}
	}
}