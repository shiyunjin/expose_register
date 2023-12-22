package expose_register

import (
	"context"
	"fmt"
	"net"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	protoc "github.com/shiyunjin/expose_register/proto"
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
	secret string
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
	// Read metadata from client.
	md, ok := metadata.FromIncomingContext(stream.Context())
	if !ok {
		return status.Errorf(codes.DataLoss, "failed to get metadata")
	}
	if t, ok := md["token"]; ok {
		if t[0] != s.secret {
			return status.Errorf(codes.Unauthenticated, "invalid token")
		}
	} else {
		return status.Errorf(codes.Unauthenticated, "empty token")
	}

	SafeClose(remoteConnStreamExit)
	remoteConnStreamExit = make(chan struct{})

	remoteConnStreamServer = stream

	<-remoteConnStreamExit
	return nil
}

func StartServer(ctx context.Context, secret, remotePort, localNetwork, localAddr string) error {
	lis, err := net.Listen("tcp", ":"+remotePort)
	if err != nil {
		return err
	}
	defer lis.Close()

	s := grpc.NewServer()
	protoc.RegisterTCPServer(s, &gServer{
		secret: secret,
	})

	exitChan := make(chan struct{})

	go func() {
		if err := s.Serve(lis); err != nil {
			fmt.Printf("failed to serve: %v\n", err)
			SafeClose(exitChan)
		}
	}()
	for {
		select {
		case <-exitChan:
			return nil
		case <-ctx.Done():
			return nil
		default:
		}

		if remoteConnStreamServer != nil {
			break
		}

		time.Sleep(1 * time.Second)
	}

	localListen, err := net.Listen(localNetwork, localAddr)
	if err != nil {
		return err
	}
	defer func() {
		if localListen != nil {
			localListen.Close()
		}
	}()

	localConn, err = localListen.Accept()
	if err != nil {
		return err
	}
	defer func() {
		if localConn != nil {
			localConn.Close()
		}
	}()

	statusRemote := make(chan bool)
	statusLocal := make(chan bool)

	go pipeSocketServer(true, statusRemote, exitChan)
	go pipeSocketServer(false, statusLocal, exitChan)

	for {
		select {
		case s := <-statusLocal:
			if !s {
				if localConn != nil {
					localConn.Close()
				}
				if localListen != nil {
					localListen.Close()
				}
				localListen, err := net.Listen(localNetwork, localAddr)
				if err != nil {
					return err
				}
				localConn, err = localListen.Accept()
				if err != nil {
					return err
				}
			}
			go pipeSocketServer(false, statusLocal, exitChan)

		case <-exitChan:
			return nil
		case <-ctx.Done():
			return nil
		}
	}
}

func StartClient(ctx context.Context, secret, remoteAddr string, remoteInSecret bool, localNetwork, localAddr string) error {
	dialOptions := []grpc.DialOption{}

	if remoteInSecret {
		dialOptions = append(dialOptions, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}

	dial, err := grpc.Dial(remoteAddr, dialOptions...)
	if err != nil {
		return err
	}

	// Create metadata and context.
	md := metadata.Pairs("token", secret)
	ctxMd := metadata.NewOutgoingContext(ctx, md)

	remoteConnStreamClient, err = protoc.NewTCPClient(dial).Connect(ctxMd)
	if err != nil {
		return err
	}

	localConn, err = net.Dial(localNetwork, localAddr)
	if err != nil {
		return err
	}
	defer func() {
		if localConn != nil {
			localConn.Close()
		}
	}()

	statusRemote := make(chan bool)
	statusLocal := make(chan bool)
	exitChan := make(chan struct{})

	go pipeSocketClient(true, statusRemote, exitChan)
	go pipeSocketClient(false, statusLocal, exitChan)

	for {
		select {
		case s := <-statusLocal:
			if !s {
				if localConn != nil {
					localConn.Close()
				}
				localConn, err = net.Dial(localNetwork, localAddr)
				if err != nil {
					return err
				}
			}

			go pipeSocketClient(false, statusLocal, exitChan)

		case <-exitChan:
			return nil
		case <-ctx.Done():
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
			err = remoteConnStreamClient.Send(&protoc.Data{
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
			err = remoteConnStreamServer.Send(&protoc.Data{
				Data: buf[:read],
			})
		}
		if err != nil {
			SafeClose(exitChan)
			return err
		}
	}
}
