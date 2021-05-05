package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"sync"

	"github.com/hashicorp/raft"
	"github.com/tidwall/buntdb"
)

/*
Finite State Machine. In hashicorp/raft, you must implement raft.FSM interface to create Finite State Machine. It consists of 3 functions:
* Apply will be invoked when Raft already committed log entries in step 2.
* Snapshot is used to support log compaction. This can be used to save a point-in-time snapshot of the FSM.
* Restore is used to restore an FSM from a snapshot.
*/
type fsm struct {
	mu     sync.Mutex
	db     *buntdb.DB
	logger *log.Logger
}

// newFSM returns a new Fsm.
func newFSM(db *buntdb.DB) *fsm {
	return &fsm{
		db:     db,
		logger: log.New(os.Stderr, "[fsm] ", log.LstdFlags),
	}
}

// Apply applies a Raft log entry to the key-value store.
func (f *fsm) Apply(l *raft.Log) interface{} {
	var c command
	if err := json.Unmarshal(l.Data, &c); err != nil {
		f.logger.Printf("failed to unmarshal command: %s", err.Error())
		panic(err)
	}

	switch c.Op {
	case "set":
		return f.applySet(c.Key, c.Value)
	default:
		panic(fmt.Sprintf("unrecognized command op: %s", c.Op))
	}
}

// Snapshot returns a snapshot of the key-value store.
func (f *fsm) Snapshot() (raft.FSMSnapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	return newSnapshotNoop()
}

// Restore stores the key-value store to a previous state.
func (f *fsm) Restore(rc io.ReadCloser) error {
	defer func() {
		if err := rc.Close(); err != nil {
			f.logger.Printf("[FINALLY RESTORE] close error %s\n", err.Error())
		}
	}()

	f.logger.Printf("[START RESTORE] read all message from snapshot\n")
	var totalRestored int

	decoder := json.NewDecoder(rc)
	for decoder.More() {
		var data = &command{}
		err := decoder.Decode(data)
		if err != nil {
			f.logger.Printf("[END RESTORE] error decode data %s\n", err.Error())
			return err
		}

		err = f.db.Update(func(tx *buntdb.Tx) error {
			_, _, err := tx.Set(data.Key, data.Value, nil)
			return err
		})

		if err != nil {
			f.logger.Printf("[END RESTORE] error persist data %s\n", err.Error())
			return err
		}

		totalRestored++
	}

	// read closing bracket
	_, err := decoder.Token()
	if err != nil {
		f.logger.Printf("[END RESTORE] error %s\n", err.Error())
		return err
	}

	f.logger.Printf("[END RESTORE] success restore %d messages in snapshot\n", totalRestored)
	return nil
}

func (f *fsm) applySet(key, value string) interface{} {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.db.Update(func(tx *buntdb.Tx) error {
		_, _, err := tx.Set(key, value, nil)
		return err
	})
}

// snapshotNoop handle noop snapshot
type snapshotNoop struct{}

// Persist persist to disk. Return nil on success, otherwise return error.
func (s snapshotNoop) Persist(_ raft.SnapshotSink) error { return nil }

// Release release the lock after persist snapshot.
// Release is invoked when we are finished with the snapshot.
func (s snapshotNoop) Release() {}

// newSnapshotNoop is returned by an FSM in response to a snapshotNoop
// It must be safe to invoke FSMSnapshot methods with concurrent
// calls to Apply.
func newSnapshotNoop() (raft.FSMSnapshot, error) {
	return &snapshotNoop{}, nil
}
