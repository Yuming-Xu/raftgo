package main
import(
	"fmt"
	"time"
	"sort"
	"sync"
	"log"
	"net"
	"math/rand"
	"golang.org/x/net/context"

	"google.golang.org/grpc"

	pb "google.golang.org/grpc/golangwork/service_raft"
)

const (
    MAXLOGLENGTH int = 10000000
)
var timer *time.Timer
var ID uint64
	//lock
var	timerLock sync.Mutex // for time
var	serverLock sync.RWMutex// for state
var	logLock sync.RWMutex// for log
var	IndexLock sync.RWMutex// for nextIndex and MarchIndex
var	peers []string
var port []string
var count int64
var conns []*grpc.ClientConn
var clients []pb.MsgRpcClient
var TransMsgTime int64
var CheckSendIndexTime int64
var AddEntryTime int64
var RecvCmdTime int64
var HandleReturnTime int64
var CheckMatchTime int64
var RpcCostTime int64

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
	start:=time.Now().UnixNano()
	//log.Println("node:",s.NodeID,":Reciving from node:",msgSend.NodeID)
	//if msgSend.Type == 0{
		//log.Println(msgSend.Type,msgSend.Term,msgSend.LogIndex,msgSend.LogTerm,msgSend.CommitIndex,msgSend.NodeID)
	//}
	//log.Println(s.simple_raftlog_length,s.CommitIndex,s.currentTerm,s.votes,s.lastApplied,s.votedFor)
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
		//log.Println("i am out-of-date")
		serverLock.Lock()
		s.currentTerm = msgSend.Term
		serverLock.Unlock()
		ResetClock()
		s.BecomeFollower()
	}
	msgRet.Term = s.currentTerm
	switch msgSend.Type {
		case 1://voteRPC
			serverLock.RLock()
			if msgSend.Term < s.currentTerm {
				serverLock.RUnlock()
				//log.Println("node:",s.NodeID,"lower term, reject",msgSend.NodeID)
				msgRet.Success = 0
			}else {
				if s.votedFor == 0 || s.votedFor == msgSend.NodeID{
					serverLock.RUnlock()
					if s.IsUpToDate(msgSend.LogTerm,msgSend.LogIndex){
						serverLock.Lock()
						s.votedFor = msgSend.NodeID
						serverLock.Unlock()
						msgRet.Success = 1
					}else{
						//log.Println("node:",s.NodeID,"out-of-date, reject",msgSend.NodeID)
						msgRet.Success = 0
					}
				}else {
					serverLock.RUnlock()
					//log.Println("node:",s.NodeID,"have votedFor other, reject",msgSend.NodeID)
					msgRet.Success = 0
				}
			}
		default://heartbeat or appendEntry RPC
			if msgSend.Term < s.currentTerm {
				//log.Println("out-of-date,reject")
				msgRet.Success = 0
			}else {
				ResetClock()
				//if msgSend.Type!=2{
					//log.Println("oh it is here:111")
				//}
				s.CheckSendIndex(msgSend.CommitIndex)
				//log.Println(msgSend.CommitIndex,s.CommitIndex,s.simple_raftlog_length)
				s.leaderId = msgSend.NodeID
				//log.Println("lock check")
				logLock.RLock()
				//log.Println("msg check")
				if s.simple_raftlog_length < msgSend.LogIndex {
					//didn't exist a Entry in the previous one
					//log.Println("too advanced")
					logLock.RUnlock()
					msgRet.Success = 0
				}else if s.simple_raftlog_length >= msgSend.LogIndex && 
				msgSend.LogTerm != s.simple_raftlog_Term[msgSend.LogIndex]{
					//there is a conflict,delete all entries in Index and after it,
					//and we can't append this entry
					//log.Println("conflict,reject")
					logLock.RUnlock()
					logLock.Lock()
					s.simple_raftlog_length = msgSend.LogIndex - 1
					logLock.Unlock()
					msgRet.Success = 0
				}else{
					if msgSend.Type!=2{
						//log.Println("oh it is here:133")
					}
					msgRet.Success = 1//actually mgsSend.LogIndex = msgSend.Entry.Index - 1
					if msgSend.Type != 2 &&
					 s.simple_raftlog_length >= msgSend.Entry.Index &&
					  msgSend.Entry.Term != s.simple_raftlog_Term[msgSend.Entry.Index]{
						//unmatch the new one,conflict delete all，then append
						//log.Println("unmatch the new one,delete and then append")
						logLock.RUnlock()
						logLock.Lock()
						s.simple_raftlog_length = msgSend.LogIndex
						logLock.Unlock()
						s.AddEntry(msgSend.Entry.Key,msgSend.Entry.Value,msgSend.Entry.Term)
					}else if msgSend.Type != 2 && 
					s.simple_raftlog_length >= msgSend.Entry.Index && 
					msgSend.Entry.Term == s.simple_raftlog_Term[msgSend.Entry.Index]{
						//log.Println("not the new one,don't have to append")
						logLock.RUnlock()//not the new entry，and we don't need to delete
					}else if msgSend.Type != 2 && s.simple_raftlog_length == msgSend.LogIndex {
						//log.Println("yes it is a new one,append")
						logLock.RUnlock()//new one, and there is not conflict
						s.AddEntry(msgSend.Entry.Key,msgSend.Entry.Value,msgSend.Entry.Term)
					}else {//it is heartbeat
						if msgSend.Type != 2{
							//log.Println("bug:it is not heartbeat!!")
						}
						logLock.RUnlock()
					}
				}
				//if msgSend.Type!=2{
				//	log.Println("oh it is here:155")
				//}
			}
	}
	//log.Println("Transback message")
	end:=time.Now().UnixNano()
	TransMsgTime += (end - start)
	return &msgRet,nil
}

