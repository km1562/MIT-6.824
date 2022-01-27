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
	"bytes"
	"fmt"
	"log"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	//	"6.824/labgob"
	"6.824/labrpc"
)

func init() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	rand.Seed(time.Now().Unix())
}

const (
	ElectionTimeout  = time.Millisecond * 300 // 选举
	HeartBeatTimeout = time.Millisecond * 150 // leader 发送心跳
	ApplyInterval    = time.Millisecond * 100 // apply log
	RPCTimeout       = time.Millisecond * 100
	MaxLockTime      = time.Millisecond * 10 // debug
)

type Role int

const (
	Follower  Role = 0
	Candidate Role = 1
	Leader    Role = 2
)

//
// as each Raft peer becomes aware that successive log entries are
// committed, the peer should send an ApplyMsg to the service (or
// tester) on the same server, via the applyCh passed to Make(). set
// CommandValid to true to indicate that the ApplyMsg contains a newly
// committed log entry.
//
// in part 2D you'll want to send other kinds of messages (e.g.,
// snapshots) on the applyCh, but set CommandValid to false for these
// other uses.
//
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

//
// A Go object implementing a single Raft peer.
//

type logEntry struct {
	Term    int
	Idx     int
	Command interface{}
}

type Raft struct {
	mu        sync.Mutex          // Lock to protect shared access to this peer's state
	peers     []*labrpc.ClientEnd // RPC end points of all peers
	persister *Persister          // Object to hold this peer's persisted state
	me        int                 // this peer's index into peers[]
	dead      int32               // set by Kill()

	// Your data here (2A, 2B, 2C).
	// Look at the paper's Figure 2 for a description of what
	// state a Raft server must maintain.
	role Role
	term int

	electionTimer       *time.Timer
	appendEntriesTimers []*time.Timer
	applyTimer          *time.Timer
	notifyApplyCh       chan struct{}
	stopCh              chan struct{}

	voteFor           int
	logEntries        []logEntry
	applyCh           chan ApplyMsg
	commitIndex       int
	lastSnapshotIndex int
	lastSnapshortTerm int
	lastApplied       int
	nextIndex         []int
	matchIndex        []int

	DebugLog  bool      // print log
	lockStart time.Time // debug 用，找出长时间 lock
	lockEnd   time.Time
	lockName  string
	gid       int
}

// return currentTerm and whether this server
// believes it is the leader.
func (rf *Raft) GetState() (int, bool) {
	rf.lock("get state")
	defer rf.unlock("get state")

	// Your code here (2A).
	return rf.term, rf.role == Leader
}

//
// save Raft's persistent state to stable storage,
// where it can later be retrieved after a crash and restart.
// see paper's Figure 2 for a description of what should be persistent.
//
func (rf *Raft) persist() {
	// Your code here (2C).
	// Example:
	// w := new(bytes.Buffer)
	// e := labgob.NewEncoder(w)
	// e.Encode(rf.xxx)
	// e.Encode(rf.yyy)
	// data := w.Bytes()
	// rf.persister.SaveRaftState(data)
}

//
// restore previously persisted state.
//
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
}

func (rf *Raft) lock(m string) {
	rf.mu.Lock()
	rf.lockStart = time.Now()
	rf.lockName = m
}

func (rf *Raft) unlock(m string) {
	/*
		思路：
		解锁，如果解锁不是同一个并且超时，就记录

		learn：
		1、timr.Timer里居然有个sub函数
	*/
	rf.lockEnd = time.Now()
	rf.lockName = ""
	duration := rf.lockEnd.Sub(rf.lockStart)
	if rf.lockName != "" && duration > MaxLockTime {
		rf.log("lock too long:%s:%s:iskill:%v", m, duration, rf.killed())
	}
	rf.mu.Unlock()
}

