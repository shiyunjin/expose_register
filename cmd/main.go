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
		err := expose_register.StartServer("123456", os.Args[2], "tcp", "0.0.0.0:"+os.Args[3])
		if err != nil {
			fmt.Println(err)
			return
		}
	}

	if os.Args[1] == "-c" {
		err := expose_register.StartClient("123456", os.Args[2]+":"+os.Args[3], true, "tcp", os.Args[4]+":"+os.Args[5])
		if err != nil {
			fmt.Println(err)
			return
		}
	}
}
