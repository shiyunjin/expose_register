package main

import (
	"context"
	"fmt"
	"net"
	"os"

	"github.com/shiyunjin/expose_register"
)

func main() {
	if len(os.Args) < 2 || (os.Args[1] != "-s" && os.Args[1] != "-c") {
		fmt.Printf("Usage %s: [-s remote_port local_port | -c remote_addr remote_port local_addr local_port]", os.Args[0])
		os.Exit(1)
	}

	if os.Args[1] == "-s" {
		remoteListen, err := net.Listen("tcp", "0.0.0.0:"+os.Args[2])
		if err != nil {
			return
		}

		listen, err := net.Listen("tcp", "0.0.0.0:"+os.Args[3])
		if err != nil {
			return
		}

		if err := expose_register.StartServer(context.Background(), "123456", remoteListen, listen); err != nil {
			fmt.Println(err)
			return
		}
	}

	if os.Args[1] == "-c" {
		err := expose_register.StartClient(context.Background(), "123456", os.Args[2]+":"+os.Args[3], true, func() (net.Conn, error) {
			return net.Dial("tcp", os.Args[4]+":"+os.Args[5])
		})
		if err != nil {
			fmt.Println(err)
			return
		}
	}
}
