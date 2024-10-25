package expose_register

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/hashicorp/yamux"

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

func SafeSend[T any](c chan<- T, content T) {
	defer func() {
		if err := recover(); err != nil {
		}
	}()

	if c != nil {
		c <- content
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

func muxConfig() *yamux.Config {
	muxConfig := yamux.DefaultConfig()
	muxConfig.EnableKeepAlive = true
	muxConfig.KeepAliveInterval = 10 * time.Second
	muxConfig.MaxStreamWindowSize = 6 * 1024 * 1024

	return muxConfig
}

func StartServer(ctx context.Context, secret string, remoteListener net.Listener, localListener net.Listener) error {
	s := grpc.NewServer()
	defer s.Stop()

	protoc.RegisterTCPServer(s, &gServer{
		secret: secret,
	})

	exitChan := make(chan struct{})

	go func() {
		if err := s.Serve(remoteListener); err != nil {
			fmt.Printf("failed to serve: %v\n", err)
			SafeClose(exitChan)
		}
	}()

	defer func() {
		SafeClose(remoteConnStreamExit)
		remoteConnStreamServer = nil
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

	localListen := ListenPipe()
	defer func() {
		if localListen != nil {
			localListen.Close()
		}
	}()

	go func() {
		conn, err := localListen.Dial(`pipe`, `pipe`)
		if err != nil {
			SafeClose(exitChan)
			fmt.Printf("failed to dial: %v\n", err)
			return
		}

		session, err := yamux.Client(conn, muxConfig())
		if err != nil {
			SafeClose(exitChan)
			fmt.Printf("failed to create yamux session: %v\n", err)
			return
		}

		for {
			conn, err := localListener.Accept()
			if err != nil {
				SafeClose(exitChan)
				fmt.Printf("failed to accept: %v\n", err)
				return
			}

			select {
			case <-exitChan:
				conn.Close()
				return
			case <-ctx.Done():
				conn.Close()
				return
			default:
			}

			go func() {
				stream, err := session.Open()
				if err != nil {
					fmt.Printf("failed to open stream: %v\n", err)
					return
				}

				Join(stream, conn)
			}()
		}
	}()

	var err error
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
	defer SafeClose(statusRemote)
	statusLocal := make(chan bool)
	defer SafeClose(statusLocal)

	go pipeSocketServer(true, statusRemote, exitChan)
	go pipeSocketServer(false, statusLocal, exitChan)

	for {
		select {
		case <-statusLocal:
			return nil
		case <-exitChan:
			return nil
		case <-ctx.Done():
			return nil
		}
	}
}

func StartClient(ctx context.Context, secret, remoteAddr string, remoteInSecure bool, localDialer func() (net.Conn, error)) error {
	dialOptions := []grpc.DialOption{}

	if remoteInSecure {
		dialOptions = append(dialOptions, grpc.WithTransportCredentials(insecure.NewCredentials()))
	} else {
		dialOptions = append(dialOptions, grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{
			InsecureSkipVerify: true,
		})))
	}

	dial, err := grpc.Dial(remoteAddr, dialOptions...)
	if err != nil {
		return err
	}
	defer dial.Close()

	// Create metadata and context.
	md := metadata.Pairs("token", secret)
	ctxMd := metadata.NewOutgoingContext(ctx, md)

	remoteConnStreamClient, err = protoc.NewTCPClient(dial).Connect(ctxMd)
	if err != nil {
		return err
	}

	defer func() {
		remoteConnStreamClient = nil
	}()

	exitChan := make(chan struct{})

	pipe := ListenPipe()

	go func() {
		conn, err := pipe.Accept()
		if err != nil {
			fmt.Printf("failed to accept: %v\n", err)
			return
		}

		session, err := yamux.Server(conn, muxConfig())
		if err != nil {
			fmt.Printf("failed to create yamux session: %v\n", err)
			return
		}

		for {
			stream, err := session.Accept()
			if err != nil {
				SafeClose(exitChan)
				fmt.Printf("failed to accept stream: %v\n", err)
				return
			}

			go func() {
				conn, err := localDialer()
				if err != nil {
					fmt.Printf("failed to dial: %v\n", err)
					return
				}

				Join(stream, conn)
			}()
		}
	}()

	localConn, err = pipe.Dial(`pipe`, `pipe`)
	if err != nil {
		return err
	}
	defer func() {
		if localConn != nil {
			localConn.Close()
		}
	}()

	statusRemote := make(chan bool)
	defer SafeClose(statusRemote)
	statusLocal := make(chan bool)
	defer SafeClose(statusLocal)

	go pipeSocketClient(true, statusRemote, exitChan)
	go pipeSocketClient(false, statusLocal, exitChan)

	for {
		select {
		case <-statusLocal:
			return nil
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
			SafeSend(status, false)
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
			SafeSend(status, false)
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
