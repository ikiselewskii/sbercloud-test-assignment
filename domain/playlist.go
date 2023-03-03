package domain

import (
	"bufio"
	"context"
	"fmt"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/emptypb"
	"log"
	"os"
	protos "playlist-grpc/presentation"
	"strings"
	"sync"
	"time"
)

type Track struct {
	Title    string
	Author   string
	Duration time.Duration
	prev     *Track
	next     *Track
}

var (
	Current      Track
	IsPlaying    bool
	PausedOn     time.Duration
	pauseCh      chan struct{}      = make(chan struct{})
	playtimeReq  chan struct{}      = make(chan struct{})
	playtimeResp chan time.Duration = make(chan time.Duration)
	mu           sync.RWMutex
)

func New(filename string) *Track {
	file, err := os.Open(filename)
	if err != nil {
		panic(err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	dummy := &Track{}
	current := dummy
	for scanner.Scan() {
		line := scanner.Text()
		trackProps := strings.Split(line, " ")
		if len(trackProps) != 3 {
			log.Printf("Invalid track format: %s\n", line)
			continue
		}
		duration, err := time.ParseDuration(trackProps[2])
		if err != nil {
			log.Printf("Invalid track duration: %s\n", trackProps[2])
			continue
		}
		newTrack := &Track{
			Title:    strings.ReplaceAll(trackProps[0], "_", " "),
			Author:   strings.ReplaceAll(trackProps[1], "_", " "),
			Duration: duration,
			prev:     current,
		}
		current.next = newTrack
		current = newTrack
	}
	current.next = nil
	Current = *dummy.next

	return &Current
}

func (p *Track) Save(filename string) error {
	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()
	writer := bufio.NewWriter(file)
	defer writer.Flush()
	mu.RLock()
	defer mu.RUnlock()
	for p.prev != nil {
		p = p.prev
	}
	for node := p; node != nil; node = node.next {
		title := strings.ReplaceAll(node.Title, " ", "_")
		author := strings.ReplaceAll(node.Author, " ", "_")
		duration := node.Duration.String()
		_, err := fmt.Fprintf(writer, "%s %s %s\n", title, author, duration)
		if err != nil {
			return err
		}

	}
	return nil
}

func (p *Track) AddSong(ctx context.Context, song *protos.Song) (*emptypb.Empty, error) {
	if song.Duration.AsDuration() <= time.Second*time.Duration(30) {
		return nil, status.Errorf(codes.InvalidArgument, "Invalld song duration")
	}
	mu.Lock()
	defer mu.Unlock()
	for ; p.next != nil; p = p.next {
	}
	p.next = &Track{
		Title:    song.Title,
		Author:   song.Author,
		Duration: song.Duration.AsDuration(),
		prev:     p,
	}
	return nil, status.Errorf(
		codes.OK,
		"Song %s by %s added succesfully",
		song.Title,
		song.Author,
	)
}

func (p *Track) Delete(ctx context.Context, void *emptypb.Empty) (*emptypb.Empty, error) {
	mu.Lock()
	defer mu.Unlock()
	if IsPlaying {
		return void, status.Errorf(codes.FailedPrecondition, "Can`t delete a playing track")
	}
	if p == nil {
		return void, status.Errorf(codes.NotFound, "Nothing to delete")
	}
	if p.prev == nil {
		p = p.next
		p.prev = nil
		return void, status.Errorf(codes.OK, "Delete succesful")

	}
	if p.next == nil {
		p = p.prev
		p.next = nil
		return void, status.Errorf(codes.OK, "Delete succesful")

	}
	p.prev.next = p.next
	p.next.prev = p.prev
	p = nil
	PausedOn = 0
	return void, status.Errorf(codes.OK, "Delete succesful")
}

func (p *Track) Next(ctx context.Context, void *emptypb.Empty) (*emptypb.Empty, error) {
	mu.Lock()
	if p == nil{
		return void, status.Error(codes.NotFound,"Playlist is empty")
	}
	if Current.next != nil {
		Current = *Current.next
		PausedOn = 0
	} else {
		return void, status.Errorf(codes.NotFound, "This is the last track")
	}
	mu.Unlock()
	Current.Play(ctx, void)
	return void, status.Errorf(codes.OK, "Next track")
}

func (p *Track) Prev(ctx context.Context, void *emptypb.Empty) (*emptypb.Empty, error) {
	mu.Lock()
	if p == nil{
		return void, status.Error(codes.NotFound,"Playlist is empty")
	}
	if p.prev != nil {
		p = p.prev
		PausedOn = 0
	} else {
		return void, status.Errorf(codes.NotFound, "This is the first track")
	}
	mu.Unlock()
	_, err := Current.Play(ctx, void)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Unable to play due to %v", err)
	}
	return void, status.Errorf(
		codes.OK, "Now playing previous",
	)
}

func (p *Track) Play(ctx context.Context, void *emptypb.Empty) (*emptypb.Empty, error) {
	mu.RLock()
	defer mu.RUnlock()
	if p == nil{
		return void, status.Error(codes.NotFound,"Playlist is empty")
	}
	if IsPlaying {
		return void, status.Errorf(
			codes.FailedPrecondition,
			"Something is already playing",
		)
	}
	forked_ctx := ctx
	go player(forked_ctx, Current.Duration-PausedOn)
	return void, status.Errorf(
		codes.OK,
		"Playing",
	)
}

func (p *Track) Pause(ctx context.Context, void *emptypb.Empty) (*emptypb.Empty, error) {
	mu.RLock()
	if p == nil{
		return void, status.Error(codes.NotFound,"Playlist is empty")
	}
	if !IsPlaying {
		return void, status.Errorf(codes.FailedPrecondition, "error")
	}
	pauseCh <- struct{}{}
	title := p.Title
	author := p.Author
	pauseTime := PausedOn.String()
	mu.RUnlock()
	return void, status.Errorf(codes.OK, "Track %v by %v is paused on %v",
		title,
		author,
		pauseTime,
	)
}

func (p *Track) NowPlaying(ctx context.Context, void *emptypb.Empty) (*protos.Track, error) {
	mu.RLock()
	defer mu.RUnlock()
	if p == nil{
		return nil, status.Error(codes.NotFound,"Playlist is empty")
	}
	if !IsPlaying {
		return nil, status.Errorf(
			codes.FailedPrecondition,
			"Nothing is on, %s by %s is paused on %s",
			Current.Title,
			Current.Author,
			Current.Duration.String(),
		)
	}
	playtimeReq <- struct{}{}
	playtime := <-playtimeResp
	return &protos.Track{
			Title:    p.Title,
			Author:   p.Author,
			Duration: durationpb.New(p.Duration),
			Playtime: durationpb.New(playtime),
		},
		status.Errorf(
			codes.OK,
			"Now playing",
		)
}

func player(ctx context.Context, t time.Duration) {
	IsPlaying = true
	ctx, cancel := context.WithTimeout(ctx, t)
	defer cancel()
	start := time.Now()
	for {
		select {
		case <-pauseCh:
			d, _ := ctx.Deadline()
			mu.Lock()
			defer mu.Unlock()
			PausedOn = PausedOn + d.Sub(start)
			IsPlaying = false
			return
		case <-ctx.Done():
			Current.Next(context.Background(), nil)
			return
		case <-playtimeReq:
			d, _ := ctx.Deadline()
			mu.RLock()
			paused := PausedOn
			playtimeResp <- paused + d.Sub(start)
			mu.RUnlock()
			continue
		default:
			continue
		}
	}
}
