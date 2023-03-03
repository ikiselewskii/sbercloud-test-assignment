package main

import (
	"log"
	"net"
	"os"
	"os/signal"
	"playlist-grpc/domain"
	protos "playlist-grpc/presentation"
	"syscall"

	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

func main() {
	exit := make(chan os.Signal,1)
	signal.Notify(exit, syscall.SIGTERM, os.Interrupt)
	gs := grpc.NewServer()
	
	playlist := domain.New("playlist.txt")
	
	protos.RegisterPlaylistServer(gs,playlist)

	reflection.Register(gs)

	l,err := net.Listen("tcp",":9092")
	if err != nil{
		panic(err)
	}
	go func(){
		gs.Serve(l)
	}()
	for{
		select{
		case <- exit:
			gs.Stop()
			l.Close()
			err := playlist.Save("playlist.txt")
			if err != nil{
				log.Panic(err)
			}
			return
		default:
			continue
		}
	}
}
