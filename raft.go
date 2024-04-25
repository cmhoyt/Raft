package raft

//
// this is an outline of the API that raft must expose to
// the service (or tester). see comments below for
// each of these functions for more details.
//
// rf = Make(...)
//   create a new Raft server.
// rf.Start(command interface{}) (index, term, isleader)
//   start agreement on a new log entry
// rf.GetState() (term, isLeader)
//   ask a Raft for its current term, and whether it thinks it is leader
// ApplyMsg
//   each time a new entry is committed to the log, each Raft peer
//   should send an ApplyMsg to the service (or tester)
//   in the same server.
//

import (
	"sync"
	"sync/atomic"
	"time"
	"math/rand"
	"math"
	//"fmt"
	
	"cs350/labrpc"
)

import "bytes"
import "cs350/labgob"

// as each Raft peer becomes aware that successive log entries are
// committed, the peer should send an ApplyMsg to the service (or
// tester) on the same server, via the applyCh passed to Make(). set
// CommandValid to true to indicate that the ApplyMsg contains a newly
// committed log entry.
//
// in part 2D you'll want to send other kinds of messages (e.g.,
// snapshots) on the applyCh, but set CommandValid to false for these
// other uses.
type ApplyMsg struct {
	CommandValid bool
	Command      interface{}
	CommandIndex int

	// For 2D:
	SnapshotValid bool
	Snapshot      []byte
	SnapshotTerm  int
	SnapshotIndex int
}

type Entry struct{
	Term	int
	Command interface{}
}

// A Go object implementing a single Raft peer.
type Raft struct {
	mu        sync.Mutex          // Lock to protect shared access to this peer's state
	peers     []*labrpc.ClientEnd // RPC end points of all peers
	persister *Persister          // Object to hold this peer's persisted state
	me        int                 // this peer's index into peers[]
	dead      int32               // set by Kill()

	currentTerm int				  // latest term server has seen
	votedFor	int				  // candidateId that received vote in current term
	log 		[]Entry			  	  // log entries; each entry contains command for state machine, and term when entry was received by leader
	commitIndex int				  // index of highest log entry known to be committed
	lastApplied int				  // index of highest log entry applied to state machine
	nextIndex	[]int			  // for each server, index of the next log entry to send to that server
	matchIndex 	[]int			  // for each server, index of highest log entry known to be replicated on server

	heartbeat	bool
	state		int				  // 0 = follower, 1 = leader, -1 = candidate
	voteCount	int
	applyCh     chan ApplyMsg

	snapshot 	[]byte
	// Your data here (2A, 2B, 2C).
	// Look at the paper's Figure 2 for a description of what
	// state a Raft server must maintain.

}

// return currentTerm and whether this server
// believes it is the leader.
func (rf *Raft) GetState() (int, bool) {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	var term int
	var isleader bool
	// Your code here (2A).
	if rf.state == 1 {
		isleader = true
	} else {
		isleader = false
	}
	term = rf.currentTerm
	return term, isleader
}

// save Raft's persistent state to stable storage,
// where it can later be retrieved after a crash and restart.
// see paper's Figure 2 for a description of what should be persistent.
func (rf *Raft) persist() {
	// Your code here (2C).
	// Example:
	// w := new(bytes.Buffer)
	// e := labgob.NewEncoder(w)
	// e.Encode(rf.xxx)
	// e.Encode(rf.yyy)
	// data := w.Bytes()
	// rf.persister.SaveRaftState(data)
	w := new(bytes.Buffer)
    e := labgob.NewEncoder(w)
	rf.mu.Lock()
    e.Encode(rf.currentTerm)
    e.Encode(rf.votedFor)
    e.Encode(rf.log)
	rf.mu.Unlock()
    data := w.Bytes()
    rf.persister.SaveRaftState(data)
}

