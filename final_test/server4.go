package main
import(
	"fmt"
	"time"
	"sort"
	//"sync"
	"log"
	"net"
	"math/rand"
	"golang.org/x/net/context"

	"google.golang.org/grpc"

	pb "google.golang.org/grpc/golangwork/service_raft"
)

const (
    port = ":50054"
    MAXLOGLENGTH int = 1000
)

var timer *time.Timer
//var timerLock sync.Mutex
//var serverLock sync.RWMutex
//var logLock sync.RWMutex
var selfAddr string = "127.0.0.1:50054"
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
	nextIndex [MAXLOGLENGTH]uint64 
	matchIndex [MAXLOGLENGTH]uint64 
}

//access to much critcal area
func (s *server)TransMsg(ctx context.Context, msgSend *pb.MessageSend)(*pb.MessageRet, error){
	log.Println("node:",s.NodeID,":Reciving from node:",msgSend.NodeID)
	fmt.Println(msgSend.Type,msgSend.Term,msgSend.LogIndex,msgSend.LogTerm,msgSend.CommitIndex,msgSend.NodeID)
	fmt.Println(s.simple_raftlog_length,s.CommitIndex,s.currentTerm,s.votes,s.lastApplied,s.votedFor)
	var msgRet pb.MessageRet
	/*
	ResetClock()
	msgRet.Success = 1
	msgRet.Term = msgSend.Term
	*/
	switch msgSend.Type{
		case 0:msgRet.Type = 0
		case 1:msgRet.Type = 1
		case 2:msgRet.Type = 2
	}
	if msgSend.Term > s.currentTerm {
		//serverLock.Lock()
		s.currentTerm = msgSend.Term
		s.votedFor = 0
		//serverLock.Unlock()
		ResetClock()
		s.BecomeFollower()
	}
	//serverLock.RLock()
	msgRet.Term = s.currentTerm
	//serverLock.RULock()

	switch msgSend.Type {
		case 1://voteRPC
			if msgSend.Term < s.currentTerm {
				log.Println("node:",s.NodeID,"lower term, reject")
				msgRet.Success = 0
			} else {
				if s.votedFor == 0 || s.votedFor == msgSend.NodeID{
					if s.IsUpToDate(msgSend.LogTerm,msgSend.LogIndex){
						s.votedFor = msgSend.NodeID
						msgRet.Success = 1
					}else{
						log.Println("node:",s.NodeID,"out-of-date, reject")
						msgRet.Success = 0
					}
				}else {
					log.Println("node:",s.NodeID,"have votedFor other, reject")
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
				}
			}
	}
	log.Println("Transback message")
	return &msgRet,nil
}
func (s *server)IsUpToDate(Term,Index uint64)bool{
	return s.simple_raftlog_length==0 ||Term > s.simple_raftlog_Term[s.simple_raftlog_length] || (Term == s.simple_raftlog_Term[s.simple_raftlog_length] && Index >= s.simple_raftlog_length)
}
func (s *server)AddEntry(key,Value string,Term uint64){
	//logLock.Lock()
	s.simple_raftlog_length = s.simple_raftlog_length + 1
	s.simple_raftlog_Term[s.simple_raftlog_length] = Term
	s.simple_raftlog_Value[s.simple_raftlog_length] = Value
	s.simple_raftlog_key[s.simple_raftlog_length] = key
	//logLock.Unlock()
}
func (s *server)ProduceEntry(index uint64)*pb.Entry{
	var entry pb.Entry
	entry.Type = 0//todo: produce configtype
	entry.Term = s.simple_raftlog_Term[index]
	entry.Index = index
	entry.Key = s.simple_raftlog_key[index]
	entry.Value = s.simple_raftlog_Value[index]
	return &entry
}
func (s *server)ProduceMsg(Type uint64,NodeID uint64)(pb.MessageSend){
	var msgSend pb.MessageSend
	switch Type{
		case 0://entry
			msgSend.Type = 0
			msgSend.Term = s.currentTerm
			msgSend.NodeID = s.NodeID
			msgSend.CommitIndex = s.CommitIndex
			msgSend.LogIndex = s.nextIndex[NodeID] - 1
			msgSend.LogTerm = s.simple_raftlog_Term[s.nextIndex[NodeID] -1]
			msgSend.Entry = s.ProduceEntry(s.nextIndex[NodeID])
		case 1://vote
			msgSend.Type = 1
			msgSend.Term = s.currentTerm
			msgSend.NodeID = s.NodeID
			msgSend.LogIndex = s.simple_raftlog_length
			msgSend.LogTerm = s.simple_raftlog_Term[s.simple_raftlog_length]
		case 2://heartbeat
			msgSend.Type = 2
			msgSend.Term = s.currentTerm
			msgSend.NodeID = s.NodeID
			msgSend.LogIndex = s.nextIndex[NodeID] - 1
			msgSend.LogTerm = s.simple_raftlog_Term[s.nextIndex[NodeID] -1]
			msgSend.CommitIndex = s.CommitIndex
	}
	return msgSend
}
func (s *server)HandleReturn(Ret *pb.MessageRet,NodeID uint64){
	log.Println("node:",s.NodeID,"handling return")
	if Ret == nil{
		log.Println("bug find!!!!")
	}
	log.Println("node",NodeID,"return:",Ret.Success)
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
				log.Println("Recivie vote from",NodeID)
				s.votes = s.votes + 1
			}
		case 2://heartbeat
			if Ret.Success == 0{
				s.nextIndex[NodeID] = s.nextIndex[NodeID]-1
			}
	}
}
func ResetClock(){
	log.Println("Clock reset")
	//timerLock.Lock()
	timer.Reset(time.Duration(rand.Intn(10)+10)*time.Second)
	//timerLock.Unlock()
}
func StopClock(){
	log.Println("Clock stoped")
	//timerLock.Lock()
	timer.Stop()
	//timerLock.Unlock()
}
func (s *server)SendEntryRpc(serverAddress string,NodeID uint64){
	log.Println("node:",s.NodeID,":sending EntryRpc to",NodeID)
	conn, err := grpc.Dial(serverAddress,grpc.WithInsecure())
	if err != nil {
		log.Fatalf("fail to dial: %v", err)
	}
	client := pb.NewMsgRpcClient(conn)
	msgSend := s.ProduceMsg(0,NodeID)		
	ctx, _ := context.WithTimeout(context.TODO(),time.Millisecond*1000)
	msgRet, err := client.TransMsg(ctx,&msgSend)
	if err == nil {
		s.HandleReturn(msgRet,NodeID)
	}
	conn.Close()
}
func (s *server)BroadcastRpc(){
	log.Println("node:",s.NodeID,":Broadcasting Rpc")
	for i,serverAddress := range peers{
		if uint64(i+1) == s.NodeID{
			continue
		}
		go s.SendEntryorHeartBeat(serverAddress,uint64(i+1))
	}
}
func (s *server)SendEntryorHeartBeat(serverAddress string,NodeID uint64){
	log.Println("node:",s.NodeID,":sending heartbeat/entryRpc to ",NodeID)
	//accroding to nextindex[] to decide whether we send app or heartbeat
	//accroding to state to decide whether to send or stop
	if s.nextIndex[NodeID] > s.simple_raftlog_length{//send heartbeat
		s.SendHeartBeat(serverAddress,NodeID)
	}else{
		s.SendEntryRpc(serverAddress,NodeID)
	}
}
func (s *server)BroadcastEntryRpc(){
	log.Println("node:",s.NodeID,":Broadcasting EntryRpc")
	for i,serverAddress := range peers{
		if uint64(i+1) == s.NodeID{
			continue
		}
		go s.SendEntryRpc(serverAddress,uint64(i+1))
	}
}
func (s *server)SendHeartBeat(serverAddress string,NodeID uint64){
	log.Println("node:",s.NodeID,":sending heartbeat to ",NodeID)
	conn, err := grpc.Dial(serverAddress,grpc.WithInsecure())
	if err != nil {
		log.Fatalf("fail to dial: %v", err)
	}
	client := pb.NewMsgRpcClient(conn)
	msgSend := s.ProduceMsg(2,NodeID)
	ctx, _ := context.WithTimeout(context.TODO(),time.Millisecond*1000)
	msgRet, err := client.TransMsg(ctx,&msgSend)
	if err == nil {
		s.HandleReturn(msgRet,NodeID)
	}
	conn.Close()
}
func (s *server)BroadcastHeartBeat(){
	log.Println("node:",s.NodeID,":Broadcasting heartbeat")
	for i,serverAddress := range peers{
		if uint64(i+1) == s.NodeID{
			continue
		}
		go s.SendHeartBeat(serverAddress,uint64(i+1))
	}
}
func (s *server)SendVoteRpc(serverAddress string,NodeID uint64){
	log.Println("node:",s.NodeID,":Sending VoteRpc to",NodeID)
	conn, err := grpc.Dial(serverAddress,grpc.WithInsecure())
	if err != nil {
		log.Fatalf("fail to dial: %v", err)
	}
	client := pb.NewMsgRpcClient(conn)
	msgSend := s.ProduceMsg(1,NodeID)
	ctx, _ := context.WithTimeout(context.TODO(),time.Millisecond*1000)
	msgRet, err := client.TransMsg(ctx,&msgSend)
	if err == nil {
		s.HandleReturn(msgRet,NodeID)
	}
	conn.Close()
}
func (s *server)BroadcastVoteRpc(){
	log.Println("node:",s.NodeID,":Broadcasting VoteRpc")
	for i,serverAddress := range peers{
		if uint64(i+1) == s.NodeID{
			continue
		}
		go s.SendVoteRpc(serverAddress,uint64(i+1))
	}
}
func (s *server)BecomeFollower(){
	log.Println("node:",s.NodeID,":BecomeFollower")
	s.state = 1
	s.votedFor = 0
}
func (s *server)BecomeLeader(){
	log.Println("node:",s.NodeID,":BecomeLeader")
	s.state = 3
	s.leaderId = s.NodeID
	StopClock()
	s.nextIndex[1] = s.simple_raftlog_length+1
	s.nextIndex[2] = s.simple_raftlog_length+1
	s.nextIndex[3] = s.simple_raftlog_length+1
	//s.nextIndex[4] = s.simple_raftlog_length+1
	s.nextIndex[5] = s.simple_raftlog_length+1
	s.matchIndex[1] = 0
	s.matchIndex[2] = 0
	s.matchIndex[3] = 0
	//s.matchIndex[4] = 0
	s.matchIndex[5] = 0
}
func (s *server)BecomeCandidate(){
	log.Println("node:",s.NodeID,":BecomeCandidate")
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
	s.NodeID = 4
	s.leaderId = 0 //doesn't have a leader
	s.currentTerm = 1
	s.votedFor = 0 //doesn't vote for any one
	s.lastApplied = 0//doesn't apple anything
	s.CommitIndex = 0
	s.simple_kvstore = make(map[string]string)
	s.simple_raftlog_length = 0
	s.votes = 0
	s.state = 1//follower
	timer = time.NewTimer(time.Duration(rand.Intn(10)+10)*time.Second)
	log.Println("node:",s.NodeID,":Resetting Server")
	go s.TimerFunc()
}
func (s *server)ForwardCommand(Req *pb.Request){
	log.Println("node:%d:Forwarding message",s.NodeID)
	conn, _ := grpc.Dial(peers[s.leaderId-1],grpc.WithInsecure())
	client := pb.NewMsgRpcClient(conn)
	client.RecvCmd(context.Background(),Req)
	conn.Close()
}
func (s *server)CommitLog(){
	time.Sleep(time.Second)
	for s.CommitIndex>s.lastApplied{
		s.lastApplied = s.lastApplied+1
		s.simple_kvstore[s.simple_raftlog_key[s.lastApplied]] = s.simple_kvstore[s.simple_raftlog_Value[s.lastApplied]]
		log.Println("node:",s.NodeID,":Commiting log",s.lastApplied)
	}
}
func (s *server)TimerFunc(){
	for{
		select{
			case <- timer.C:
				log.Println("node:",s.NodeID,":Time out, begin election")
				s.BecomeCandidate()
		}
	}
}
func (s *server)RecvCmd(ctx context.Context,cmd *pb.Request)(*pb.SuccessMsg,error){
	log.Println("node:",s.NodeID,":Reciving cmd from client")
	var RetMsg pb.SuccessMsg
	if s.leaderId != s.NodeID{//leader is not me
		s.ForwardCommand(cmd)
	}else{
		s.AddEntry(cmd.Name,cmd.Value,s.currentTerm)
	}
	RetMsg.Success = true
	return &RetMsg,nil
}
func AnounanceTime(){
	for {
		fmt.Println(time.Now())
		time.Sleep(5*time.Second)
	}
}
func (s *server)CheckMatch()uint64{
	i := 1
	var result int
	var check []int
	for i<=5{
		if uint64(i)==s.NodeID{
			i++
			continue
		}
		check = append(check,int(s.matchIndex[i]))
		i++
	}
	sort.Ints(check)
	if check[3]==check[2]{
		result = check[3]
	}else if check[2]==check[1]{
		result = check[2]
	}else if check[1]==check[0]{
		result = check[1]
	}else{
		result = check[0]
	}
	return uint64(result)
}


func (s *server)MaintainState(){
	RpcTimer := time.NewTimer(5*time.Second)
	for{
		switch s.state{
			case 1://follower state,do nothing
				time.Sleep(50*time.Millisecond)
			case 2://candidate state
				time.Sleep(50*time.Millisecond)
				if s.votes >= 3{
					s.BecomeLeader()
				}
			case 3://leader state
				go s.BroadcastRpc()
				RpcTimer.Reset(3*time.Second)
				select{
					case <- RpcTimer.C:
						s.CommitIndex =  s.CheckMatch()
				}
		}
	}
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
	raftserver.BecomeFollower()//at first, the server is a follower
	go raftserver.MaintainState()
	go raftserver.CommitLog()
	//go raftserver.TimerFunc() extra func
	grpcserver.Serve(lis)//begin to serve
	//go AnounanceTime()
}