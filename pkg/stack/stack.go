package stack

import (
	"log"
	"sync"
	"sync/atomic"
)

type CbFunction func(*[]int64, uint64)

// Stack is a stack implementation that is designed to be able to push data into it and process it at the same time
//  with no locking time
type Stack struct {
	data         *[]int64
	swap         *[]int64
	current      uint64
	upperLimit   uint64
	cbInProgress sync.Mutex
	swapLock     sync.Mutex
	swapCb       *CbFunction
}

func New(elementsLimit int64) *Stack {
	data := make([]int64, elementsLimit+1)
	swap := make([]int64, elementsLimit+1)
	return &Stack{
		data:         &data,
		swap:         &swap,
		swapLock:     sync.Mutex{},
		cbInProgress: sync.Mutex{},
		current:      uint64(0),
		upperLimit:   uint64(elementsLimit),
	}
}

func (s *Stack) SetDataCallBack(cb CbFunction) {
	s.swapCb = &cb
}

func (s *Stack) Swap(runCbInParallel bool) bool {
	s.swapLock.Lock()
	isThereNewData := s.swapData(runCbInParallel, s.current)
	s.swapLock.Unlock()
	return isThereNewData
}

//go:norace
func (s *Stack) swapData(runCbInParallel bool, current uint64) bool {
	s.cbInProgress.Lock()
	s.data, s.swap, s.current = s.swap, s.data, 0
	isThereNewData := len(*s.swap) > 0
	if runCbInParallel {
		go s.runCb(s.swap, current)
	} else {
		s.runCb(s.swap, current)
	}
	return isThereNewData
}

func (s *Stack) runCb(values *[]int64, itemCount uint64) {
	defer s.cbInProgress.Unlock()
	if s.swapCb != nil {
		(*s.swapCb)(values, itemCount)
	}
}

//go:norace
func (s *Stack) Push(value int64) {
	idx := atomic.AddUint64(&s.current, 1)
	if idx >= s.upperLimit {
		s.swapLock.Lock()
		if s.current >= s.upperLimit {
			log.Println("Stack is full, flush it")
			s.swapData(true, idx-1)
		}
		idx = atomic.AddUint64(&s.current, 1)
		s.swapLock.Unlock()
	}
	(*s.data)[idx-1] = value
}

func (s *Stack) GetMemoryConsumption() uint64 {
	return s.upperLimit * 8 * 2
}