// restore previously persisted state.
func (rf *Raft) readPersist(data []byte) {
	if data == nil || len(data) < 1 { // bootstrap without any state?
		return
	}
	// Your code here (2C).
	// Example:
	// r := bytes.NewBuffer(data)
	// d := labgob.NewDecoder(r)
	// var xxx
	// var yyy
	// if d.Decode(&xxx) != nil ||
	//    d.Decode(&yyy) != nil {
	//   error...
	// } else {
	//   rf.xxx = xxx
	//   rf.yyy = yyy
	// }
	
	
	r := bytes.NewBuffer(data)
    d := labgob.NewDecoder(r)
    var currentTerm int
    var votedFor int
    var log []Entry
    if d.Decode(&currentTerm) != nil || d.Decode(&votedFor) != nil || d.Decode(&log) != nil {
        panic("fail to decode state")
    } else {
		rf.mu.Lock()
        rf.currentTerm = currentTerm
        rf.votedFor = votedFor
        rf.log = log
		rf.mu.Unlock()
    }
}

type InstallSnapshotArgs struct{
	Term				int			//leader's term
	LeaderId			int			//so follower can redirect clients
	LastIncludedIndex	int			//the snapshot replaces all entries up through and including this index
	LastIncludedTerm	int			//term of lastIncludedIndex
	Data				[]byte
	Done				bool		//true if this is the last chunk
}

type InstallSnapshotReply struct{
	Term		int		//currentTerm, for leader to update itself
}

func (rf *Raft) InstallSnapshot(args *InstallSnapshotArgs, reply *InstallSnapshotReply) {
	//rf.mu.Lock()
	//defer rf.mu.Unlock()
	//println("install snapshot")
	reply.Term = rf.currentTerm

	if args.Term < rf.currentTerm {
		return
	}

	if args.Term > rf.currentTerm {
		rf.currentTerm = args.Term
		rf.setToFollower()
	}

	if args.LastIncludedIndex <= rf.commitIndex {
		return
	}

	go func() {
		rf.applyCh <- ApplyMsg{
			SnapshotValid: true,
			Snapshot:      args.Data,
			SnapshotTerm:  args.LastIncludedTerm,
			SnapshotIndex: args.LastIncludedIndex,
		}
	}()
}

// A service wants to switch to snapshot.  Only do so if Raft hasn't
// have more recent info since it communicate the snapshot on applyCh.
func (rf *Raft) CondInstallSnapshot(lastIncludedTerm int, lastIncludedIndex int, snapshot []byte) bool {
	
	// Your code here (2D).
	//rf.mu.Lock()
	//defer rf.mu.Unlock()
	//println("condsnapshot")
	
	if lastIncludedIndex <= rf.commitIndex {
		return false
	}

	if lastIncludedIndex > len(rf.log) {
		rf.log = make([]Entry, 1)
	} else {
		rf.log = append([]Entry(nil), rf.log[lastIncludedIndex-rf.commitIndex:]...)
		rf.log[0].Command = nil
	}

	// update dummy entry with lastIncludedTerm and lastIncludedIndex
	rf.log[0].Term = lastIncludedTerm
	rf.lastApplied, rf.commitIndex = lastIncludedIndex, lastIncludedIndex
	var data []byte
	rf.persister.SaveStateAndSnapshot(data, snapshot)
	return true
}

// the service says it has created a snapshot that has
// all info up to and including index. this means the
// service no longer needs the log through (and including)
// that index. Raft should now trim its log as much as possible.
func (rf *Raft) Snapshot(index int, snapshot []byte) {
	// Your code here (2D).
	//rf.mu.Lock()
	//defer rf.mu.Unlock()
	//println("snapshot")
	snapshotIndex := rf.commitIndex
	if index <= snapshotIndex {
		return
	}

	rf.log = append([]Entry(nil), rf.log[index-snapshotIndex:]...)
	rf.log[0].Command = nil
	var data []byte
	rf.persister.SaveStateAndSnapshot(data, snapshot)
}

