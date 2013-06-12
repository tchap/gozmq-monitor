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

package monitor

import (
	"errors"
	"fmt"
	"testing"
	"time"
)

import zmq "github.com/alecthomas/gozmq"

const (
	TestMonitor_Sink   = "tcp://127.0.0.1:23456"
	TestMonitor_Events = "inproc://TestMonitor_Events"
)

func TestMonitor(test *testing.T) {
	factory, err := NewSocketFactory()
	if err != nil {
		test.Fatal(err)
	}
	defer factory.Close()

	out, err := factory.ctx.NewSocket(zmq.PULL)
	if err != nil {
		test.Fatal(err)
	}
	err = out.Bind(TestMonitor_Sink)
	if err != nil {
		out.Close()
		test.Fatal(err)
	}

	in, err := factory.NewSocket(zmq.PUSH)
	if err != nil {
		out.Close()
		test.Fatal(err)
	}

	mon, err := New(factory.ctx, in, TestMonitor_Events,
		zmq.EVENT_CONNECTED|zmq.EVENT_DISCONNECTED)
	if err != nil {
		out.Close()
		test.Fatal(err)
	}
	defer func() {
		ex := mon.CloseWait()
		if ex != nil {
			test.Error(ex)
		}
		return
	}()

	eventChan, err := mon.Start()
	if err != nil {
		out.Close()
		test.Fatal(err)
	}

	err = in.Connect(TestMonitor_Sink)
	if err != nil {
		out.Close()
		test.Fatal(err)
	}

	err = assertNextEvent(test, eventChan, zmq.EVENT_CONNECTED)
	if err != nil {
		out.Close()
		test.Fatal(err)
	}

	err = out.Close()
	if err != nil {
		test.Fatal(err)
	}

	err = assertNextEvent(test, eventChan, zmq.EVENT_DISCONNECTED)
	if err != nil {
		test.Fatal(err)
	}
}

func assertNextEvent(test *testing.T,
	eventChan <-chan *SocketEvent, event zmq.Event) error {

	select {
	case e := <-eventChan:
		if e.Error != nil {
			return e.Error
		}
		if zmq.Event(e.Event) != event {
			return errors.New(
				fmt.Sprintf("Unexpected event encountered: %d", e.Event))
		}
		if e.Addr != TestMonitor_Sink {
			return errors.New("Unexpected event address encountered.")
		}
	case <-time.After(time.Second):
		return errors.New("Test timed out.")
	}

	return nil
}

/**
 * SocketFactory to easily create sockets and close 0MQ
 */

type SocketFactory struct {
	ctx         *zmq.Context
	ss          []*zmq.Socket
	pipeCounter int
}

func NewSocketFactory() (sf *SocketFactory, err error) {
	ctx, err := zmq.NewContext()
	if err == nil {
		sf = &SocketFactory{ctx, make([]*zmq.Socket, 0), 0}
	}
	return
}

func (self *SocketFactory) NewSocket(t zmq.SocketType) (*zmq.Socket, error) {
	if self.ctx == nil {
		return nil, errors.New("SocketFactory has been closed.")
	}
	s, err := self.ctx.NewSocket(t)
	if err != nil {
		return nil, err
	}
	self.ss = append(self.ss, s)
	return s, nil
}

func (self *SocketFactory) NewPipe() (in *zmq.Socket, out *zmq.Socket, err error) {
	in, err = self.NewSocket(zmq.PAIR)
	if err != nil {
		return
	}
	out, err = self.NewSocket(zmq.PAIR)
	if err != nil {
		return
	}

	endpoint := fmt.Sprintf("inproc://pipe%d", self.pipeCounter)
	self.pipeCounter++

	// Leave the sockets to be collected by factory.Close().
	err = out.Bind(endpoint)
	if err != nil {
		return
	}
	err = in.Connect(endpoint)
	if err != nil {
		return
	}

	return
}

func (self *SocketFactory) Close() {
	for _, s := range self.ss {
		s.Close()
	}
	self.ctx.Close()
	self.ctx = nil
}
