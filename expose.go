package expose_register

import (
	"context"
	"log"
	"net"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	protoc "github.com/shiyunjin/reverse-grpc/proto"
)

var (
	localConn net.Conn
)

var (
	remoteConnStreamExit   chan struct{}
	remoteConnStreamServer protoc.TCP_ConnectServer
	remoteConnStreamClient protoc.TCP_ConnectClient
)

type gServer struct {
	protoc.UnimplementedTCPServer
}

func SafeClose[T any](c chan T) {
	defer func() {
		if err := recover(); err != nil {
		}
	}()

	if c != nil {
		close(c)
	}
}

func (s *gServer) Connect(stream protoc.TCP_ConnectServer) error {
	SafeClose(remoteConnStreamExit)
	remoteConnStreamExit = make(chan struct{})

	remoteConnStreamServer = stream

	<-remoteConnStreamExit
	return nil
}

func StartServer(remotePort, localPort string) error {
	lis, err := net.Listen("tcp", ":"+remotePort)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	defer lis.Close()

	s := grpc.NewServer()
	protoc.RegisterTCPServer(s, &gServer{})
	go func() {
		if err := s.Serve(lis); err != nil {
			log.Fatalf("failed to serve: %v", err)
		}
	}()
	for {
		if remoteConnStreamServer != nil {
			break
		}

		time.Sleep(1 * time.Second)
	}

	localListen, err := net.Listen("tcp", "0.0.0.0:"+localPort)
	if err != nil {
		return err
	}
	defer localListen.Close()

	localConn, err = localListen.Accept()
	if err != nil {
		return err
	}

	statusRemote := make(chan bool)
	statusLocal := make(chan bool)
	exitChan := make(chan struct{})

	go pipeSocketServer(true, statusRemote, exitChan)
	go pipeSocketServer(false, statusLocal, exitChan)

	for {
		select {
		case status := <-statusLocal:
			if !status {
				localConn, err = localListen.Accept()
				if err != nil {
					return err
				}
			}
			go pipeSocketServer(false, statusLocal, exitChan)

		case _ = <-statusRemote:
			go pipeSocketServer(true, statusRemote, exitChan)

		case <-exitChan:
			return nil
		}
	}
}

func StartClient(remoteAddr, remotePort, localAddr, localPort string) error {
	var err error

	dial, err := grpc.Dial(remoteAddr+":"+remotePort, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return err
	}

	remoteConnStreamClient, err = protoc.NewTCPClient(dial).Connect(context.Background())
	if err != nil {
		return err
	}

	localConn, err = net.Dial("tcp", localAddr+":"+localPort)
	if err != nil {
		return err
	}

	statusRemote := make(chan bool)
	statusLocal := make(chan bool)
	exitChan := make(chan struct{})

	go pipeSocketClient(true, statusRemote, exitChan)
	go pipeSocketClient(false, statusLocal, exitChan)

	for {
		select {
		case status := <-statusLocal:
			if !status {
				localConn, err = net.Dial("tcp", localAddr+":"+localPort)
				if err != nil {
					return err
				}
			}

			go pipeSocketClient(false, statusLocal, exitChan)

		case <-exitChan:
			return nil
		}
	}
}

func pipeSocketClient(remoteToLocal bool, status chan<- bool, exitChan chan struct{}) error {
	for {
		buf := make([]byte, 1024)
		var err error
		var read int
		var content []byte

		if remoteToLocal {
			resp, err := remoteConnStreamClient.Recv()
			if err != nil {
				SafeClose(exitChan)
				return err
			}
			content = resp.Data
		} else {
			read, err = localConn.Read(buf)
		}
		if err != nil {
			status <- false
			return err
		}

		if remoteToLocal {
			_, err = localConn.Write(content)
		} else {
			err = remoteConnStreamClient.Send(&protoc.Resp{
				Data: buf[:read],
			})
		}
		if err != nil {
			SafeClose(exitChan)
			return err
		}
	}
}

func pipeSocketServer(remoteToLocal bool, status chan<- bool, exitChan chan struct{}) error {
	for {
		buf := make([]byte, 1024)
		var err error
		var read int
		var content []byte

		if remoteToLocal {
			resp, err := remoteConnStreamServer.Recv()
			if err != nil {
				status <- false
				SafeClose(remoteConnStreamExit)
				return err
			}
			content = resp.Data
		} else {
			read, err = localConn.Read(buf)
		}
		if err != nil {
			status <- false
			return err
		}

		if remoteToLocal {
			_, err = localConn.Write(content)
		} else {
			err = remoteConnStreamServer.Send(&protoc.Req{
				Status: 200,
				Data:   buf[:read],
			})
		}
		if err != nil {
			SafeClose(remoteConnStreamExit)
			SafeClose(exitChan)
			return err
		}
	}
}
