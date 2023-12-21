package main

import (
	"fmt"
	"os"

	expose_register "github.com/shiyunjin/reverse-grpc"
)

func main() {
	if len(os.Args) < 2 || (os.Args[1] != "-s" && os.Args[1] != "-c") {
		fmt.Printf("Usage %s: [-s remote_port local_port | -c remote_addr remote_port local_addr local_port]", os.Args[0])
		os.Exit(1)
	}

	if os.Args[1] == "-s" {
		expose_register.StartServer(os.Args[2], os.Args[3])
	}

	if os.Args[1] == "-c" {
		expose_register.StartClient(os.Args[2], os.Args[3], os.Args[4], os.Args[5])
	}
}
