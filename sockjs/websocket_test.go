package sockjs

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/websocket"
)

func TestHandler_WebSocketHandshakeError(t *testing.T) {
	h := newTestHandler()
	server := httptest.NewServer(http.HandlerFunc(h.sockjsWebsocket))
	defer server.Close()
	req, _ := http.NewRequest("GET", server.URL, nil)
	req.Header.Set("origin", server.URL)
	resp, _ := http.DefaultClient.Do(req)
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("Unexpected response code, got '%d', expected '%d'", resp.StatusCode, http.StatusBadRequest)
	}
}

func TestHandler_WebSocket(t *testing.T) {
	h := newTestHandler()
	server := httptest.NewServer(http.HandlerFunc(h.sockjsWebsocket))
	defer server.CloseClientConnections()
	url := "ws" + server.URL[4:]
	conn, resp, err := websocket.DefaultDialer.Dial(url, nil)
	if err != websocket.ErrBadHandshake {
		t.Errorf("Expected error '%v', got '%v'", websocket.ErrBadHandshake, err)
	}
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("Unexpected response code, got '%d', expected '%d'", resp.StatusCode, http.StatusForbidden)
	}
	if conn != nil {
		t.Errorf("Connection should be nil, got '%v'", conn)
	}
	// another request with "origin" set properly
	var connCh = make(chan Conn)
	h.handlerFunc = func(conn Conn) { connCh <- conn }
	conn, resp, err = websocket.DefaultDialer.Dial(url, map[string][]string{"Origin": []string{server.URL}})
	if err != nil {
		t.Errorf("Unexpected error '%v'", err)
	}
	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Errorf("Wrong response code returned, got '%d', expected '%d'", resp.StatusCode, http.StatusSwitchingProtocols)
	}
	select {
	case <-connCh: //ok
	default:
		t.Errorf("Sockjs Handler not invoked")
	}
}

func TestHandler_WebSocketTerminationByServer(t *testing.T) {
	h := newTestHandler()
	server := httptest.NewServer(http.HandlerFunc(h.sockjsWebsocket))
	defer server.Close()
	url := "ws" + server.URL[4:]
	h.handlerFunc = func(conn Conn) {
		conn.Close(1024, "some close message")
		conn.Close(0, "this should be ognored")
	}
	conn, _, err := websocket.DefaultDialer.Dial(url, map[string][]string{"Origin": []string{server.URL}})
	_, msg, err := conn.ReadMessage()
	if string(msg) != "o" || err != nil {
		t.Errorf("Open frame expected, got '%s' and error '%v', expected '%s' without error", msg, err, "o")
	}
	_, msg, err = conn.ReadMessage()
	if string(msg) != `c[1024,"some close message"]` || err != nil {
		t.Errorf("Open frame expected, got '%s' and error '%v', expected '%s' without error", msg, err, `c[1024,"some close message"]`)
	}
	_, msg, err = conn.ReadMessage()
	if err != io.ErrUnexpectedEOF {
		t.Errorf("Expected EOF, got '%v'", err)
	}
}

func TestHandler_WebSocketTerminationByClient(t *testing.T) {
	h := newTestHandler()
	server := httptest.NewServer(http.HandlerFunc(h.sockjsWebsocket))
	defer server.Close()
	url := "ws" + server.URL[4:]
	var done = make(chan struct{})
	h.handlerFunc = func(conn Conn) {
		if _, err := conn.Recv(); err != ErrSessionNotOpen {
			t.Errorf("Recv should fail")
		}
		close(done)
	}
	conn, _, _ := websocket.DefaultDialer.Dial(url, map[string][]string{"Origin": []string{server.URL}})
	conn.Close()
	<-done
}

func TestHandler_WebSocketCommunication(t *testing.T) {
	h := newTestHandler()
	server := httptest.NewServer(http.HandlerFunc(h.sockjsWebsocket))
	// defer server.CloseClientConnections()
	url := "ws" + server.URL[4:]
	var done = make(chan struct{})
	h.handlerFunc = func(conn Conn) {
		conn.Send("message 1")
		conn.Send("message 2")
		msg, err := conn.Recv()
		if msg != "message 3" || err != nil {
			t.Errorf("Got '%s', expecte '%s'", msg, "message 3")
		}
		conn.Close(123, "close")
		close(done)
	}
	conn, _, _ := websocket.DefaultDialer.Dial(url, map[string][]string{"Origin": []string{server.URL}})
	conn.WriteJSON([]string{"message 3"})
	var expected = []string{"o", `a["message 1"]`, `a["message 2"]`, `c[123,"close"]`}
	for _, exp := range expected {
		_, msg, err := conn.ReadMessage()
		if string(msg) != exp || err != nil {
			t.Errorf("Wrong frame, got '%s' and error '%v', expected '%s' without error", msg, err, exp)
		}
	}
	<-done
}