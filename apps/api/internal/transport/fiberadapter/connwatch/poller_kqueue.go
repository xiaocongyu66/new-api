//go:build darwin || freebsd || netbsd || openbsd || dragonfly

package connwatch

import (
	"errors"

	"golang.org/x/sys/unix"
)

// kqueue-based poller. EVFILT_READ also fires when data is readable, so
// we only treat it as a disconnect when EV_EOF (or EV_ERROR) is set.
//
// EVFILT_USER is just used to nudge Kevent on close().
type kqueuePoller struct {
	kqfd int
}

const wakeIdent = 1

func newPoller() (poller, error) {
	kqfd, err := unix.Kqueue()
	if err != nil {
		return nil, err
	}
	ev := unix.Kevent_t{
		Ident:  wakeIdent,
		Filter: unix.EVFILT_USER,
		Flags:  unix.EV_ADD | unix.EV_CLEAR,
	}
	if _, err := unix.Kevent(kqfd, []unix.Kevent_t{ev}, nil, nil); err != nil {
		_ = unix.Close(kqfd)
		return nil, err
	}
	return &kqueuePoller{kqfd: kqfd}, nil
}

func (p *kqueuePoller) register(fd int) error {
	ev := unix.Kevent_t{
		Ident:  uint64(fd),
		Filter: unix.EVFILT_READ,
		Flags:  unix.EV_ADD,
	}
	_, err := unix.Kevent(p.kqfd, []unix.Kevent_t{ev}, nil, nil)
	return err
}

func (p *kqueuePoller) deregister(fd int) error {
	ev := unix.Kevent_t{
		Ident:  uint64(fd),
		Filter: unix.EVFILT_READ,
		Flags:  unix.EV_DELETE,
	}
	_, err := unix.Kevent(p.kqfd, []unix.Kevent_t{ev}, nil, nil)
	if errors.Is(err, unix.ENOENT) {
		return nil
	}
	return err
}

func (p *kqueuePoller) wait() ([]int, error) {
	events := make([]unix.Kevent_t, 64)
	for {
		n, err := unix.Kevent(p.kqfd, nil, events, nil)
		if err != nil {
			if errors.Is(err, unix.EINTR) {
				continue
			}
			return nil, err
		}
		fds := make([]int, 0, n)
		for i := range n {
			e := events[i]
			if e.Filter == unix.EVFILT_USER && e.Ident == wakeIdent {
				return nil, errPollerClosed
			}
			if e.Flags&unix.EV_EOF != 0 || e.Flags&unix.EV_ERROR != 0 {
				fds = append(fds, int(e.Ident))
			}
		}
		if len(fds) == 0 {
			continue
		}
		return fds, nil
	}
}

func (p *kqueuePoller) close() error {
	wake := unix.Kevent_t{
		Ident:  wakeIdent,
		Filter: unix.EVFILT_USER,
		Fflags: unix.NOTE_TRIGGER,
	}
	_, _ = unix.Kevent(p.kqfd, []unix.Kevent_t{wake}, nil, nil)
	return unix.Close(p.kqfd)
}

var errPollerClosed = errors.New("connwatch: poller closed")
