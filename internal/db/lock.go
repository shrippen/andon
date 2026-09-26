package db

import (
	"errors"
	"os"
	"syscall"
)

// ErrLocked means another Andon process holds the database.
var ErrLocked = errors.New("db: locked by another process")

const lockMode = 0o600

// Held is a taken process lock (andon.db.lock next to the database). Two
// servers on one database would run every background job twice: hints
// resolve in one and reopen in the other.
type Held struct{ f *os.File }

// Lock takes the process lock for the database at path without waiting;
// ErrLocked when another process has it. The kernel drops it when the
// process ends, so a crash leaves nothing stale.
func Lock(path string) (*Held, error) {
	f, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, lockMode)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, ErrLocked
		}
		return nil, err
	}
	return &Held{f: f}, nil
}

// Release gives the lock back.
func (h *Held) Release() {
	_ = syscall.Flock(int(h.f.Fd()), syscall.LOCK_UN)
	h.f.Close()
}