// example RequestVote RPC arguments structure.
// field names must start with capital letters!
type RequestVoteArgs struct {
	// Your data here (2A, 2B).
	Term	int			// candidates term
	CandidateId int		// candidate requesting vote
	LastLogIndex int 	// index of candidate's last log entry
	LastLogTerm int		// term of candidate's last log entry
}

// example RequestVote RPC reply structure.
// field names must start with capital letters!
type RequestVoteReply struct {
	// Your data here (2A).
	Term	int			// currentTerm, for candidate to update itself
	VoteGranted bool	// true means candidate recieved vote
}

type AppendEntriesArgs struct {
	Term			int			// leaders term
	LeaderId		int			// so follower can redirect clients
	PrevLogIndex	int			// index of log entry immediately preceding new ones
	PrevLogTerm		int			// term of prevLogIndex entry
	Entries 		[]Entry		// log entries to store
	LeaderCommit	int			// leader's commiteIndex
}

type AppendEntriesReply struct {
	Term		int		// current term
	Success		bool 	// true if follower contained entry matching prevLogIndex and prevLogTerm
	Conflict	int		// to decrement nextIndex
}

func (rf *Raft) AppendEntries(args *AppendEntriesArgs, reply *AppendEntriesReply) {
	rf.mu.Lock()
	state := rf.state
	current := rf.currentTerm
	reply.Term = rf.currentTerm
	reply.Success = false
	rf.mu.Unlock()
	defer rf.persist()
	if state != 0{
		rf.setToFollower()
	}

	// Reply false if term < currentTerm
	if args.Term < current{
		rf.mu.Lock()
        reply.Success = false
        reply.Term = rf.currentTerm
		rf.mu.Unlock()
		return
	}

	rf.mu.Lock()
	logIndex := len(rf.log)-1
	rf.mu.Unlock()
	if args.PrevLogIndex > logIndex{
		reply.Conflict = logIndex + 1
		return
	}

	//If RPC request or response contains term T > currentTerm:
	//set currentTerm = T, convert to follower
	if args.Term > current {
		rf.mu.Lock()
		rf.currentTerm = args.Term
		rf.mu.Unlock()
		rf.setToFollower()
	} 
	rf.mu.Lock()
	rf.heartbeat = true
	
	
	// Reply false if log doesn’t contain an entry at prevLogIndex
	//whose term matches prevLogTerm 
	
	lastLogIndex := len(rf.log) - 1
	rf.mu.Unlock()
    if lastLogIndex < args.PrevLogIndex {
		rf.mu.Lock()
        reply.Success = false
        reply.Term = rf.currentTerm
		rf.mu.Unlock()
        return
    }
	
	// If an existing entry conflicts with a new one (same index but different terms), 
	//delete the existing entry and all that follow it
	rf.mu.Lock()
	log := rf.log
	rf.mu.Unlock()
	if log[(args.PrevLogIndex)].Term != args.PrevLogTerm {
		rf.mu.Lock()
        reply.Success = false
        reply.Term = rf.currentTerm
		conflict := rf.log[args.PrevLogIndex].Term
		i := args.PrevLogIndex
		for ; rf.log[i].Term == conflict; i-- {
		}
		reply.Conflict = i + 1
		rf.mu.Unlock()
        return
    }

	//Append any new entries not already in the log
	unmatch_idx := -1
    for idx := range args.Entries {
		rf.mu.Lock()
		log = rf.log
		rf.mu.Unlock()
        if len(log) < (args.PrevLogIndex+2+idx) || log[(args.PrevLogIndex+1+idx)].Term != args.Entries[idx].Term {
            unmatch_idx = idx
            break
        }
    }
	if unmatch_idx != -1 {
		//println("\nappending")
		rf.mu.Lock()
        rf.log = rf.log[:(args.PrevLogIndex + 1 + unmatch_idx)]
        rf.log = append(rf.log, args.Entries[unmatch_idx:]...)
		// println("server: ", rf.me, "\n")
		// for _, value := range rf.log {
		// 	fmt.Printf(" %+v", value)
		// }
		rf.mu.Unlock()
    }

	// If leaderCommit > commitIndex, set commitIndex = min(leaderCommit, index of last new entry)
    reply.Success = true
	rf.mu.Lock()
	commit := rf.commitIndex
	log = rf.log
	rf.mu.Unlock()
	if args.LeaderCommit > commit{
		a := float64(args.LeaderCommit)
		r := float64(len(log)-1)
		rf.setCommit(int(math.Min(a, r)))
	}
}