func (rf *Raft) changeRole(role Role) {
	/*
		思路：不同的role，执行不同的功能

		learn:
		1、panic
		2、make函数
	*/
	rf.role = role
	switch role {
	case Follower:
	case Candidate:
		rf.term += 1
		rf.voteFor = rf.me
		rf.resetElectionTimer()
	case Leader:
		_, lastLogIndex := rf.lastLogTermIndex()
		rf.nextIndex = make([]int, len(rf.peers))
		for i := 0; i < len(rf.peers); i++ {
			rf.nextIndex[i] = lastLogIndex + 1
		}
		rf.matchIndex = make([]int, len(rf.peers))
		rf.matchIndex[rf.me] = lastLogIndex
		rf.resetElectionTimer()
	default:
		panic("unknown role")
	}
}

func (rf *Raft) lastLogTermIndex() (int, int) {
	term := rf.logEntries[len(rf.logEntries)-1].Term
	index := rf.lastSnapshotIndex + len(rf.logEntries) - 1
	return term, index
}

//
// A service wants to switch to snapshot.  Only do so if Raft hasn't
// have more recent info since it communicate the snapshot on applyCh.
//
func (rf *Raft) CondInstallSnapshot(lastIncludedTerm int, lastIncludedIndex int, snapshot []byte) bool {

	// Your code here (2D).

	return true
}

// the service says it has created a snapshot that has
// all info up to and including index. this means the
// service no longer needs the log through (and including)
// that index. Raft should now trim its log as much as possible.
func (rf *Raft) Snapshot(index int, snapshot []byte) {
	// Your code here (2D).

}

//
// example RequestVote RPC arguments structure.
// field names must start with capital letters!
//

//
// example RequestVote RPC reply structure.
// field names must start with capital letters!
//

//
// example RequestVote RPC handler.
//
//func (rf *Raft) RequestVote(args *RequestVoteArgs, reply *RequestVoteReply) {
//	// Your code here (2A, 2B).
//}

//
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
//
func (rf *Raft) sendRequestVote(server int, args *RequestVoteArgs, reply *RequestVoteReply) bool {
	ok := rf.peers[server].Call("Raft.RequestVote", args, reply)
	return ok
}

//
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
//
func (rf *Raft) Start(command interface{}) (int, int, bool) {

	/*
		思路：
		判断是否是leader，如果是就重设，
		如果不是，就直接返回
	*/
	//index := -1
	//term := -1
	//isLeader := true
	rf.lock("start")
	term := rf.term
	isLeader := rf.role == Leader
	_, lastIndex := rf.lastLogTermIndex()
	index := lastIndex + 1

	if isLeader {
		rf.logEntries = append(rf.logEntries, logEntry{
			Term:    rf.term,
			Command: command,
			Idx:     index,
		})

		rf.matchIndex[rf.me] = index
		rf.persist()
	}

	rf.resetHeartBeatTimers()
	rf.unlock("start")
	return index, term, isLeader
	// Your code here (2B).

	return index, term, isLeader
}

//
// the tester doesn't halt goroutines created by Raft after each test,
// but it does call the Kill() method. your code can use killed() to
// check whether Kill() has been called. the use of atomic avoids the
// need for a lock.
//
// the issue is that long-running goroutines use memory and may chew
// up CPU time, perhaps causing later tests to fail and generating
// confusing debug output. any goroutine with a long-running loop
// should call killed() to check whether it should stop.
//
func (rf *Raft) Kill() {
	/*
		思路
		直接关掉

		learn:
		1、close就可以直接实现了
	*/
	atomic.StoreInt32(&rf.dead, 1)
	// Your code here, if desired.
	close(rf.stopCh)
}

func (rf *Raft) killed() bool {
	z := atomic.LoadInt32(&rf.dead)
	return z == 1
}

func (rf *Raft) log(format string, a ...interface{}) {
	if rf.DebugLog == false {
		return
	}
	term, idx := rf.lastLogTermIndex()
	r := fmt.Sprintf(format, a...)
	s := fmt.Sprintf("gid:%d, me: %d, role:%v,term:%d, commitIdx: %v, snidx:%d, apply:%v, matchidx: %v, nextidx:%+v, lastlogterm:%d,idx:%d",
		rf.gid, rf.me, rf.role, rf.term, rf.commitIndex, rf.lastSnapshotIndex, rf.lastApplied, rf.matchIndex, rf.nextIndex, term, idx)
	log.Printf("%s:log:%s\n", s, r)
}