func (s *server)CheckSendIndex(index uint64){
	start:=time.Now().UnixNano()
	//log.Println("CheckSendIndex")
	serverLock.Lock()
	//log.Println("oh it is here:171")
	if index > s.CommitIndex{
		if index > s.simple_raftlog_length{
			s.CommitIndex = s.simple_raftlog_length
		}else{
			s.CommitIndex = index
		}
		serverLock.Unlock()
	}else{
		serverLock.Unlock()
	}
	end:=time.Now().UnixNano()
	CheckSendIndexTime += (end - start)
	//log.Println("CheckSendIndex return")
}

func (s *server)IsUpToDate(Term,Index uint64)bool{
	logLock.RLock()
	var uptodate bool = s.simple_raftlog_length==0 ||Term > s.simple_raftlog_Term[s.simple_raftlog_length] || (Term == s.simple_raftlog_Term[s.simple_raftlog_length] && Index >= s.simple_raftlog_length)
	logLock.RUnlock()
	return uptodate
}
func (s *server)AddEntry(key,Value string,Term uint64)uint64{
	start:=time.Now().UnixNano()
	logLock.Lock()
	//log.Println("adding entry")
	s.simple_raftlog_length = s.simple_raftlog_length + 1
	s.simple_raftlog_Term[s.simple_raftlog_length] = Term
	s.simple_raftlog_Value[s.simple_raftlog_length] = Value
	s.simple_raftlog_key[s.simple_raftlog_length] = key
	logLock.Unlock()
	end:=time.Now().UnixNano()
	AddEntryTime += (end - start)
	return s.simple_raftlog_length
}
func (s *server)ProduceEntry(index uint64)*pb.Entry{
	var entry pb.Entry
	logLock.RLock()
	entry.Type = 0//todo: produce configtype
	entry.Term = s.simple_raftlog_Term[index]
	entry.Index = index
	entry.Key = s.simple_raftlog_key[index]
	entry.Value = s.simple_raftlog_Value[index]
	logLock.RUnlock()
	return &entry
}
func (s *server)ProduceMsg(Type uint64,NodeID uint64)(pb.MessageSend){
	var msgSend pb.MessageSend
	serverLock.RLock()
	switch Type{
		case 0://entry
			msgSend.Type = 0
			msgSend.Term = s.currentTerm
			msgSend.NodeID = s.NodeID
			msgSend.CommitIndex = s.CommitIndex
			logLock.RLock()
			IndexLock.RLock()
			msgSend.LogIndex = s.nextIndex[NodeID] - 1
			//fmt.Println(msgSend.LogIndex,NodeID,s.nextIndex[NodeID])
			msgSend.LogTerm = s.simple_raftlog_Term[s.nextIndex[NodeID]-1]
			IndexLock.RUnlock()
			logLock.RUnlock()
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
			logLock.RLock()
			IndexLock.RLock()
			msgSend.LogIndex = s.nextIndex[NodeID] - 1
			msgSend.LogTerm = s.simple_raftlog_Term[s.nextIndex[NodeID]-1]
			IndexLock.RUnlock()
			logLock.RUnlock()
			msgSend.CommitIndex = s.CommitIndex
	}
	serverLock.RUnlock()
	return msgSend
}
func (s *server)HandleReturn(Ret *pb.MessageRet,NodeID uint64,LogIndex uint64){
	start:=time.Now().UnixNano()
	//log.Println("node:",s.NodeID,"handling return")
	if Ret == nil{
		log.Println("bug find!!!!")
	}
	//log.Println("node",NodeID,Ret.Type,"return:",Ret.Success)
	serverLock.RLock()
	if Ret.Term > s.currentTerm{//this server is out-of-date
		serverLock.RUnlock()
		serverLock.Lock()
		s.currentTerm = Ret.Term
		serverLock.Unlock()
		s.BecomeFollower()
		return
	}else if Ret.Term < s.currentTerm{//out of date
		serverLock.RUnlock()
		return
	}else{
		serverLock.RUnlock()
	}
	switch Ret.Type{
		case 0:
			IndexLock.Lock()
			if Ret.Success == 0{
				//fail to add
				//since we know that the entry can't be added
				//we should update according to LogIndex
				//it is always true that LogIndex <= s.nextIndex[NodeID]-1 
				//so we don't have to set a condition
				s.nextIndex[NodeID] = LogIndex
			}else if Ret.Success == 1{//successfully add an entry
				//since we know that the entry is successfully added in the position of LogIndex
				//we should update accroding to LogIndex
				if LogIndex >= s.nextIndex[NodeID]-1{
					s.matchIndex[NodeID] = LogIndex+1
					s.nextIndex[NodeID] = LogIndex+2
				}
			}
			IndexLock.Unlock()
		case 1:
			if Ret.Success == 1{
				log.Println("Recivie vote from",NodeID)
				serverLock.Lock()
				s.votes = s.votes + 1
				serverLock.Unlock()
			}
		case 2://heartbeat
			IndexLock.Lock()
			if Ret.Success == 0{
				//it is always true that LogIndex <= s.nextIndex[NodeID]-1
				//so we don't have to set a condition
				s.nextIndex[NodeID] = LogIndex
			}else{
				if LogIndex >= s.nextIndex[NodeID]-1{
					//since we know that it is matched at logindex
					s.matchIndex[NodeID] = LogIndex
					s.nextIndex[NodeID] = LogIndex+1
				}
			}
			IndexLock.Unlock()
	}
	end:=time.Now().UnixNano()
	HandleReturnTime += (end - start)
}
func ResetClock(){
	//log.Println("Clock reset")
	timerLock.Lock()
	timer.Reset(time.Duration(rand.Intn(400)+100)*time.Millisecond)
	timerLock.Unlock()
}
func StopClock(){
	//log.Println("Clock stoped")
	timerLock.Lock()
	timer.Stop()
	timerLock.Unlock()
}
func (s *server)SendEntryRpc(NodeID uint64){
	//log.Println("node:",s.NodeID,":sending EntryRpc to",NodeID)
	start:=time.Now().UnixNano()
	msgSend := s.ProduceMsg(0,NodeID)		
	ctx, _ := context.WithTimeout(context.TODO(),time.Millisecond*10)
	msgRet, err := clients[NodeID-1].TransMsg(ctx,&msgSend)
	if err == nil {
		s.HandleReturn(msgRet,NodeID,msgSend.LogIndex)
	}
	end:=time.Now().UnixNano()
	RpcCostTime += (end - start)
}
func (s *server)BroadcastRpc(){
	//log.Println("node:",s.NodeID,":Broadcasting Rpc")
	i:=0
	for i<=4{
		i++
		if uint64(i) == s.NodeID{
			continue
		}
		go s.SendEntryorHeartBeat(uint64(i))
	}
}
func (s *server)SendEntryorHeartBeat(NodeID uint64){
	//log.Println("node:",s.NodeID,":sending heartbeat/entryRpc to ",NodeID)
	//accroding to nextindex[] to decide whether we send app or heartbeat
	//accroding to state to decide whether to send or stop
	logLock.RLock()
	IndexLock.RLock()
	if s.nextIndex[NodeID] > s.simple_raftlog_length{//send heartbeat
		IndexLock.RUnlock()
		logLock.RUnlock()
		s.SendHeartBeat(NodeID)
	}else{
		IndexLock.RUnlock()
		logLock.RUnlock()
		s.SendEntryRpc(NodeID)
	}
}

