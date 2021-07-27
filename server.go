package main

import (
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"golang.org/x/sys/unix"

	"k8s.io/klog/v2"
)

// TODO variable
// if socket exists means there is an instance running
// ask for socket takeover
// otherwise create the socket and bind directly
const socketPath = "/tmp/zero-downtime.sock"

type Server struct {
	socket  string // path to the unix socket
	address string // ip:port
}

// handle zero downtime requests
func (s *Server) zeroDowntimeHandler(fd int) error {
	uds, err := net.Listen("unix", s.socket)
	if err != nil {
		return err
	}
	klog.Infof("Listen on socket %s for zero downtime", s.socket)

	err = os.Chmod(s.socket, 0777)
	if err != nil {
		return err
	}

	for {
		// block in each connection
		conn, err := uds.Accept()
		if err != nil {
			return err
		}
		// get the connection fd
		udsFd, err := getConnFd(conn.(*net.UnixConn))
		if err != nil {
			klog.Warning("could get connection fd")
			conn.Close()
			continue
		}
		// check if it's a legit request
		buf := make([]byte, 1024)
		size, err := conn.Read(buf)
		if err != nil {
			klog.Warningf("Error reading buffer from UDS connection: %v", err)
			conn.Close()
			continue
		}
		data := string(buf[:size])
		klog.Infof("Read new data from connection: %s", data)
		if data != "ZERO" {
			klog.Infof("unexpected connection")
			conn.Close()
			continue

		}
		// get the connection fd
		klog.Infof("ZERO DOWNTIME starting, connection received on %v from %v", conn.LocalAddr(), conn.RemoteAddr())

		rights := unix.UnixRights(int(fd))
		err = unix.Sendmsg(udsFd, nil, rights, nil, 0)
		if err != nil {
			klog.Warning("could not send file decriptor over socket")
			conn.Close()
			return err
		}
		// exit and clean
		conn.Close()
		uds.Close()
		return nil
	}

}

func (s *Server) ListenAndServe() error {
	var listener net.Listener
	var err error
	errHTTP := make(chan error)
	errZero := make(chan error)
	// zerodowntime exit
	defer func() {

	}()

	if s.socketExist() {
		klog.Infof("Socket exists %s trying to get the listener", s.socket)
		// initiate zero downtime process
		// politely ask the other to handover their sockets and files
		// so we can take the connections and the logs
		conn, err := net.Dial("unix", s.socket)
		if err != nil {
			return err
		}
		defer conn.Close()

		// get the connection fd ( a duplicate to avoid os.EAGAIN)
		klog.Infof("Connected on %s, getting file descriptor", s.socket)
		// udsFd, err := getConnFd(conn.(*net.UnixConn))
		// if err != nil {
		//	return err
		//}
		udsFileDup, err := conn.(*net.UnixConn).File()
		if err != nil {
			return err
		}
		udsFd := int(udsFileDup.Fd())
		// inform we want the socket
		if _, err := conn.Write([]byte("ZERO")); err != nil {
			return err
		}

		// receive socket control message
		klog.Infof("Waiting for listener file descriptor on uds %s", s.socket)
		b := make([]byte, unix.CmsgSpace(4))
		_, _, _, _, err = unix.Recvmsg(udsFd, nil, b, 0)
		if err != nil {
			return err
		}

		// parse socket control message
		cmsgs, err := unix.ParseSocketControlMessage(b)
		if err != nil {
			return err
		}
		fds, err := unix.ParseUnixRights(&cmsgs[0])
		if err != nil {
			return err
		}
		fd := fds[0]
		klog.Infof("Got socket fd %d\n", fd)

		// construct net listener
		zF := os.NewFile(uintptr(fd), "listener")

		listener, err = net.FileListener(zF)
		if err != nil {
			return err
		}
		klog.Infof("New listener created from fd %d on address %v", fd, listener.Addr())

	} else {
		klog.Infof("Socket doesn't exist, creating server on %s", s.address)
		// we are the first
		// create the http listener
		listener, err = net.Listen("tcp", s.address)
		if err != nil {
			return err
		}
		klog.Infof("New listener created on address %v", listener.Addr())
	}

	defer listener.Close()

	// run the http server based on the new or received listener
	go func() {
		errHTTP <- http.Serve(listener, http.FileServer(http.Dir("./")))
	}()

	// obtain the listener fd (bind socket)
	// and start the zeroDowntime process
	// pass listener fd
	listenerFd, err := getConnFd(listener.(*net.TCPListener))
	if err != nil {
		return err
	}
	go func() {
		// wait until the primary leaves
		for {
			if !s.socketExist() {
				break
			}
			klog.Info("waiting primary leaves to start our zero downtime handler")
			time.Sleep(1 * time.Second)
		}
		errZero <- s.zeroDowntimeHandler(listenerFd)
	}()

	select {
	case err := <-errZero:
		// wait for graceful shutdown
		klog.Infoln("Gracefully stopping")
		time.Sleep(5 * time.Second)
		return err
	case err := <-errHTTP:
		klog.Infof("http server error: %v", err)
		return nil
	}

}

func (s *Server) socketExist() bool {
	_, err := os.Stat(s.socket)
	if err != nil {
		if os.IsNotExist(err) {
			return false
		}
		// TODO handle other errors
		return false
	}
	return true
}

func main() {
	// Enable signal handler
	signalCh := make(chan os.Signal, 2)
	defer func() {
		close(signalCh)
	}()

	signal.Notify(signalCh, os.Interrupt, unix.SIGINT)
	go func() {
		select {
		case <-signalCh:
			log.Printf("Exiting: received signal")
			syscall.Unlink(socketPath)
		}

	}()

	s := Server{}
	s.socket = socketPath
	s.address = "localhost:9090"

	if err := s.ListenAndServe(); err != nil {
		syscall.Unlink(socketPath)
		klog.Fatal(err)
	}

	klog.Infoln("Exiting ........")
}

// https://gist.github.com/kirk91/ec25703848172e8f56f671e0e1c73751
func getConnFd(conn syscall.Conn) (connFd int, err error) {
	var rawConn syscall.RawConn
	rawConn, err = conn.SyscallConn()
	if err != nil {
		return
	}

	err = rawConn.Control(func(fd uintptr) {
		connFd = int(fd)
	})
	return
}