func (rf *Raft) UpToDate(cLastIndex int, cLastTerm int) bool {
	
	myLastIndex := len(rf.log)-1
	myLastTerm := rf.log[len(rf.log)-1].Term
	
	if cLastTerm == myLastTerm {
		return cLastIndex >= myLastIndex
	}
	return cLastTerm > myLastTerm
}

// example RequestVote RPC handler.
func (rf *Raft) RequestVote(args *RequestVoteArgs, reply *RequestVoteReply) {
	rf.mu.Lock()
	state := rf.state
	rf.mu.Unlock()
	defer rf.persist()
	// Your code here (2A, 2B).
	if state == -1{
		rf.setToFollower()
	}
	rf.mu.Lock()
	current := rf.currentTerm
	rf.mu.Unlock()
	if args.Term < current{
		reply.Term = current
		reply.VoteGranted = false
		return
	} 

	if args.Term > current {
		rf.mu.Lock()
		rf.currentTerm = args.Term
		rf.mu.Unlock()
		rf.setToFollower()
	} 

	rf.mu.Lock()
	defer rf.mu.Unlock()
	if (rf.votedFor == -1 || rf.votedFor == args.CandidateId) && rf.UpToDate(args.LastLogIndex, args.LastLogTerm) {
		reply.Term = rf.currentTerm
		reply.VoteGranted = true
		rf.votedFor = args.CandidateId
		rf.heartbeat = true
	} else {
		rf.heartbeat = false
	}
	
}

// example code to send a RequestVote RPC to a server.
// server is the index of the target server in rf.peers[].
// expects RPC arguments in args.
// fills in *reply with RPC reply, so caller should
// pass &reply.
// the types of the args and reply passed to Call() must be
// the same as the types of the arguments declared in the
// handler function (including whether they are pointers).
//
// The labrpc package simulates a lossy network, in which servers
// may be unreachable, and in which requests and replies may be lost.
// Call() sends a request and waits for a reply. If a reply arrives
// within a timeout interval, Call() returns true; otherwise
// Call() returns false. Thus Call() may not return for a while.
// A false return can be caused by a dead server, a live server that
// can't be reached, a lost request, or a lost reply.
//
// Call() is guaranteed to return (perhaps after a delay) *except* if the
// handler function on the server side does not return.  Thus there
// is no need to implement your own timeouts around Call().
//
// look at the comments in ../labrpc/labrpc.go for more details.
//
// if you're having trouble getting RPC to work, check that you've
// capitalized all field names in structs passed over RPC, and
// that the caller passes the address of the reply struct with &, not
// the struct itself.
func (rf *Raft) sendRequestVote(server int, args *RequestVoteArgs, reply *RequestVoteReply) bool {
	ok := rf.peers[server].Call("Raft.RequestVote", args, reply)
	return ok
}

func (rf *Raft) sendAppendEntries(server int, args *AppendEntriesArgs, reply *AppendEntriesReply) bool {
	ok := rf.peers[server].Call("Raft.AppendEntries", args, reply)
	return ok
}