func (s *server)BroadcastEntryRpc(){
	//log.Println("node:",s.NodeID,":Broadcasting EntryRpc")
	i:=0
	for i<=4{
		i++
		if uint64(i) == s.NodeID{
			continue
		}
		go s.SendEntryRpc(uint64(i))
	}
}
func (s *server)SendHeartBeat(NodeID uint64){
	//log.Println("node:",s.NodeID,":sending heartbeat to ",NodeID)
	start:=time.Now().UnixNano()
	msgSend := s.ProduceMsg(2,NodeID)
	ctx, _ := context.WithTimeout(context.TODO(),time.Millisecond*10)
	msgRet, err := clients[NodeID-1].TransMsg(ctx,&msgSend)
	if err == nil {
		s.HandleReturn(msgRet,NodeID,msgSend.LogIndex)
	}
	end:=time.Now().UnixNano()
	RpcCostTime = (end - start)
}
func (s *server)BroadcastHeartBeat(){
	//log.Println("node:",s.NodeID,":Broadcasting heartbeat")
	i:=0
	for i<=4{
		i++
		if uint64(i) == s.NodeID{
			continue
		}
		go s.SendHeartBeat(uint64(i))
	}
}
func (s *server)SendVoteRpc(NodeID uint64){
	//log.Println("node:",s.NodeID,":Sending VoteRpc to",NodeID)
	msgSend := s.ProduceMsg(1,NodeID)
	ctx, _ := context.WithTimeout(context.TODO(),time.Millisecond*10)
	msgRet, err := clients[NodeID-1].TransMsg(ctx,&msgSend)
	if err == nil {
		s.HandleReturn(msgRet,NodeID,msgSend.LogIndex)
	}
}
func (s *server)BroadcastVoteRpc(){
	//log.Println("node:",s.NodeID,":Broadcasting VoteRpc")
	i:=0
	for i<=4{
		i++
		if uint64(i) == s.NodeID{
			continue
		}
		go s.SendVoteRpc(uint64(i))
	}
}
func (s *server)BecomeFollower(){
	log.Println("node:",s.NodeID,":BecomeFollower")
	serverLock.Lock()
	s.state = 1
	s.votedFor = 0
	s.votes = 0
	serverLock.Unlock()
}
func (s *server)BecomeLeader(){
	log.Println("node:",s.NodeID,":BecomeLeader")
	serverLock.Lock()
	s.state = 3
	s.leaderId = s.NodeID
	serverLock.Unlock()
	StopClock()
	i:=1
	logLock.RLock()
	IndexLock.Lock()
	for i<=5{
		if uint64(i)==s.NodeID{
			i++
			continue
		}

		s.nextIndex[i] = s.simple_raftlog_length+1
		s.matchIndex[i] = 0
		i++
	}
	IndexLock.Unlock()
	logLock.RUnlock()
	commitTimer := time.NewTimer(1*time.Millisecond)
	for{
		serverLock.RLock()
		state := s.state
		tempCommit := s.CommitIndex
		serverLock.RUnlock()
		switch state{
			case 3://leader state
				commitTimer.Reset(1*time.Millisecond)
				select{
					case <- commitTimer.C:
						temp := s.CheckMatch()
						if temp>tempCommit{
							serverLock.Lock()
							s.CommitIndex = temp
							serverLock.Unlock()
						}
				}
			default:
				return
		}
	}
}
func (s *server)BecomeCandidate(){
	log.Println("node:",s.NodeID,":BecomeCandidate")
	serverLock.Lock()
	s.state = 2
	s.leaderId = 0
	s.currentTerm = s.currentTerm + 1
	s.votedFor = s.NodeID
	s.votes = 1;//vote for himself
	serverLock.Unlock()
	//reset timer
	ResetClock()
	s.BroadcastVoteRpc()
}
func (s *server)ResetServer(){
	s.NodeID = ID
	s.leaderId = 0 //doesn't have a leader
	s.currentTerm = 1
	s.votedFor = 0 //doesn't vote for any one
	s.lastApplied = 0//doesn't apple anything
	s.CommitIndex = 0
	s.simple_kvstore = make(map[string]string)
	s.simple_raftlog_length = 0
	s.votes = 0
	s.state = 1//follower
	for i,serverAddress := range peers{
		if uint64(i+1) == s.NodeID{
			var blank pb.MsgRpcClient
			conns = append(conns,nil)
			clients = append(clients,blank)
			continue
		}
		conn, _ := grpc.Dial(serverAddress,grpc.WithInsecure())
		conns = append(conns,conn)
		client := pb.NewMsgRpcClient(conn)
		clients = append(clients,client)
	}
	timer = time.NewTimer(time.Duration(rand.Intn(400)+100)*time.Millisecond)
	log.Println("node:",s.NodeID,":Resetting Server")
	go s.TimerFunc()
}
func (s *server)ForwardCommand(Req *pb.Request)*pb.SuccessMsg{
	//log.Println("node:",s.NodeID,":Forwarding message")
	serverLock.RLock()
	leaderId:= s.leaderId-1
	serverLock.RUnlock()
	Msg,_ := clients[leaderId].RecvCmd(context.Background(),Req)
	return Msg
}
func (s *server)CommitLog(){
	for{
		time.Sleep(1*time.Microsecond)
		serverLock.RLock()
		tempIndex := s.CommitIndex
		serverLock.RUnlock()
		for tempIndex>s.lastApplied{
			//log.Println("node:",s.NodeID,":Commiting log",s.lastApplied)
			s.lastApplied = s.lastApplied+1
			s.simple_kvstore[s.simple_raftlog_key[s.lastApplied]] = s.simple_kvstore[s.simple_raftlog_Value[s.lastApplied]]
		}
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
	start:=time.Now().UnixNano()
	//log.Println("node:",s.NodeID,":Reciving cmd from client")
	var RetMsg pb.SuccessMsg
	var index uint64
	RetMsg.Success = true
	serverLock.RLock()
	if s.leaderId !=0 &&s.leaderId != s.NodeID{//leader is not me
		serverLock.RUnlock()
		return s.ForwardCommand(cmd),nil
	}else if s.leaderId == s.NodeID{
		serverLock.RUnlock()
		index = s.AddEntry(cmd.Name,cmd.Value,s.currentTerm)
		s.BroadcastEntryRpc()
	}else
	{
		for s.leaderId==0{
			serverLock.RUnlock()
			time.Sleep(1*time.Millisecond)
			serverLock.RLock()
		}
		serverLock.RUnlock()
		return s.ForwardCommand(cmd),nil
	}
	serverLock.RLock()
	for index>s.CommitIndex{
		serverLock.RUnlock()
		time.Sleep(1*time.Millisecond)
		serverLock.RLock()
	}
	serverLock.RUnlock()
	count++
	end:=time.Now().UnixNano()
	RecvCmdTime += (end - start)
	return &RetMsg,nil
}
//just a test func
func AnounanceTime(){
	for {
		fmt.Println(time.Now())
		time.Sleep(5*time.Second)
	}
}
//check matchindex to update commitindex
func (s *server)CheckMatch()uint64{
	start:=time.Now().UnixNano()
	i := 1
	var result int
	var check []int
	IndexLock.RLock()
	for i<=5{
		if uint64(i)==s.NodeID{
			i++
			continue
		}
		check = append(check,int(s.matchIndex[i]))
		i++
	}
	IndexLock.RUnlock()
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
	end:=time.Now().UnixNano()
	CheckMatchTime += (end - start)
	return uint64(result)
}
func (s *server)AnounanceStatus(){
	TransMsgTime = 0
	CheckSendIndexTime = 0
	AddEntryTime = 0
	RecvCmdTime = 0
	HandleReturnTime = 0
	CheckMatchTime = 0
	for{
		preTransMsgTime := TransMsgTime
		preCheckSendIndexTime := CheckSendIndexTime
		preAddEntrytime := AddEntryTime
		preRecvCmdTime := RecvCmdTime
		preHandleReturnTime := HandleReturnTime
		preCheckMatchTime := CheckMatchTime
		precount := count
		time.Sleep(5*time.Second)
		//fmt.Println("commitindex:",s.CommitIndex,"loglength:",s.simple_raftlog_length)
		//fmt.Println("leaderID",s.leaderId,"lastApplied",s.lastApplied)
		//fmt.Println("nextindex:",s.nextIndex[1],s.nextIndex[2],s.nextIndex[3],s.nextIndex[4],s.nextIndex[5])
		//fmt.Println("matchindex:",s.matchIndex[1],s.matchIndex[2],s.matchIndex[3],s.matchIndex[4],s.matchIndex[5])
		//time.Sleep(time.Second)
		currentcount := count
		currentTransMsgTime := TransMsgTime
		currentCheckSendIndexTime := CheckSendIndexTime
		currentAddEntryTime := AddEntryTime
		currentRecvCmdTime := RecvCmdTime
		currentHandleReturnTime := HandleReturnTime
		currentCheckMatchTime := CheckMatchTime
		averange := currentcount - precount
		fmt.Println("qps:",(currentcount - precount)/5)
		if averange != 0{
			fmt.Println("latency:",(currentRecvCmdTime - preRecvCmdTime)/(averange*int64(time.Millisecond)))
		}
		fmt.Println("CheckMatchTime:",(currentCheckMatchTime - preCheckMatchTime)/(5*int64(time.Millisecond)))
		fmt.Println("HandleReturnTime",(currentHandleReturnTime - preHandleReturnTime)/(5*int64(time.Millisecond)))
		fmt.Println("TransMsgTime:",(currentTransMsgTime - preTransMsgTime)/(5*int64(time.Millisecond)))
		fmt.Println("AddEntryTime:",(currentAddEntryTime - preAddEntrytime)/(5*int64(time.Millisecond)))
		fmt.Println("CheckSendIndexTime:",(currentCheckSendIndexTime - preCheckSendIndexTime)/(5*int64(time.Millisecond)))
	}
}

func (s *server)MaintainState(){
	RpcTimer := time.NewTimer(30*time.Millisecond)
	for{
		serverLock.RLock()
		state := s.state
		serverLock.RUnlock()
		switch state{
			case 1://follower state,do nothing
				time.Sleep(1*time.Millisecond)
			case 2://candidate state
				time.Sleep(1*time.Millisecond)
				serverLock.RLock()
				votes := s.votes
				serverLock.RUnlock()
				if votes >= 3{
					go s.BecomeLeader()
				}
			case 3://leader state
				go s.BroadcastRpc()
				RpcTimer.Reset(30*time.Millisecond)
				select{
					case <- RpcTimer.C:

				}
		}
	}
}

func main(){
	fmt.Scanf("%v",&ID)
	port = []string{":50051",
					 ":50052",
					 ":50053",
					 ":50054",
					 ":50055"}
	peers = []string{	"127.0.0.1:50051",
						"127.0.0.1:50052",
						"127.0.0.1:50053",
						"127.0.0.1:50054",
						"127.0.0.1:50055"}
	lis, err := net.Listen("tcp",port[ID-1])
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
	go raftserver.AnounanceStatus()
	//go raftserver.TimerFunc() extra func
	grpcserver.Serve(lis)//begin to serve
	//go AnounanceTime()
	for{
		precount := count
		time.Sleep(time.Second)
		currentcount := count
		averange := currentcount - precount 
		fmt.Println("may be qps?:",averange)
		//time.Sleep(3*time.Second)
		//log.Println("node:",raftserver.NodeID,":Commiting log",raftserver.lastApplied)
	}
}