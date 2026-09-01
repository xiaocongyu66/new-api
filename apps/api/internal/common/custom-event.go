// Copyright 2014 Manu Martinez-Almeida.  All rights reserved.
// Use of this source code is governed by a MIT style
// license that can be found in the LICENSE file.

package common

import (
	"fmt"
	"io"
	"strings"
)

type stringWriter interface {
	io.Writer
	writeString(string) (int, error)
}

type stringWrapper struct {
	io.Writer
}

func (w stringWrapper) writeString(str string) (int, error) {
	return w.Writer.Write([]byte(str))
}

func checkWriter(writer io.Writer) stringWriter {
	if w, ok := writer.(stringWriter); ok {
		return w
	} else {
		return stringWrapper{writer}
	}
}

// Server-Sent Events
// W3C Working Draft 29 October 2009
// http://www.w3.org/TR/2009/WD-eventsource-20091029/

var dataReplacer = strings.NewReplacer(
	"\n", "\n",
	"\r", "\\r")

// CustomEvent does not synchronize writes to the response writer. Streaming
// callers must serialize event writes at the stream level.
type CustomEvent struct {
	Event string
	Id    string
	Retry uint
	Data  interface{}
}

func encode(writer io.Writer, event CustomEvent) error {
	w := checkWriter(writer)
	return writeData(w, event.Data)
}

func writeData(w stringWriter, data interface{}) error {
	dataReplacer.WriteString(w, fmt.Sprint(data))
	if strings.HasPrefix(data.(string), "data") {
		w.writeString("\n\n")
	}
	return nil
}

// RenderTo writes only the event bytes. Callers install the response headers
// themselves; gateway.RenderSSE writes the SSE content type through the
// transport contract before calling this.
func (r CustomEvent) RenderTo(w io.Writer) error {
	return encode(w, r)
}
