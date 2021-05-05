package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"sync"
	"time"

	"github.com/hashicorp/raft"
	"github.com/tidwall/buntdb"
)

const (
	raftTimeout = 10 * time.Second
)

// Store is the interface Raft-backed key-value stores must implement
type Store interface {
	Get(key string) (string, error)

	Set(key, value string) error

	Join(nodeID string, addr string) error
}

type command struct {
	Op    string `json:"op,omitempty"`
	Key   string `json:"key,omitempty"`
	Value string `json:"value,omitempty"`
}

// rStore is a simple key-value store, where all changes are made via Raft consensus.
type rStore struct {
	dir      string
	bindAddr string

	raft *raft.Raft // The consensus mechanism
	db   *buntdb.DB // in-mem db
	mu   sync.Mutex

	logger *log.Logger
}

// New returns a new Store.
func NewStore(dir string, bindAddr string, db *buntdb.DB) Store {

	return &rStore{
		dir:      dir,
		bindAddr: bindAddr,
		db:       db,
		logger:   log.New(os.Stderr, "[store] ", log.LstdFlags),
	}
}

func (s *rStore) Open(enableSingle bool, localID string) error {
	// Setup Raft configuration.
	config := raft.DefaultConfig()
	config.LocalID = raft.ServerID(localID)

	// Setup Raft communication.
	addr, err := net.ResolveTCPAddr("tcp", s.bindAddr)
	if err != nil {
		return err
	}
	transport, err := raft.NewTCPTransport(s.bindAddr, addr, 3, 10*time.Second, os.Stderr)
	if err != nil {
		return err
	}

	// Create the snapshot store. This allows the Raft to truncate the log.
	snapshots, err := raft.NewFileSnapshotStore(s.dir, 1, os.Stderr)
	if err != nil {
		return fmt.Errorf("file snapshot store: %s", err)
	}

	// Create the log store and stable store in memory.
	// Note: This can be extended to persistent storage like bolt/badger etc.
	var logStore raft.LogStore
	var stableStore raft.StableStore
	logStore = raft.NewInmemStore()
	stableStore = raft.NewInmemStore()

	// Ma
	ra, err := raft.NewRaft(config, newFSM(s.db), logStore, stableStore, snapshots, transport)
	if err != nil {
		return fmt.Errorf("new raft: %s", err)
	}
	s.raft = ra

	if enableSingle {
		configuration := raft.Configuration{
			Servers: []raft.Server{
				{
					ID:      config.LocalID,
					Address: transport.LocalAddr(),
				},
			},
		}
		ra.BootstrapCluster(configuration)
	}

	return nil
}

// Get returns the value for the given key.
func (s *rStore) Get(key string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var val string
	var bErr error

	err := s.db.View(func(tx *buntdb.Tx) error {
		val, bErr = tx.Get(key)
		if bErr != nil {
			return bErr
		}
		s.logger.Printf("value is %s\n", val)
		return nil
	})

	return val, err
}

// Set sets the value for the given key.
func (s *rStore) Set(key, value string) error {
	if s.raft.State() != raft.Leader {
		return fmt.Errorf("not leader")
	}

	c := &command{
		Op:    "set",
		Key:   key,
		Value: value,
	}
	b, err := json.Marshal(c)
	if err != nil {
		return err
	}

	f := s.raft.Apply(b, raftTimeout)
	return f.Error()
}

// Join joins a node, identified by nodeID and located at addr, to this store.
// The node must be ready to respond to Raft communications at that address.
func (s *rStore) Join(nodeID, addr string) error {
	s.logger.Printf("received join request for remote node %s at %s", nodeID, addr)

	configFuture := s.raft.GetConfiguration()
	if err := configFuture.Error(); err != nil {
		s.logger.Printf("failed to get raft configuration: %v", err)
		return err
	}

	for _, srv := range configFuture.Configuration().Servers {
		// If a node already exists with either the joining node's ID or address,
		// that node may need to be removed from the config first.
		if srv.ID == raft.ServerID(nodeID) || srv.Address == raft.ServerAddress(addr) {
			// However if *both* the ID and the address are the same, then nothing -- not even
			// a join operation -- is needed.
			if srv.Address == raft.ServerAddress(addr) && srv.ID == raft.ServerID(nodeID) {
				s.logger.Printf("node %s at %s already member of cluster, ignoring join request", nodeID, addr)
				return nil
			}

			future := s.raft.RemoveServer(srv.ID, 0, 0)
			if err := future.Error(); err != nil {
				return fmt.Errorf("error removing existing node %s at %s: %s", nodeID, addr, err)
			}
		}
	}

	f := s.raft.AddVoter(raft.ServerID(nodeID), raft.ServerAddress(addr), 0, 0)
	if f.Error() != nil {
		return f.Error()
	}
	s.logger.Printf("node %s at %s joined successfully", nodeID, addr)
	return nil
}