func (rf *Raft) sendSnapshot(server int, args *InstallSnapshotArgs, reply *InstallSnapshotReply) {
	ok := rf.peers[server].Call("Raft.InstallSnapshot", args, reply)
	if !ok {
		return
	}

	if reply.Term > rf.currentTerm {
		rf.mu.Lock()
		rf.currentTerm = reply.Term
		rf.mu.Unlock()
		rf.setToFollower()
		return
	}

	rf.nextIndex[server] = args.LastIncludedIndex + 1
	rf.matchIndex[server] = args.LastIncludedTerm
}

// the service using Raft (e.g. a k/v server) wants to start
// agreement on the next command to be appended to Raft's log. if this
// server isn't the leader, returns false. otherwise start the
// agreement and return immediately. there is no guarantee that this
// command will ever be committed to the Raft log, since the leader
// may fail or lose an election. even if the Raft instance has been killed,
// this function should return gracefully.
//
// the first return value is the index that the command will appear at
// if it's ever committed. the second return value is the current
// term. the third return value is true if this server believes it is
// the leader.
func (rf *Raft) Start(command interface{}) (int, int, bool) {
	index := -1
	term := -1
	isLeader := true
	// Your code here (2B).
	rf.mu.Lock()
	state := rf.state
	rf.mu.Unlock()
	if state != 1{
		isLeader = false
		return index, term, isLeader
	} else if rf.state == 1{
		term = rf.currentTerm
		rf.mu.Lock()
		rf.log = append(rf.log, Entry{Term: term, Command: command})
		index = len(rf.log)-1
		rf.matchIndex[rf.me] = index
		rf.nextIndex[rf.me] = index + 1
		rf.mu.Unlock()
		rf.persist()
		// println("start")
		// for _, value := range rf.log {
		// 	fmt.Printf(" %+v", value)
		//   }
	}
	
	return index, term, isLeader
}

// the tester doesn't halt goroutines created by Raft after each test,
// but it does call the Kill() method. your code can use killed() to
// check whether Kill() has been called. the use of atomic avoids the
// need for a lock.
//
// the issue is that long-running goroutines use memory and may chew
// up CPU time, perhaps causing later tests to fail and generating
// confusing debug output. any goroutine with a long-running loop
// should call killed() to check whether it should stop.
func (rf *Raft) Kill() {
	atomic.StoreInt32(&rf.dead, 1)
	// Your code here, if desired.
}

func (rf *Raft) killed() bool {
	z := atomic.LoadInt32(&rf.dead)
	return z == 1
}

func (rf *Raft) setToCandidate() {
	// increment currentTerm, vote for self, reset timer, send requestvote
	rf.mu.Lock()
	rf.state = -1
	rf.currentTerm++
	rf.votedFor = rf.me
	rf.mu.Unlock()
	rf.persist()
	//reset election timer happens inside election
	rf.election()
}

func (rf *Raft) setToFollower() {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	rf.state = 0
	rf.votedFor = -1
}

func (rf *Raft) setToLeader() {
	rf.mu.Lock()
	rf.state = 1
	for i := range rf.peers {
		rf.nextIndex[i] = len(rf.log)
		rf.matchIndex[i] = 0
	}
	rf.mu.Unlock()
	rf.requestHeartbeat()
}

func (rf *Raft) election() {
	electionTimeout := electionTimeout()
	startElection := time.Now()
	rf.persist()
	rf.mu.Lock()
	a := RequestVoteArgs{}
	a.Term = rf.currentTerm
	a.CandidateId = rf.me
	a.LastLogIndex = len(rf.log) - 1
	a.LastLogTerm = rf.log[len(rf.log) - 1].Term	
	rf.voteCount = 0
	rf.mu.Unlock()
	for i := range rf.peers{
		if i == rf.me{
			rf.mu.Lock()
			rf.voteCount++
			rf.mu.Unlock()
			continue;
		}
		go func(server int){
			r := RequestVoteReply{}
			if rf.sendRequestVote(server, &a, &r){
				rf.mu.Lock()
				current := rf.currentTerm
				rf.mu.Unlock()
				if r.VoteGranted == true{
					rf.mu.Lock()
					rf.voteCount++
					rf.mu.Unlock()
				} else if r.Term > current {
					rf.mu.Lock()
					rf.currentTerm = r.Term
					rf.mu.Unlock()
			 		rf.setToFollower()
					rf.persist()
				}
			}
		}(i)
	}

	for {
		rf.mu.Lock()
		votes := rf.voteCount
		peers := rf.peers
		rf.mu.Unlock()
		if time.Now().Sub(startElection).Milliseconds() >= electionTimeout.Milliseconds() || votes >= (len(peers)/2)+1{
			//println("election timeout or win")
			break
		}
	}
}