//func (rf *Raft) startApplyLogs(){
//	/*
//	思路：开始应用日志
//
//	learn:
//	1、rf.applyTimer.Reset(ApplyInterval)--这个是什么意思
//	2、这个make到底是干嘛的
//	3、
//	*/
//	defer rf.applyTimer.Reset(ApplyInterval)
//
//	rf.lock("applyLogs1")
//	if rf.lastApplied < rf.lastSnapshotIndex{
//		msgs = make([]ApplyMsg, 0, 1)
//		msgs = append(msga, ApplyMsg{
//			CommandValid: false,
//			Command: : "installSnapsShot",
//			CommandIndex: rf.lastSnapshotIndex,
//		})
//	}
//}

func (rf *Raft) getLogByIndex(logIndex int) LogEntry {
	idx := logIndex - rf.lastSnapshotIndex
	return rf.logEntries[idx]
}

func (rf *Raft) getRealIdxByLogIndex(logIndex int) int {
	idx := logIndex - rf.lastSnapshotIndex
	if idx < 0 {
		return -1
	} else {
		return idx
	}
}

func randElectionTimeout() time.Duration {

	/*
		思路：设置超时时间

		learn:
		1、time.Duration随机，然后取余数再加上去
	*/
	r := time.Duration(rand.Int63()) % ElectionTimeout
	return ElectionTimeout + r
}

// The ticker go routine starts a new election if this peer hasn't received
// heartsbeats recently.
func (rf *Raft) ticker() {
	for rf.killed() == false {

		// Your code here to check if a leader election should
		// be started and to randomize sleeping time using
		// time.Sleep().

	}
}

//
// the service or tester wants to create a Raft server. the ports
// of all the Raft servers (including this one) are in peers[]. this
// server's port is peers[me]. all the servers' peers[] arrays
// have the same order. persister is a place for this server to
// save its persistent state, and also initially holds the most
// recent saved state, if any. applyCh is a channel on which the
// tester or service expects Raft to send ApplyMsg messages.
// Make() must return quickly, so it should start goroutines
// for any long-running work.
//
func Make(peers []*labrpc.ClientEnd, me int,
	persister *Persister, applyCh chan ApplyMsg, gid ...int) *Raft {
	rf := &Raft{}
	rf.peers = peers
	rf.persister = persister
	rf.me = me
	rf.applyCh = applyCh

	rf.DebugLog = false

	//gid for test
	if len(gid) != 0 {
		rf.gid = gid[0]
	} else {
		rf.gid = -1
	}
	// Your initialization code here (2A, 2B, 2C).

	// initialize from state persisted before a crash

	rf.stopCh = make(chan struct{})
	rf.term = 0
	rf.voteFor = -1
	rf.role = Follower
	rf.logEntries = make([]logEntry, 1)
	rf.readPersist(persister.ReadRaftState())

	rf.electionTimer = time.NewTimer(randElectionTimeout())
	rf.appendEntriesTimers = make([]*time.Timer, len(rf.peers))
	for i, _ := range rf.peers {
		rf.appendEntriesTimers[i] = time.NewTimer(HeartBeatTimeout)
	}
	rf.applyTimer = time.NewTimer(ApplyInterval)
	rf.notiyApplyCh = make(chan struct{}, 100)

	//apply log
	go func() {
		for {
			select {
			case <-rf.stopCh:
				return
			case <-rf.applyTimer.C:
				rf.notifyApplyCh <- struct{}{}
			case <-rf.notifyApplyCh:
				rf.startApplyLogs()
			}
		}
	}()

	// 发起投票
	go func() {
		for {
			select {
			case <-rf.stopCh:
				return
			case <-rf.electionTimer.C:
				rf.startElection()
			}
		}
	}()

	// leader 发送日志
	for i, _ := range peers {
		if i == rf.me {
			continue
		}
		go func(index int) {
			for {
				select {
				case <-rf.stopCh:
					return
				case <-rf.appendEntriesTimers[index].C:
					rf.appendEntriesToPeer(index)
				}
			}
		}(i)

	}
	// start ticker goroutine to start elections
	//go rf.ticker()

	return rf
}
