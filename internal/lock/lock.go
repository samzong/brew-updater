package lock

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type Lock struct {
	path string
}

func Acquire(path string, timeout time.Duration) (*Lock, error) {
	for {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			defer f.Close()
			_, _ = f.WriteString(fmt.Sprintf("%d|%d", os.Getpid(), time.Now().Unix()))
			return &Lock{path: path}, nil
		}
		if !os.IsExist(err) {
			return nil, err
		}

		stale, err := isStale(path, timeout)
		if err != nil {
			return nil, err
		}
		if stale {
			_ = os.Remove(path)
			continue
		}
		return nil, errors.New("lock already held")
	}
}

func isStale(path string, timeout time.Duration) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	parts := strings.Split(string(data), "|")
	if len(parts) != 2 {
		return true, nil
	}
	pid, err := strconv.ParseInt(strings.TrimSpace(parts[0]), 10, 64)
	if err != nil {
		return true, nil
	}
	ts, err := strconv.ParseInt(strings.TrimSpace(parts[1]), 10, 64)
	if err != nil {
		return true, nil
	}
	if !pidAlive(int(pid)) {
		return true, nil
	}
	return time.Since(time.Unix(ts, 0)) > timeout, nil
}

func (l *Lock) Release() error {
	return os.Remove(l.path)
}

func pidAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	if err := proc.Signal(syscall.Signal(0)); err != nil {
		return false
	}
	return true
}
