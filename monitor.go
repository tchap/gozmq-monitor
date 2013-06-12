/*
Copyright (C) 2013 Ondrej Kupka
Copyright (C) 2013 Contributors as noted in the AUTHORS file

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"),
to deal in the Software without restriction, including without limitation
the rights to use, copy, modify, merge, publish, distribute, sublicense,
and/or sell copies of the Software, and to permit persons to whom
the Software is furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included
in all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL
THE AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING
FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS
IN THE SOFTWARE.
*/

/*
gozmq Monitor combines gozmq.Socket.Monitor API call with gozmq-poller.Poller
and implements a channel-based API for receiving socket events.
*/
package monitor

// #cgo pkg-config: libzmq
// #include <zmq.h>
// #include "zmq_version_guard.h"
import "C"

import (
	"errors"
	"fmt"
	"unsafe"
)

import zmq "github.com/alecthomas/gozmq"
import poller "github.com/tchap/gozmq-poller"

type SocketEvent struct {
	Event uint16
	Value int32
	Addr  string
	Error error
}

type Monitor struct {
	socket *zmq.Socket
	poller *poller.Poller
}

// Construct a new monitor reacting to the requested event types. Use addr as
// the internal inproc endpoint for transporting events.
func New(ctx *zmq.Context, socket *zmq.Socket,
	addr string, events zmq.Event) (monitor *Monitor, err error) {

	var major, minor, patch C.int
	C.zmq_version(&major, &minor, &patch)
	if !(major == 3 && minor >= 3) {
		return nil, errors.New(fmt.Sprintf(
			"This library requires libzmq >= 3.3.0, but %d.%d.%d was detected.",
			major, minor, patch))
	}

	mon, err := ctx.NewSocket(zmq.PAIR)
	if err != nil {
		return
	}

	err = socket.Monitor(addr, events)
	if err != nil {
		return
	}

	err = mon.Connect(addr)
	if err != nil {
		mon.Close()
		return
	}

	poller, err := poller.New(ctx, 0)
	if err != nil {
		mon.Close()
		return
	}

	monitor = &Monitor{
		socket: mon,
		poller: poller,
	}
	return
}

// Start polling for events on the monitor socket and pass those socket events
// through the channel back to the user.
func (self *Monitor) Start() (eventChan <-chan *SocketEvent, err error) {
	ch, err := self.poller.Poll(zmq.PollItems{zmq.PollItem{
		Socket: self.socket,
		Events: zmq.POLLIN,
	}})
	if err != nil {
		return
	}

	eventCh := make(chan *SocketEvent, 1)

	go func() {
		for res := range ch {
			if res.Error != nil {
				eventCh <- &SocketEvent{Error: res.Error}
				break
			}

			frames, ex := self.socket.RecvMultipart(0)
			if ex != nil {
				eventCh <- &SocketEvent{Error: ex}
				continue
			}

			ex = self.poller.Continue()

			if ex != nil {
				eventCh <- &SocketEvent{Error: ex}
				continue
			}

			eventCh <- self.parseEvent(frames)
		}
		close(eventCh)
	}()

	eventChan = eventCh
	return
}

// A wrapper around Poller.Stop that stops the internal poller.
func (self *Monitor) Stop() (err error) {
	return self.poller.Stop()
}

// Close the poller, stop the other background goroutine.
func (self *Monitor) Close() (exit <-chan bool, err error) {
	ch, err := self.poller.Close()
	if err != nil {
		return
	}

	exitCh := make(chan bool, 1)
	exit = exitCh

	go func() {
		<-ch

		// Ignore the error (ENOTSOCK cannot happen anyway).
		self.socket.Close()

		exitCh <- true
		close(exitCh)
	}()

	return
}

// Close and wait for the close channel to be signaled.
func (self *Monitor) CloseWait() (err error) {
	ch, err := self.Close()
	if err != nil {
		return
	}
	<-ch
	return
}

// Internal wrapper around the helper function, which is implemented in C so
// that we can access C structures directly.
func (self *Monitor) parseEvent(frames [][]byte) (event *SocketEvent) {
	switch len(frames) {
	case 1: // libzmq < 3.3.0 - not supported
		panic("Deprecated libzmq socket monitor API was detected.")
	case 2: // libzmq >= 3.3.0
		var e SocketEvent
		uint16Size := unsafe.Sizeof(e.Event)
		int32Size := unsafe.Sizeof(e.Value)

		if uintptr(len(frames[0])) != (uint16Size + int32Size) {
			panic(fmt.Sprintln("Invalid payload size [frame %v]", frames[0]))
		}
		event_raw := frames[0][:uint16Size]
		value_raw := frames[0][int32Size:]

		e.Event = *(*uint16)(unsafe.Pointer(&event_raw[0]))
		e.Value = *(*int32)(unsafe.Pointer(&value_raw[0]))
		e.Addr = string(frames[1])
		return &e
	default:
		panic(fmt.Sprintln("Unexpected payload received: %v", frames))
	}
}
