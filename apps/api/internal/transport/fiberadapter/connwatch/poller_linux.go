//go:build linux

package connwatch

import (
	"errors"

	"golang.org/x/sys/unix"
)

// epoll-based poller. Watches EPOLLRDHUP (peer half-closed write side, i.e.
// FIN), EPOLLHUP (both halves), and EPOLLERR (errors including RST).
//
// Stays level-triggered — we DEL the fd after each event so it never
// repeats.
type epollPoller struct {
	epfd   int
	wakeup [2]int // pipe; close writer side to make EpollWait return
}

func newPoller() (poller, error) {
	epfd, err := unix.EpollCreate1(unix.EPOLL_CLOEXEC)
	if err != nil {
		return nil, err
	}

	var pipes [2]int
	if err := unix.Pipe2(pipes[:], unix.O_CLOEXEC|unix.O_NONBLOCK); err != nil {
		_ = unix.Close(epfd)
		return nil, err
	}

	// Register the pipe read end so closing the write end wakes EpollWait via EPOLLHUP.
	ev := unix.EpollEvent{Events: unix.EPOLLIN, Fd: int32(pipes[0])}
	if err := unix.EpollCtl(epfd, unix.EPOLL_CTL_ADD, pipes[0], &ev); err != nil {
		_ = unix.Close(epfd)
		_ = unix.Close(pipes[0])
		_ = unix.Close(pipes[1])
		return nil, err
	}

	return &epollPoller{epfd: epfd, wakeup: pipes}, nil
}

func (p *epollPoller) register(fd int) error {
	ev := unix.EpollEvent{
		Events: unix.EPOLLRDHUP | unix.EPOLLHUP | unix.EPOLLERR,
		Fd:     int32(fd),
	}
	return unix.EpollCtl(p.epfd, unix.EPOLL_CTL_ADD, fd, &ev)
}

func (p *epollPoller) deregister(fd int) error {
	err := unix.EpollCtl(p.epfd, unix.EPOLL_CTL_DEL, fd, nil)
	if errors.Is(err, unix.ENOENT) {
		return nil
	}
	return err
}

func (p *epollPoller) wait() ([]int, error) {
	events := make([]unix.EpollEvent, 64)
	for {
		n, err := unix.EpollWait(p.epfd, events, -1)
		if err != nil {
			if errors.Is(err, unix.EINTR) {
				continue
			}
			return nil, err
		}
		fds := make([]int, 0, n)
		for i := range n {
			fd := int(events[i].Fd)
			if fd == p.wakeup[0] {
				return nil, errPollerClosed
			}
			fds = append(fds, fd)
		}
		if len(fds) == 0 {
			continue
		}
		return fds, nil
	}
}

func (p *epollPoller) close() error {
	_ = unix.Close(p.wakeup[1])
	_ = unix.Close(p.wakeup[0])
	return unix.Close(p.epfd)
}

var errPollerClosed = errors.New("connwatch: poller closed")
