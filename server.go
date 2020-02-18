package main
import(
	"fmt"
	"time"
	"sync"
	"log"
	"net"
	"math/rand"
	"golang.org/x/net/context"

	"google.golang.org/grpc"

	pb "google.golang.org/grpc/golangwork/service_raft"
)

const (
    port = ":50051"
    MAXLOGLENGTH int = 1000
)

var timer *time.Timer
var timerLock sync.Mutex
//var serverLock sync.RWMutex
//var logLock sync.RWMutex
var selfAddr string = "127.0.0.1:50051"
var	peers []string

type server struct{
	pb.UnimplementedMsgRpcServer
	state uint64 
	NodeID uint64 
	leaderId uint64 
	votes uint64 
	currentTerm uint64 
	votedFor uint64 
	lastApplied uint64 
	CommitIndex uint64 
	simple_kvstore map[string]string //need to be initialize
	simple_raftlog_key [MAXLOGLENGTH] string 
	simple_raftlog_Value [MAXLOGLENGTH] string 
	simple_raftlog_Term [MAXLOGLENGTH] uint64 
	simple_raftlog_length uint64 
	nextIndex []uint64 
	matchIndex []uint64 
}

//access to much critcal area
func (s *server)TransMsg(ctx context.Context, msgSend *pb.MessageSend)(*pb.MessageRet, error){

	var msgRet pb.MessageRet

	switch msgSend.Type{
		case 0:msgRet.Type = 0
		case 1:msgRet.Type = 1
		case 2:msgRet.Type = 2
	}

	if msgSend.Term > s.currentTerm {
		//serverLock.Lock()
		s.currentTerm = msgSend.Term
		//serverLock.Unlock()
		s.BecomeFollower()
	}
	//serverLock.RLock()
	msgRet.Term = s.currentTerm
	//serverLock.RULock()

	switch msgSend.Type {
		case 1://voteRPC
			if msgSend.Term < s.currentTerm {
				msgRet.Success = 0
			} else {
				if s.votedFor == 0 || s.votedFor == msgSend.NodeID{
					if s.IsUpToDate(msgSend.LogTerm,msgSend.LogIndex){
						s.votedFor = msgSend.NodeID
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
				s.leaderId = msgSend.NodeID
				if s.simple_raftlog_length < msgSend.LogIndex {//didn't exist a Entry in the previous one
					msgRet.Success = 0
				}else if s.simple_raftlog_length >= msgSend.LogIndex && msgSend.LogTerm != s.simple_raftlog_Term[msgSend.LogIndex]{
					//there is a conflict,delete all entries in Index and after it
					//logLock.Lock()
					s.simple_raftlog_length = msgSend.LogIndex - 1
					//logLock.Unlock()
					msgRet.Success = 0
				}else {
					msgRet.Success = 1
					//may be unworkable
					if s.simple_raftlog_length > msgSend.LogIndex && msgSend.Entry.Term != s.simple_raftlog_Term[msgSend.Entry.Index]{
						//unmatch the new one,conflict delete all
						//logLock.Lock()
						s.simple_raftlog_length = msgSend.LogIndex+1
						//logLock.Unlock()
					}
					if msgSend.Type != 2{
						s.AddEntry(msgSend.Entry.Key,msgSend.Entry.Value,msgSend.Entry.Term)
					}
				}
				if msgSend.CommitIndex > s.CommitIndex{
				//serverLock.Lock()
					if msgSend.CommitIndex > s.simple_raftlog_length{
						s.CommitIndex = s.simple_raftlog_length
					}else{
						s.CommitIndex = msgSend.CommitIndex
					}
				
				//serverLock.Unlock()
				s.CommitLog()
				}
			}
	}
	return &msgRet,nil
}
func (s *server)IsUpToDate(Term,Index uint64)bool{
	return s.simple_raftlog_length==0 ||Term > s.simple_raftlog_Term[s.simple_raftlog_length] || (Term == s.simple_raftlog_Term[s.simple_raftlog_length] && Index >= s.simple_raftlog_length)
}
func (s *server)AddEntry(key,Value string,Term uint64){
	//logLock.Lock()
	s.simple_raftlog_Term[s.simple_raftlog_length] = Term
	s.simple_raftlog_Value[s.simple_raftlog_length] = Value
	s.simple_raftlog_key[s.simple_raftlog_length] = key
	s.simple_raftlog_length = s.simple_raftlog_length + 1
	//logLock.Unlock()
}
func (s *server)ProduceEntry(index uint64)pb.Entry{
	var entry pb.Entry
	entry.Type = 0//todo: produce configtype
	entry.Term = s.simple_raftlog_Term[index]
	entry.Index = index
	entry.Key = s.simple_raftlog_key[index]
	entry.Value = s.simple_raftlog_Value[index]
	return entry
}
func (s *server)ProduceMsg(Type uint64)(pb.MessageSend){
	var msgSend pb.MessageSend
	switch Type{
		case 0://entry
			msgSend.Type = 0
			msgSend.Term = s.currentTerm
			msgSend.NodeID = s.NodeID
			msgSend.CommitIndex = s.CommitIndex
			msgSend.LogIndex = s.nextIndex[s.NodeID] - 1
			msgSend.LogTerm = s.simple_raftlog_Term[s.nextIndex[s.NodeID] -1]
			*msgSend.Entry = s.ProduceEntry(s.nextIndex[s.NodeID])
		case 1://vote
			msgSend.Type = 1
			msgSend.NodeID = s.NodeID
			msgSend.LogIndex = s.simple_raftlog_length
			msgSend.LogTerm = s.simple_raftlog_Term[s.simple_raftlog_length]
		case 2://heartbeat
			msgSend.Type = 2
			msgSend.Term = s.currentTerm
			msgSend.NodeID = s.NodeID
			msgSend.LogIndex = s.nextIndex[s.NodeID] - 1
			msgSend.LogTerm = s.simple_raftlog_Term[s.nextIndex[s.NodeID] -1]
			msgSend.CommitIndex = s.CommitIndex
	}
	return msgSend
}
func (s *server)HandleReturn(Ret *pb.MessageRet,NodeID uint64){
	if Ret.Term > s.currentTerm{//this server is out-of-date
		s.BecomeFollower()
		return
	}
	switch Ret.Type{
		case 0:
			if Ret.Success == 0{
				s.nextIndex[NodeID] = s.nextIndex[NodeID]-1
			}else{
				s.matchIndex[NodeID] = s.nextIndex[NodeID]
				s.nextIndex[NodeID] = s.nextIndex[NodeID]+1
			}
		case 1:
			if Ret.Success == 1{
				s.votes = s.votes + 1
			}
		case 2://heartbeat
			if Ret.Success == 0{
				s.nextIndex[NodeID] = s.nextIndex[NodeID]-1
			}else{
				s.matchIndex[NodeID] = s.nextIndex[NodeID]
				s.nextIndex[NodeID] = s.nextIndex[NodeID]+1
			}
	}
}
func ResetClock(){
	timerLock.Lock()
	timer.Reset(time.Duration(rand.Intn(400)+100)*time.Millisecond)
	timerLock.Unlock()
}
func StopClock(){
	timerLock.Lock()
	timer.Stop()
	timerLock.Unlock()
}
func (s *server)SendEntryRpc(serverAddress string,NodeID uint64){
	fmt.Println("sending message")
	conn, _ := grpc.Dial(serverAddress,grpc.WithInsecure())
	client := pb.NewMsgRpcClient(conn)
	msgSend := s.ProduceMsg(0)		
	ctx, _ := context.WithTimeout(context.TODO(),time.Millisecond*100)
	msgRet, _ := client.TransMsg(ctx,&msgSend)
	s.HandleReturn(msgRet,0)
}
func (s *server)BroadcastRpc(){
	for i,serverAddress := range peers{
		if uint64(i) == s.NodeID{
			continue
		}
		go s.SendEntryorHeartBeat(serverAddress,uint64(i))
	}
}
func (s *server)SendEntryorHeartBeat(serverAddress string,NodeID uint64){
	//accroding to nextindex[] to decide whether we send app or heartbeat
	//accroding to state to decide whether to send or stop
	if s.nextIndex[NodeID] > s.simple_raftlog_length{//send heartbeat
		s.SendHeartBeat(serverAddress,NodeID)
	}else{
		s.SendEntryRpc(serverAddress,NodeID)
	}
}
func (s *server)BroadcastEntryRpc(){
	for i,serverAddress := range peers{
		if uint64(i) == s.NodeID{
			continue
		}
		go s.SendEntryRpc(serverAddress,uint64(i))
	}
}
func (s *server)SendHeartBeat(serverAddress string,NodeID uint64){
	fmt.Println("sending message")
	conn, _ := grpc.Dial(serverAddress,grpc.WithInsecure())
	client := pb.NewMsgRpcClient(conn)
	msgSend := s.ProduceMsg(2)
	ctx, _ := context.WithTimeout(context.TODO(),time.Millisecond*100)
	msgRet, _ := client.TransMsg(ctx,&msgSend)
	conn.Close()
	s.HandleReturn(msgRet,2)
}
func (s *server)BroadcastHeartBeat(){
	for i,serverAddress := range peers{
		if uint64(i) == s.NodeID{
			continue
		}
		go s.SendHeartBeat(serverAddress,uint64(i))
	}
}
func (s *server)SendVoteRpc(serverAddress string,NodeID uint64){
	fmt.Println("sending message")
	conn, _ := grpc.Dial(serverAddress,grpc.WithInsecure())
	client := pb.NewMsgRpcClient(conn)
	msgSend := s.ProduceMsg(1)
	ctx, _ := context.WithTimeout(context.TODO(),time.Millisecond*100)
	msgRet, _ := client.TransMsg(ctx,&msgSend)
	conn.Close()
	s.HandleReturn(msgRet,1)
}
func (s *server)BroadcastVoteRpc(){
	for i,serverAddress := range peers{
		if uint64(i) == s.NodeID{
			continue
		}
		go s.SendVoteRpc(serverAddress,uint64(i))
	}
}
func (s *server)BecomeFollower(){
	s.state = 1
	s.votedFor = 0
}
func (s *server)BecomeLeader(){
	s.state = 3
	s.leaderId = s.NodeID
	//nextIndex[1] = s.simple_raftlog_length+1
	s.nextIndex[2] = s.simple_raftlog_length+1
	s.nextIndex[3] = s.simple_raftlog_length+1
	s.nextIndex[4] = s.simple_raftlog_length+1
	s.nextIndex[5] = s.simple_raftlog_length+1
	//matchIndex[1] = 0
	s.matchIndex[2] = 0
	s.matchIndex[3] = 0
	s.matchIndex[4] = 0
	s.matchIndex[5] = 0
}
func (s *server)BecomeCandidate(){
	s.state = 2
	//serverLock.Lock()
	s.currentTerm = s.currentTerm + 1
	s.votedFor = s.NodeID
	//serverLock.Unlock()
	//reset timer
	ResetClock()
	s.votes = 1;//vote for himself
	s.BroadcastVoteRpc()
}
func (s *server)ResetServer(){
	s.NodeID = 1
	s.leaderId = 0 //doesn't have a leader
	s.currentTerm = 1
	s.votedFor = 0 //doesn't vote for any one
	s.lastApplied = 0//doesn't apple anything
	s.CommitIndex = 0
	s.simple_kvstore = make(map[string]string)
	s.votes = 0
	s.state = 1//follower
	timer = time.NewTimer(time.Duration(rand.Intn(400)+100)*time.Millisecond)
	go s.TimerFunc()
}
func (s *server)ForwardCommand(Req *pb.Request){
	fmt.Println("forwarding message")
	conn, _ := grpc.Dial(peers[s.leaderId],grpc.WithInsecure())
	client := pb.NewMsgRpcClient(conn)
	client.RecvCmd(context.Background(),Req)
	conn.Close()
}
func (s *server)CommitLog(){
	for s.CommitIndex>s.lastApplied{
		s.lastApplied = s.lastApplied+1
		s.simple_kvstore[s.simple_raftlog_key[s.lastApplied]] = s.simple_kvstore[s.simple_raftlog_Value[s.lastApplied]]
	}
}
func (s *server)TimerFunc(){
	for{
		select{
			case <- timer.C:
				s.BecomeCandidate()
		}
	}
}
func (s *server)RecvCmd(ctx context.Context,cmd *pb.Request)(*pb.SuccessMsg,error){
	var RetMsg pb.SuccessMsg
	if s.leaderId != s.NodeID{//leader is not me
		s.ForwardCommand(cmd)
	}else{
		s.AddEntry(cmd.Name,cmd.Value,s.currentTerm)
	}
	RetMsg.Success = true
	return &RetMsg,nil
}

func main(){
	peers = []string{	"127.0.0.1:50051",
						"127.0.0.1:50052",
						"127.0.0.1:50053",
						"127.0.0.1:50054",
						"127.0.0.1:50055"}
	lis, err := net.Listen("tcp",port)
	if err != nil {
        log.Fatalf("failed to listen: %v", err)
	}
	grpcserver := grpc.NewServer()
	var raftserver *server
	raftserver = new(server)
	pb.RegisterMsgRpcServer(grpcserver,raftserver)
	raftserver.ResetServer()
	go raftserver.TimerFunc()
	go grpcserver.Serve(lis)//begin to serve
	raftserver.BecomeFollower()//at first, the server is a follower
	for{
		switch raftserver.state{
			case 1://follower state,do nothing
			case 2://candidate state
				if raftserver.votes == 3{
					raftserver.BecomeLeader()
				}
			case 3://leader state
				go raftserver.BroadcastRpc()
				RpcTimer := time.NewTimer(10*time.Millisecond)
				select{
					case <- RpcTimer.C:
				}
		}
	}
}