func (rf *Raft) append(){
	
	for i := range rf.peers{
		if i == rf.me{
			continue;
		}
		if rf.nextIndex[i] <= rf.commitIndex {
			reply := InstallSnapshotReply{}
			args := InstallSnapshotArgs{}
			args.Term = rf.currentTerm
			args.LastIncludedIndex = rf.commitIndex
			args.LastIncludedTerm = rf.log[args.LastIncludedIndex].Term
			for _ , value := range rf.log[:args.LastIncludedIndex] {
				args.Data = append(args.Data, byte(value.Term))
			}
			go rf.sendSnapshot(i, &args, &reply)
		}
		rf.mu.Lock()
		args := AppendEntriesArgs{}
		args.Term = rf.currentTerm
		args.LeaderId = rf.me
		args.PrevLogIndex = rf.nextIndex[i] - 1
		args.PrevLogTerm = rf.log[args.PrevLogIndex].Term
		args.LeaderCommit = rf.commitIndex
		args.Entries = rf.log[rf.nextIndex[i]:]
		rf.mu.Unlock()
		go func(server int){
			reply := AppendEntriesReply{}
			if rf.sendAppendEntries(server, &args, &reply){
				//If successful: update nextIndex and matchIndex for follower and possibly commit
				if reply.Success == true{
					rf.mu.Lock()
					rf.matchIndex[server] = args.PrevLogIndex + len(args.Entries)
					rf.nextIndex[server] = rf.matchIndex[server] + 1
					log := rf.log
					commit := rf.commitIndex
					match := rf.matchIndex
					peers := rf.peers
					rf.mu.Unlock()
					//If there exists an N such that N > commitIndex, a majority
					//of matchIndex[i] ≥ N, and log[N].term == currentTerm: set commitIndex = N
					//println("outside loop")
					for N := (len(log) - 1); N > commit; N-- {
						count := 0
						rf.mu.Lock()
						for _, matchIndex := range match {
							if matchIndex >= N {
								count += 1
							}
						}
						rf.mu.Unlock()
					
						if count > len(peers)/2 {
							rf.setCommit(N)
							break
						}
					}
				//If AppendEntries fails because of log inconsistency: decrement nextIndex and retry
				} else{
					rf.mu.Lock()
					current := rf.currentTerm
					rf.mu.Unlock()
					if reply.Term > current {
						rf.mu.Lock()
						rf.currentTerm = reply.Term
						rf.mu.Unlock()
					 	rf.setToFollower()		
						rf.persist()	
					} else {
						//If AppendEntries fails because of log inconsistency: decrement nextIndex and retry
						rf.mu.Lock()
						rf.nextIndex[server] = reply.Conflict
						rf.mu.Unlock()
					}
				}
			}
		}(i)
	}
	
}

func (rf *Raft) setCommit(commitIndex int) {
	rf.mu.Lock()
	defer rf.mu.Unlock()
    rf.commitIndex = commitIndex
	//println("server: ", rf.me, " commitIndex: ", rf.commitIndex, " lastApplied: ", rf.lastApplied, "\n")
	// for _, value := range rf.log.Entries {
	// 	fmt.Printf(" %+v", value)
	// }
	// count := 0
	// for _ , value := range rf.log.Entries {
	// 	count += 1
	// 	println(value.Command)
	// }
	// println("count: ", count)
	// println("outside")
	// println("len: ", len(rf.log))
	if rf.commitIndex > rf.lastApplied {	
		 for i := rf.lastApplied + 1; i <= rf.commitIndex; i++ {
			a := ApplyMsg{}
			a.CommandIndex = i
			a.Command = rf.log[i].Command
			a.CommandValid = true
			rf.applyCh <- a
			rf.lastApplied = i
		}
	}
}

