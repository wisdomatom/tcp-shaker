package tcp

import (
	"fmt"
	"net"
	"os"
	"time"

	"golang.org/x/sys/unix"
)

const maxEpollEvents = 32

// createSocket creates a socket with necessary options set.
func createSocketZeroLinger(family int, sourceAddr string, zeroLinger bool) (fd int, err error) {
	// Create socket
	fd, err = _createNonBlockingSocket(family, sourceAddr)
	if err == nil {
		if zeroLinger {
			err = _setZeroLinger(fd)
		}
	}
	return
}

// createNonBlockingSocket creates a non-blocking socket with necessary options all set.
func _createNonBlockingSocket(family int, sourceAddr string) (int, error) {
	// Create socket
	fd, err := _createSocket(family)
	if err != nil {
		return 0, err
	}
	// bind socket
	if sourceAddr != "" {
		err = _bindSocket(fd, sourceAddr)
		if err != nil {
			unix.Close(fd)
			return fd, err
		}
	}
	// Set necessary options
	err = _setSockOpts(fd)
	if err != nil {
		unix.Close(fd)
	}
	return fd, err
}

func _bindSocket(fd int, addr string) error {
	a := unix.SockaddrInet4{}
	ip := net.ParseIP(addr)
	if ip != nil {
		a.Addr = [4]byte{ip.To4()[0], ip.To4()[1], ip.To4()[2], ip.To4()[3]}
	}
	return unix.Bind(fd, &a)
}

// createSocket creates a socket with CloseOnExec set
func _createSocket(family int) (int, error) {
	fd, err := unix.Socket(family, unix.SOCK_STREAM, 0)
	unix.CloseOnExec(fd)
	return fd, err
}

// setSockOpts sets SOCK_NONBLOCK and TCP_QUICKACK for given fd
func _setSockOpts(fd int) error {
	err := unix.SetNonblock(fd, true)
	if err != nil {
		return err
	}
	return unix.SetsockoptInt(fd, unix.IPPROTO_TCP, unix.TCP_QUICKACK, 0)
}

var zeroLinger = unix.Linger{Onoff: 1, Linger: 0}

// setLinger sets SO_Linger with 0 timeout to given fd
func _setZeroLinger(fd int) error {
	return unix.SetsockoptLinger(fd, unix.SOL_SOCKET, unix.SO_LINGER, &zeroLinger)
}

func createPoller() (fd int, err error) {
	fd, err = unix.EpollCreate1(unix.EPOLL_CLOEXEC)
	if err != nil {
		err = os.NewSyscallError("epoll_create1", err)
	}
	return fd, err
}

// registerEvents registers given fd with read and write events.
func registerEvents(pollerFd int, fd int) error {
	var event unix.EpollEvent
	event.Events = unix.EPOLLOUT | unix.EPOLLIN | unix.EPOLLET
	event.Fd = int32(fd)
	if err := unix.EpollCtl(pollerFd, unix.EPOLL_CTL_ADD, fd, &event); err != nil {
		return os.NewSyscallError(fmt.Sprintf("epoll_ctl(%d, ADD, %d, ...)", pollerFd, fd), err)
	}
	return nil
}

func pollEvents(pollerFd int, timeout time.Duration) ([]event, error) {
	var timeoutMS = int(timeout.Nanoseconds() / 1000000)
	var epollEvents [maxEpollEvents]unix.EpollEvent
	nEvents, err := unix.EpollWait(pollerFd, epollEvents[:], timeoutMS)
	if err != nil {
		if err == unix.EINTR {
			return nil, nil
		}
		return nil, os.NewSyscallError("epoll_wait", err)
	}

	var events = make([]event, 0, nEvents)

	for i := 0; i < nEvents; i++ {
		var fd = int(epollEvents[i].Fd)
		var evt = event{Fd: fd, Err: nil}

		errCode, err := unix.GetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_ERROR)
		if err != nil {
			evt.Err = os.NewSyscallError("getsockopt", err)
		}
		if errCode != 0 {
			evt.Err = newErrConnect(errCode)
		}
		events = append(events, evt)
	}
	return events, nil
}
