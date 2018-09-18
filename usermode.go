package main

import (
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/scylladb/scylla-bench/usermode"

	"github.com/gocql/gocql"
)

// UserMode is a decorator for usermode.Driver that
// plugs it into current main function.
type UserMode struct {
	wg     sync.WaitGroup
	once   sync.Once
	driver *usermode.Driver
	ops    map[string]bool
	ctx    context.Context
	cancel func()
	ch     chan int
	err    error
}

// NewUserMode gives new UserMode value.
func NewUserMode() *UserMode {
	ctx, cancel := context.WithCancel(context.Background())

	um := &UserMode{
		ops:    make(map[string]bool),
		ch:     make(chan int),
		ctx:    ctx,
		cancel: cancel,
	}
	um.wg.Add(1)

	for _, op := range strings.Split(ops, ",") {
		if i := strings.IndexRune(op, '='); i != -1 {
			if ok, err := strconv.ParseBool(op[i+1:]); err == nil {
				um.ops[op[:i]] = ok
			}
		}
	}

	return um
}

// Summary gives a summary of profile-driven benchmark.
func (um *UserMode) Summary() string {
	return um.driver.Summary()
}

// Do is a single worker thread that takes care of inserting batches
// in a synchronous manner.
// Do blocks until there are batches awaiting to be processed.
func (um *UserMode) Do(s *gocql.Session, ch chan Result, _ WorkloadGenerator, rl RateLimiter) {
	if err := um.init(s, ch); err != nil {
		ch <- Result{
			Final:   true,
			Latency: NewHistogram(),
		}
		return
	}

	rb := NewResultBuilder()
	start := time.Now()

	if um.ops["insert"] || len(um.ops) == 0 {
		ctx := usermode.WithTrace(um.ctx, um.makeTrace(ch, rb, rl))

		for range um.ch {
			rl.Wait()

			if err := um.driver.BatchInsert(ctx); err != nil {
				log.Println(err)
			}
		}
	}

	rb.FullResult.ElapsedTime = time.Since(start)
	ch <- *rb.FullResult
}

func (um *UserMode) makeTrace(ch chan<- Result, rb *ResultBuilder, rl RateLimiter) *usermode.Trace {
	last := time.Now()
	return &usermode.Trace{
		ExecutedBatch: func(info usermode.ExecutedBatchInfo) {
			if info.Err != nil {
				rb.IncErrors()
			} else {
				rb.IncOps()
				rb.AddRows(info.Size)
				rb.RecordLatency(info.Latency, rl)
			}
			if now := time.Now(); now.Sub(last) > time.Second {
				ch <- *rb.PartialResult
				rb.ResetPartialResult()
				last = now
			}
		},
	}
}

func (um *UserMode) makeInit(s *gocql.Session, ch chan<- Result) func() {
	return func() {
		defer um.wg.Done()

		if err := um.setup(s); err != nil {
			um.err = err
			ch <- Result{
				Final:   true,
				Latency: NewHistogram(),
				Errors:  1,
			}
		}
	}
}

func (um *UserMode) setup(s *gocql.Session) error {
	if profileFile == "" {
		return errors.New("no usermode specified")
	}
	p, err := ioutil.ReadFile(profileFile)
	if err != nil {
		return fmt.Errorf("error reading %q usermode: %s", profileFile, err)
	}
	q, err := usermode.ParseProfile(p)
	if err != nil {
		return fmt.Errorf("error parsing %q usermode: %s", profileFile, err)
	}
	um.driver = usermode.NewDriver(q, s)
	if b, ok := um.ops["keyspace"]; b || !ok {
		if err := um.driver.CreateKeyspace(); err != nil {
			return fmt.Errorf("error preparing keyspace: %s", err)
		}
	}
	if b, ok := um.ops["table"]; b || !ok {
		if err := um.driver.CreateTable(); err != nil {
			return fmt.Errorf("error preparing table: %s", err)
		}
	}
	go um.waitForStop()
	if testDuration > 0 {
		go um.loopFor(testDuration)
	} else {
		go um.loop(int(iterations))
	}
	return nil
}

func (um *UserMode) waitForStop() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	defer um.cancel()

	for range ticker.C {
		if atomic.LoadUint32(&stopAll) != 0 {
			return
		}
	}
}

func (um *UserMode) loopFor(d time.Duration) {
	defer close(um.ch)
	t := time.After(d)
	for i := 0; ; i++ {
		select {
		case um.ch <- i:
		case <-t:
			return
		case <-um.ctx.Done():
			return
		}
	}

}

func (um *UserMode) loop(n int) {
	defer close(um.ch)
	for i := 0; i < n; i++ {
		select {
		case um.ch <- i:
		case <-um.ctx.Done():
			return
		}
	}
}

func (um *UserMode) init(s *gocql.Session, ch chan<- Result) error {
	um.once.Do(um.makeInit(s, ch))
	um.wg.Wait()
	return um.err
}