func electionTimeout() time.Duration {
	timeout := 500 + rand.Intn(500)		// 500 ms - 1s
	return time.Duration(timeout) * time.Millisecond
}

// The ticker go routine starts a new election if this peer hasn't received
// heartsbeats recently.
func (rf *Raft) ticker() {
	for rf.killed() == false {
		// Your code here to check if a leader election should
		// be started and to randomize sleeping time using
		// time.Sleep().
		rf.mu.Lock()
		state := rf.state
		rf.mu.Unlock()
		switch state {
		case 0: //follower
			//If election timeout elapses without receiving AppendEntries
			//RPC from current leader or granting vote to candidate: convert to candidate
			//heartbeat update inside append and vote

			electionTimeout := electionTimeout()
			time.Sleep(electionTimeout)
			rf.mu.Lock()
			heartbeat := rf.heartbeat
			rf.mu.Unlock()
			if heartbeat {
				rf.mu.Lock()
				rf.heartbeat = false
				rf.mu.Unlock()
			} else {
				rf.setToCandidate()
			}

		case -1: //candidate
			// if votes from majority then become leader
			// if received append rpc, convert to follower
			// if timeout, start election
			rf.mu.Lock()
			votes := rf.voteCount
			peers := rf.peers
			rf.mu.Unlock()
			if votes >= (len(peers)/2)+1{
				rf.setToLeader()
			} else {
				rf.election()
			}
			
		case 1: // leader
			rf.requestHeartbeat()
			
		}
		time.Sleep(50 * time.Millisecond)
		rf.mu.Lock()
		num := rf.commitIndex
		rf.mu.Unlock()
		rf.setCommit(num)
	}
}

func (rf *Raft) requestHeartbeat(){
	rf.mu.Lock()
	state := rf.state
	rf.mu.Unlock()
	if state != 1{
		return
	}
	rf.append()
}

// the service or tester wants to create a Raft server. the ports
// of all the Raft servers (including this one) are in peers[]. this
// server's port is peers[me]. all the servers' peers[] arrays
// have the same order. persister is a place for this server to
// save its persistent state, and also initially holds the most
// recent saved state, if any. applyCh is a channel on which the
// tester or service expects Raft to send ApplyMsg messages.
// Make() must return quickly, so it should start goroutines
// for any long-running work.
func Make(peers []*labrpc.ClientEnd, me int,
	persister *Persister, applyCh chan ApplyMsg) *Raft {
	rf := &Raft{}
	rf.peers = peers
	rf.persister = persister
	rf.me = me

	// Your initialization code here (2A, 2B, 2C).
	rf.currentTerm = 0
	rf.votedFor = -1		// (null)
	rf.log = []Entry{
		{
			Term: 0,
			Command: 0,
		},
	}
	rf.commitIndex = 0
	rf.lastApplied = 0
	rf.nextIndex = make([]int, len(rf.peers))
	rf.matchIndex = make([]int, len(rf.peers))
	rf.heartbeat = false
	rf.state = 0
	rf.voteCount = 0
	rf.applyCh = applyCh

	rf.snapshot = make([]byte, 0)

	//commit dummy log so indexing is 1 based
	a := ApplyMsg{}
	a.CommandIndex = 0
	a.Command = rf.log[0].Command
	a.CommandValid = true
	rf.applyCh <- a

	// initialize from state persisted before a crash
	rf.readPersist(persister.ReadRaftState())

	// start ticker goroutine to start elections
	go rf.ticker()

	return rf
}
